package git

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/sync/singleflight"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/gitprovider"
)

// refreshGraceWindow is how far ahead of oauth_token_expires_at a stored
// token is treated as "expiring soon" and proactively refreshed, instead of
// being handed out and only discovered stale mid git-fetch or mid API call.
// GitLab access tokens default to a 2h lifetime, so 15m leaves ample margin
// for a slow cron tick or a long-running fetch to still redeem it in time.
const refreshGraceWindow = 15 * time.Minute

// refreshTimeout bounds a single refresh's network round trip + DB save,
// independent of any individual caller's own context deadline (see
// refreshOAuthTokenIfNeeded). 20s comfortably covers the GitLab provider's
// single token POST (15s HTTP client timeout) plus the surrounding record
// read/save.
const refreshTimeout = 20 * time.Second

// refreshGroup collapses concurrent refresh attempts for the same
// repository_keys row into a single in-flight call. Without this, two repos
// sharing one provider's credential (all repos under a provider reuse the
// same global row) that both go stale at once would each read the same
// refresh_token and race to redeem it — GitLab rotates refresh tokens on
// use, so the loser's redeem fails with invalid_grant, and if both redeems
// somehow succeed the last Save() to land can persist an already-consumed
// refresh_token, permanently bricking the next refresh. Keyed by record ID,
// which is process-wide unique per credential.
var refreshGroup singleflight.Group

// LoadRepositoryCredential resolves the optional reusable key assigned to a repository.
func LoadRepositoryCredential(ctx context.Context, app core.App, repositoryID string) (*Credential, error) {
	repository, err := app.FindRecordById("repositories", repositoryID)
	if err != nil {
		return nil, fmt.Errorf("find repository: %w", err)
	}
	keyID := repository.GetString("repository_key")
	if keyID == "" {
		return &Credential{AuthType: AuthTypeNone}, nil
	}
	return LoadCredentialByID(ctx, app, keyID)
}

// LoadCredentialByID loads and decrypts a reusable repository key.
func LoadCredentialByID(ctx context.Context, app core.App, keyID string) (*Credential, error) {
	record, err := app.FindRecordById("repository_keys", keyID)
	if err != nil {
		return nil, fmt.Errorf("find repository key: %w", err)
	}

	credential := &Credential{AuthType: AuthType(record.GetString("auth_type"))}
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))

	decrypt := func(field string) ([]byte, error) {
		return decryptRecordField(record, field, secretKey)
	}

	switch credential.AuthType {
	case AuthTypeSSH:
		if credential.SSHPrivateKey, err = decrypt("ssh_private_key"); err != nil {
			return nil, err
		}
		if credential.SSHPassphrase, err = decrypt("ssh_passphrase"); err != nil {
			return nil, err
		}
		credential.SSHKnownHost = record.GetString("ssh_known_host")
	case AuthTypeBasic:
		credential.GitUsername = record.GetString("git_username")
		password, decryptErr := decrypt("git_password")
		if decryptErr != nil {
			return nil, decryptErr
		}
		credential.GitPassword = string(password)
	case AuthTypeOAuthToken:
		providerSlug := record.GetString("oauth_provider")
		provider, ok := gitprovider.Get(providerSlug)
		if !ok {
			return nil, fmt.Errorf("unknown git provider %q", providerSlug)
		}
		token, decryptErr := decrypt("oauth_token")
		if decryptErr != nil {
			return nil, decryptErr
		}
		if len(token) == 0 {
			return nil, fmt.Errorf("repository key %q has no oauth_token stored", keyID)
		}
		accessToken, refreshErr := refreshOAuthTokenIfNeeded(ctx, app, record, provider, secretKey, string(token))
		if refreshErr != nil {
			return nil, refreshErr
		}
		// Downgrade to basic auth for go-git's transport: ResolveTransportAuth
		// already builds gogithttp.BasicAuth generically, so it needs no
		// changes to understand OAuth tokens.
		credential.AuthType = AuthTypeBasic
		credential.GitUsername = provider.BasicAuthUsername()
		credential.GitPassword = accessToken
	default:
		return nil, fmt.Errorf("unsupported repository key auth type %q", credential.AuthType)
	}

	return credential, nil
}

