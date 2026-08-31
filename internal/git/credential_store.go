package git

import (
	"context"
	"fmt"
	"os"
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

	result, err, _ := refreshGroup.Do(record.Id, func() (any, error) {
		return doRefreshOAuthToken(ctx, app, record.Id, provider, secretKey)
	})
	if err != nil {
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

	current, err := decryptRecordField(record, "oauth_token", secretKey)
	if err != nil {
		return "", err
	}

	encryptedRefresh := record.GetString("oauth_refresh_token")
	if encryptedRefresh == "" {
		// No refresh token on file: nothing we can do proactively, let the
		// stale token fail naturally against the provider's API.
		return string(current), nil
	}
	refreshTokenBytes, err := decryptRecordField(record, "oauth_refresh_token", secretKey)
	if err != nil {
		return "", err
	}

	newToken, err := provider.RefreshToken(ctx, string(refreshTokenBytes))
	if err != nil {
		return "", fmt.Errorf("refresh %s oauth token: %w", provider.Slug(), err)
	}

	encryptedAccess, err := crypto.Encrypt([]byte(newToken.AccessToken), secretKey)
	if err != nil {
		return "", fmt.Errorf("encrypt refreshed oauth_token: %w", err)
	}
	record.Set("oauth_token", encryptedAccess)
	if newToken.RefreshToken != "" {
		encryptedNewRefresh, err := crypto.Encrypt([]byte(newToken.RefreshToken), secretKey)
		if err != nil {
			return "", fmt.Errorf("encrypt refreshed oauth_refresh_token: %w", err)
		}
		record.Set("oauth_refresh_token", encryptedNewRefresh)
	}
	if !newToken.ExpiresAt.IsZero() {
		record.Set("oauth_token_expires_at", newToken.ExpiresAt)
	}
	if newToken.AccountLogin != "" {
		record.Set("oauth_account_login", newToken.AccountLogin)
	}
	if err := app.Save(record); err != nil {
		return "", fmt.Errorf("persist refreshed oauth token: %w", err)
	}

	return newToken.AccessToken, nil
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
