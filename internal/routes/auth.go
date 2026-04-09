package routes

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
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

// getClientIP extracts the client IP from the request, respecting X-Forwarded-For
func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Remove port from RemoteAddr
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
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

		ssoRecord, err := app.FindAuthRecordByToken(body.Token, "auth")
		if err != nil {
			log.Printf("[auth] elevate failed: invalid token from IP %s", clientIP)
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication failed"})
		}

		if ssoRecord.Collection().Name != "sso_users" {
			log.Printf("[auth] elevate failed: wrong collection from IP %s", clientIP)
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication failed"})
		}

		email := ssoRecord.Email()
		if email == "" {
			log.Printf("[auth] elevate failed: no email in SSO record from IP %s", clientIP)
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication failed"})
		}

		superuser, err := app.FindAuthRecordByEmail("_superusers", email)
		if err != nil {
			log.Printf("[auth] elevate failed: no superuser for %s from IP %s", maskEmail(email), clientIP)
			return e.JSON(http.StatusForbidden, map[string]string{"error": "access denied"})
		}

		token, err := superuser.NewAuthToken()
		if err != nil {
			log.Printf("[auth] elevate failed: token generation error for %s from IP %s: %v", maskEmail(email), clientIP, err)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "authentication failed"})
		}

		// Mark the SSO record as used by clearing its auth origins to prevent token reuse
		// We don't delete the record to allow future SSO logins with the same identity
		ssoRecord.SetVerified(false)
		if err := app.Save(ssoRecord); err != nil {
			log.Printf("[auth] warning: failed to invalidate SSO session for %s: %v", maskEmail(email), err)
		}

		log.Printf("[auth] elevate success: %s from IP %s", maskEmail(email), clientIP)

		return e.JSON(http.StatusOK, map[string]interface{}{
			"token":  token,
			"record": superuser,
		})
	})
}