// LoadOAuthToken decrypts and returns the raw OAuth access token stored on a
// repository_keys record, along with the provider slug that issued it. Used
// by git provider discovery routes (list orgs/repos/branches), which need
// the token itself rather than a go-git Credential.
func LoadOAuthToken(ctx context.Context, app core.App, keyID string) (provider, token string, err error) {
	record, err := app.FindRecordById("repository_keys", keyID)
	if err != nil {
		return "", "", fmt.Errorf("find repository key: %w", err)
	}
	if AuthType(record.GetString("auth_type")) != AuthTypeOAuthToken {
		return "", "", fmt.Errorf("repository key %q is not an oauth_token credential", keyID)
	}

	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	if len(secretKey) != 32 {
		return "", "", fmt.Errorf("SECRET_KEY must be exactly 32 bytes")
	}
	if record.GetString("oauth_token") == "" {
		return "", "", fmt.Errorf("repository key %q has no oauth_token stored", keyID)
	}
	plain, err := decryptRecordField(record, "oauth_token", secretKey)
	if err != nil {
		return "", "", err
	}

	providerSlug := record.GetString("oauth_provider")
	gp, ok := gitprovider.Get(providerSlug)
	if !ok {
		return "", "", fmt.Errorf("unknown git provider %q", providerSlug)
	}
	accessToken, err := refreshOAuthTokenIfNeeded(ctx, app, record, gp, secretKey, string(plain))
	if err != nil {
		return "", "", err
	}

	return providerSlug, accessToken, nil
}

// refreshOAuthTokenIfNeeded returns currentToken unchanged unless the stored
// oauth_token_expires_at is within refreshGraceWindow (or already past), in
// which case it refreshes via refreshGroup and returns the fresh access
// token. This is what makes GitLab's short-lived (~2h) OAuth app tokens
// behave like GitHub's non-expiring ones from a caller's perspective —
// nobody has to notice or manually reconnect.
func refreshOAuthTokenIfNeeded(ctx context.Context, app core.App, record *core.Record, provider gitprovider.Provider, secretKey []byte, currentToken string) (string, error) {
	expiresAt := record.GetDateTime("oauth_token_expires_at").Time()
	if expiresAt.IsZero() || time.Now().Add(refreshGraceWindow).Before(expiresAt) {
		return currentToken, nil
	}

	// refreshCtx carries values from ctx (e.g. tracing) but not its
	// cancellation, and gets its own bounded deadline: this call runs inside
	// refreshGroup, shared by every concurrent caller on this credential, so
	// one caller's request being cancelled (client disconnect, its own
	// shorter timeout) must not abort the network round trip for the others
	// still waiting on the result.
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
	defer cancel()

	result, err, _ := refreshGroup.Do(record.Id, func() (any, error) {
		return doRefreshOAuthToken(refreshCtx, app, record.Id, provider, secretKey)
	})
	if err != nil {
		// The token is still within the grace window but not yet actually
		// expired — a transient refresh failure (network blip, provider
		// hiccup) shouldn't break every credential load for the next
		// refreshGraceWindow; fall back to the still-valid current token and
		// let the next load retry. Only once the real expiry has passed is
		// there no usable fallback and the error must propagate.
		if time.Now().Before(expiresAt) {
			return currentToken, nil
		}
		// Only a confirmed provider rejection (refresh_token consumed/
		// invalid) means the credential is actually dead — a transport-level
		// failure (network blip, provider outage) happening to coincide with
		// the token's real expiry shouldn't flip "reconnect required" on for
		// what the very next tick might resolve on its own.
		if errors.Is(err, gitprovider.ErrRefreshRejected) {
			persistRefreshError(app, record.Id, expiresAt, err.Error())
		}
		return "", err
	}
	return result.(string), nil
}

