package routes

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/pocketbase/pocketbase/tools/types"
)

// rateLimiter implements a simple in-memory rate limiter per IP
type rateLimiter struct {
	mu       sync.RWMutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
	// Cleanup goroutine to prevent memory leak
	go func() {
		ticker := time.NewTicker(window)
		for range ticker.C {
			rl.cleanup()
		}
	}()
	return rl
}

func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for ip, times := range rl.requests {
		var valid []time.Time
		for _, t := range times {
			if now.Sub(t) < rl.window {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(rl.requests, ip)
		} else {
			rl.requests[ip] = valid
		}
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	times := rl.requests[ip]

	// Filter out expired entries
	var valid []time.Time
	for _, t := range times {
		if now.Sub(t) < rl.window {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.limit {
		rl.requests[ip] = valid
		return false
	}

	rl.requests[ip] = append(valid, now)
	return true
}

// Global rate limiter for auth/elevate: 5 requests per minute per IP
var elevateRateLimiter = newRateLimiter(5, time.Minute)

// maskEmail masks an email for safe logging (e.g., "user@example.com" -> "u***@example.com")
func maskEmail(email string) string {
	if email == "" {
		return "[empty]"
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "[invalid]"
	}
	local := parts[0]
	if len(local) <= 1 {
		return local[0:1] + "***@" + parts[1]
	}
	return local[0:1] + "***@" + parts[1]
}

// envWireopsTrustedProxies is re-read when the value changes (see cachedElevateTrustedNets).
const envWireopsTrustedProxies = "WIREOPS_TRUSTED_PROXIES"

var (
	elevateTrustedMu   sync.Mutex
	cachedElevateEnv   string
	cachedElevateNets  []*net.IPNet
	defaultElevateNets = mustParseElevateDefaultTrustedNets()
)

func mustParseElevateDefaultTrustedNets() []*net.IPNet {
	out := make([]*net.IPNet, 0, 2)
	for _, cidr := range []string{"127.0.0.0/8", "::1/128"} {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("auth: invalid default trusted proxy CIDR " + cidr + ": " + err.Error())
		}
		out = append(out, n)
	}
	return out
}

func parseOneTrustedProxyCIDR(s string) (*net.IPNet, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if strings.Contains(s, "/") {
		_, n, err := net.ParseCIDR(s)
		return n, err
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, errInvalidTrustedProxy
	}
	if ip4 := ip.To4(); ip4 != nil {
		return &net.IPNet{IP: ip4, Mask: net.CIDRMask(32, 32)}, nil
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}, nil
}

// errInvalidTrustedProxy is used only by parseOneTrustedProxyCIDR.
var errInvalidTrustedProxy = errors.New("invalid IP")

func parseElevateTrustedProxyNetsFromEnv(env string) []*net.IPNet {
	nets := append([]*net.IPNet(nil), defaultElevateNets...)
	if env == "" {
		return nets
	}
	for _, part := range strings.Split(env, ",") {
		n, err := parseOneTrustedProxyCIDR(part)
		if err != nil {
			log.Printf("[auth] skipping invalid WIREOPS_TRUSTED_PROXIES entry %q: %v", strings.TrimSpace(part), err)
			continue
		}
		if n != nil {
			nets = append(nets, n)
		}
	}
	return nets
}

func cachedElevateTrustedNets() []*net.IPNet {
	env := os.Getenv(envWireopsTrustedProxies)
	elevateTrustedMu.Lock()
	defer elevateTrustedMu.Unlock()
	// Reparse when env changes or cache is still uninitialized (env "" equals initial
	// cachedElevateEnv "" but nets must still be loaded once).
	if env != cachedElevateEnv || cachedElevateNets == nil {
		cachedElevateEnv = env
		cachedElevateNets = parseElevateTrustedProxyNetsFromEnv(env)
	}
	return cachedElevateNets
}

func peerIPFromRemoteAddr(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func isTrustedProxy(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range cachedElevateTrustedNets() {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// getClientIP returns the client IP for rate limiting. X-Forwarded-For and X-Real-IP are
// only honored when the immediate TCP peer (RemoteAddr) is in the trusted proxy set
// (loopback by default; extend with WIREOPS_TRUSTED_PROXIES).
func getClientIP(r *http.Request) string {
	peer := peerIPFromRemoteAddr(r.RemoteAddr)
	if !isTrustedProxy(peer) {
		return peer
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}
	return peer
}

const (
	ssoUsersCollectionName    = "sso_users"
	ssoUsersTableName         = "sso_users"
	ssoFieldElevateConsumed   = "elevate_consumed"
	ssoFieldElevateConsumedAt = "elevate_consumed_at"
)

var (
	errElevateInvalidToken       = errors.New("elevate: invalid sso token")
	errElevateSSOAlreadyConsumed = errors.New("elevate: sso session already consumed")
	errElevateNoEmail            = errors.New("elevate: sso record has no email")
	errElevateNoUser             = errors.New("elevate: no matching user")
)

// sqlClaimSSOForElevate performs a single-row compare-and-set: only rows with
// elevate_consumed still false (or NULL) are updated.
func sqlClaimSSOForElevate() string {
	return `UPDATE ` + quoteSQLiteIdent(ssoUsersTableName) +
		` SET ` + quoteSQLiteIdent(ssoFieldElevateConsumed) + ` = {:consumed}, ` +
		quoteSQLiteIdent(ssoFieldElevateConsumedAt) + ` = {:consumedAt}, verified = 0` +
		` WHERE id = {:id} AND IFNULL(` + quoteSQLiteIdent(ssoFieldElevateConsumed) + `, 0) = 0`
}

func quoteSQLiteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func RegisterAuthRoutes(r *router.Router[*core.RequestEvent], app core.App) {
	r.POST("/api/custom/auth/elevate", func(e *core.RequestEvent) error {
		clientIP := getClientIP(e.Request)

		// Rate limiting check
		if !elevateRateLimiter.allow(clientIP) {
			log.Printf("[auth] rate limit exceeded for IP %s on /auth/elevate", clientIP)
			return e.JSON(http.StatusTooManyRequests, map[string]string{
				"error": "too many requests, please try again later",
			})
		}

		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil || body.Token == "" {
			log.Printf("[auth] elevate failed: missing token from IP %s", clientIP)
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
		}

		var (
			elevatedToken string
			superRecord   *core.Record
			elevatedEmail string
		)

		txErr := app.RunInTransaction(func(txApp core.App) error {
			ssoRecord, err := txApp.FindAuthRecordByToken(body.Token, "auth")
			if err != nil {
				return errElevateInvalidToken
			}
			if ssoRecord.Collection().Name != ssoUsersCollectionName || ssoRecord.TableName() != ssoUsersTableName {
				return errElevateInvalidToken
			}

			if ssoRecord.GetBool(ssoFieldElevateConsumed) {
				return errElevateSSOAlreadyConsumed
			}

			email := ssoRecord.Email()
			if email == "" {
				return errElevateNoEmail
			}
			elevatedEmail = email

			user, err := txApp.FindAuthRecordByEmail("users", email)
			if err != nil {
				// Auto-provision user if they don't exist
				userCol, colErr := txApp.FindCollectionByNameOrId("users")
				if colErr != nil {
					log.Printf("[auth] failed to find users collection: %v", colErr)
					return errElevateNoUser
				}
				user = core.NewRecord(userCol)
				user.SetEmail(email)
				user.SetVerified(true)
				user.SetPassword(security.RandomString(32))
				user.Set("name", ssoRecord.GetString("name"))
				user.Set("avatar", ssoRecord.GetString("avatar"))
				user.Set("role", ssoRecord.GetString("role"))
				user.Set("is_sso", true)
				user.Set("emailVisibility", true)
				
				if saveErr := txApp.Save(user); saveErr != nil {
					log.Printf("[auth] failed to auto-provision user %s: %v", email, saveErr)
					return errElevateNoUser
				}
			}
			
			needsSave := false
			if role := ssoRecord.GetString("role"); role != "" && user.GetString("role") != role {
				user.Set("role", role)
				needsSave = true
			}
			if !user.GetBool("is_sso") {
				user.Set("is_sso", true)
				needsSave = true
			}
			if !user.GetBool("emailVisibility") {
				user.Set("emailVisibility", true)
				needsSave = true
			}
			
			if needsSave {
				if err := txApp.Save(user); err != nil {
					return err
				}
			}

			consumedAt := types.NowDateTime()
			result, err := txApp.DB().NewQuery(sqlClaimSSOForElevate()).Bind(dbx.Params{
				"consumed":   true,
				"consumedAt": consumedAt.String(),
				"id":         ssoRecord.Id,
			}).Execute()
			if err != nil {
				return err
			}
			n, raErr := result.RowsAffected()
			if raErr != nil {
				return raErr
			}
			if n == 0 {
				return errElevateSSOAlreadyConsumed
			}

			token, err := user.NewAuthToken()
			if err != nil {
				return err
			}

			elevatedToken = token
			superRecord = user
			return nil
		})

		switch {
		case errors.Is(txErr, errElevateInvalidToken):
			log.Printf("[auth] elevate failed: invalid token from IP %s", clientIP)
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication failed"})
		case errors.Is(txErr, errElevateSSOAlreadyConsumed):
			log.Printf("[auth] elevate failed: SSO session already used from IP %s", clientIP)
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication failed"})
		case errors.Is(txErr, errElevateNoEmail):
			log.Printf("[auth] elevate failed: no email in SSO record from IP %s", clientIP)
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication failed"})
		case errors.Is(txErr, errElevateNoUser):
			log.Printf("[auth] elevate failed: no user for %s from IP %s", maskEmail(elevatedEmail), clientIP)
			return e.JSON(http.StatusForbidden, map[string]string{"error": "access denied"})
		case txErr != nil:
			log.Printf("[auth] elevate failed: transaction error from IP %s: %v", clientIP, txErr)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "authentication failed"})
		}

		log.Printf("[auth] elevate success: %s from IP %s", maskEmail(elevatedEmail), clientIP)

		return e.JSON(http.StatusOK, map[string]interface{}{
			"token":  elevatedToken,
			"record": superRecord,
		})
	})
}