// doRefreshOAuthToken is the singleflight-guarded body of a refresh: only one
// caller per repository_keys row runs this at a time, and every caller that
// joined the same in-flight call gets its result rather than each redeeming
// the refresh_token independently (GitLab rotates it on use, so a second
// redeem of the same token fails). It re-fetches the record fresh — rather
// than trusting the possibly-stale one the caller read before entering the
// singleflight group — so a caller that arrives just after another goroutine
// already refreshed (sequential, not concurrent, so singleflight alone
// wouldn't dedupe it) sees the already-refreshed token and skips redeeming
// again.
func doRefreshOAuthToken(ctx context.Context, app core.App, keyID string, provider gitprovider.Provider, secretKey []byte) (string, error) {
	record, err := app.FindRecordById("repository_keys", keyID)
	if err != nil {
		return "", fmt.Errorf("find repository key: %w", err)
	}

	expiresAt := record.GetDateTime("oauth_token_expires_at").Time()
	if !expiresAt.IsZero() && time.Now().Add(refreshGraceWindow).Before(expiresAt) {
		current, decErr := decryptRecordField(record, "oauth_token", secretKey)
		if decErr != nil {
			return "", decErr
		}
		return string(current), nil
	}

	// A previous attempt may have redeemed the refresh token but failed to
	// persist the result. That pair is now the only live credential (GitLab
	// rotated the refresh token on use), so it takes precedence over what's
	// stored — redeeming the stored, already-consumed refresh token again
	// would fail with invalid_grant and brick the credential.
	base := identityOf(record)
	var pending *pendingRefresh
	if v, ok := pendingRefreshes.Load(keyID); ok {
		p := v.(*pendingRefresh)
		if p.base.matches(record) {
			pending = p
		} else {
			// The row moved on (manual reconnect or a later successful
			// save): the stored credential is authoritative again.
			pendingRefreshes.Delete(keyID)
		}
	}
	if pending != nil && (pending.token.ExpiresAt.IsZero() || time.Now().Add(refreshGraceWindow).Before(pending.token.ExpiresAt)) {
		if err := persistRefreshedToken(app, keyID, base, pending.token, secretKey); err != nil {
			log.Printf("[git] oauth token refresh: key %s: retry persisting refreshed token: %v", keyID, err)
		} else {
			pendingRefreshes.Delete(keyID)
		}
		return pending.token.AccessToken, nil
	}

	current, err := decryptRecordField(record, "oauth_token", secretKey)
	if err != nil {
		return "", err
	}

	var refreshToken string
	if pending != nil {
		refreshToken = pending.token.RefreshToken
	} else if record.GetString("oauth_refresh_token") != "" {
		refreshTokenBytes, err := decryptRecordField(record, "oauth_refresh_token", secretKey)
		if err != nil {
			return "", err
		}
		refreshToken = string(refreshTokenBytes)
	}
	if refreshToken == "" {
		// No refresh token on file: nothing we can do proactively, let the
		// stale token fail naturally against the provider's API.
		return string(current), nil
	}

	newToken, err := provider.RefreshToken(ctx, refreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh %s oauth token: %w", provider.Slug(), err)
	}
	if newToken.RefreshToken == "" && pending != nil {
		// Provider didn't rotate: the pending refresh token is still the live one.
		newToken.RefreshToken = pending.token.RefreshToken
	}

	if err := persistRefreshedToken(app, keyID, base, newToken, secretKey); err != nil {
		// The refresh token was already redeemed: keep the new pair in
		// memory so the next attempt persists it instead of redeeming the
		// consumed one, and still hand the fresh access token to callers.
		log.Printf("[git] oauth token refresh: key %s: persist refreshed token failed, will retry: %v", keyID, err)
		pendingRefreshes.Store(keyID, &pendingRefresh{token: newToken, base: base})
		return newToken.AccessToken, nil
	}
	pendingRefreshes.Delete(keyID)

	return newToken.AccessToken, nil
}

// credentialIdentity identifies the stored credential a refresh started
// from, so a result can be discarded if the row was replaced meanwhile (a
// manual reconnect). The expiry alone isn't enough: a reconnect whose
// response carries no expires_in leaves oauth_token_expires_at untouched.
// The oauth_token ciphertext always changes on reconnect (AES-GCM uses a
// random nonce) and never changes on a failed save.
type credentialIdentity struct {
	expiresAt   time.Time
	tokenCipher string
}

func identityOf(record *core.Record) credentialIdentity {
	return credentialIdentity{
		expiresAt:   record.GetDateTime("oauth_token_expires_at").Time(),
		tokenCipher: record.GetString("oauth_token"),
	}
}

func (c credentialIdentity) matches(record *core.Record) bool {
	current := identityOf(record)
	return c.expiresAt.Equal(current.expiresAt) && c.tokenCipher == current.tokenCipher
}

// pendingRefresh is a redeemed-but-not-yet-persisted token pair, together
// with the identity of the stored credential it replaces.
type pendingRefresh struct {
	token *gitprovider.Token
	base  credentialIdentity
}

// pendingRefreshes holds, per repository_keys ID, a token pair whose save
// failed after the provider already rotated the refresh token. Only touched
// from inside refreshGroup, so it never races with a concurrent redeem.
var pendingRefreshes sync.Map

// persistRetryDelays is the backoff between attempts to save a refreshed
// token (e.g. across a transient SQLite "database is locked").
var persistRetryDelays = []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, time.Second}

// persistRefreshedToken writes newToken onto keyID's row, re-reading the
// record on each attempt and retrying a failed save. It gives up without
// writing if the row no longer matches base: someone else (a manual
// reconnect) replaced the credential meanwhile, and overwriting it would be
// wrong.
func persistRefreshedToken(app core.App, keyID string, base credentialIdentity, newToken *gitprovider.Token, secretKey []byte) error {
	var lastErr error
	for attempt := 0; attempt <= len(persistRetryDelays); attempt++ {
		if attempt > 0 {
			time.Sleep(persistRetryDelays[attempt-1])
		}
		record, err := app.FindRecordById("repository_keys", keyID)
		if err != nil {
			lastErr = fmt.Errorf("find repository key: %w", err)
			continue
		}
		if !base.matches(record) {
			return nil
		}
		if err := applyRefreshedToken(record, newToken, secretKey); err != nil {
			return err
		}
		if err := app.Save(record); err != nil {
			lastErr = fmt.Errorf("persist refreshed oauth token: %w", err)
			continue
		}
		return nil
	}
	return lastErr
}

// applyRefreshedToken sets a freshly-refreshed token pair and its status
// fields on record, encrypting secrets. It doesn't save.
func applyRefreshedToken(record *core.Record, newToken *gitprovider.Token, secretKey []byte) error {
	encryptedAccess, err := crypto.Encrypt([]byte(newToken.AccessToken), secretKey)
	if err != nil {
		return fmt.Errorf("encrypt refreshed oauth_token: %w", err)
	}
	record.Set("oauth_token", encryptedAccess)
	if newToken.RefreshToken != "" {
		encryptedNewRefresh, err := crypto.Encrypt([]byte(newToken.RefreshToken), secretKey)
		if err != nil {
			return fmt.Errorf("encrypt refreshed oauth_refresh_token: %w", err)
		}
		record.Set("oauth_refresh_token", encryptedNewRefresh)
	}
	if !newToken.ExpiresAt.IsZero() {
		record.Set("oauth_token_expires_at", newToken.ExpiresAt)
	}
	if newToken.AccountLogin != "" {
		record.Set("oauth_account_login", newToken.AccountLogin)
	}
	record.Set("oauth_last_refresh_at", time.Now())
	record.Set("oauth_refresh_error", "")
	return nil
}

// persistRefreshError records a terminal (provider-rejected) refresh failure
// on keyID's repository_keys row so callers like the git-providers status
// API and the background refresher's next tick can tell "reconnect
// required" apart from a token that's merely due for its next refresh. Best
// effort: swallows its own save error — losing the status write must never
// mask the original refresh error being returned to the caller.
//
// expectedExpiresAt is the oauth_token_expires_at the failing attempt was
// acting on; it re-fetches the record fresh and skips the write if that
// field has since moved (a concurrent manual reconnect or a later
// successful refresh already fixed the credential), so a slow-to-persist
// stale failure can't clobber a credential that's already healthy again.
func persistRefreshError(app core.App, keyID string, expectedExpiresAt time.Time, message string) {
	record, err := app.FindRecordById("repository_keys", keyID)
	if err != nil {
		return
	}
	if !record.GetDateTime("oauth_token_expires_at").Time().Equal(expectedExpiresAt) {
		return
	}
	record.Set("oauth_refresh_error", message)
	_ = app.Save(record)
}

// decryptRecordField decrypts a single AES-GCM-encrypted field on a
// repository_keys record, returning (nil, nil) if the field is unset.
func decryptRecordField(record *core.Record, field string, secretKey []byte) ([]byte, error) {
	value := record.GetString(field)
	if value == "" {
		return nil, nil
	}
	if len(secretKey) != 32 {
		return nil, fmt.Errorf("SECRET_KEY must be exactly 32 bytes")
	}
	plain, err := crypto.Decrypt(value, secretKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: %w", field, err)
	}
	return plain, nil
}
