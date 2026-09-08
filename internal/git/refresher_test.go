package git

import (
	"context"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/gitprovider"
)

// sweepRefreshProvider is a gitprovider.Provider used only by
// TestRefreshExpiringOAuthTokens, kept separate from the other fake
// providers in this package since gitprovider.Register panics on a
// duplicate slug within the same test binary.
type sweepRefreshProvider struct{}

func (sweepRefreshProvider) Slug() string                    { return "sweep-refresh-provider" }
func (sweepRefreshProvider) Name() string                    { return "Sweep Refresh Provider" }
func (sweepRefreshProvider) Configured() bool                { return true }
func (sweepRefreshProvider) BasicAuthUsername() string       { return "oauth2" }
func (sweepRefreshProvider) AuthorizeURL(_, _ string) string { return "" }
func (sweepRefreshProvider) ExchangeCode(_ context.Context, _, _ string) (*gitprovider.Token, error) {
	return nil, nil
}
func (sweepRefreshProvider) RefreshToken(_ context.Context, _ string) (*gitprovider.Token, error) {
	return &gitprovider.Token{
		AccessToken:  "swept-token",
		RefreshToken: "swept-refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		AccountLogin: "octocat",
	}, nil
}
func (sweepRefreshProvider) ListOrganizations(_ context.Context, _ string) ([]gitprovider.Org, error) {
	return nil, nil
}
func (sweepRefreshProvider) ListRepositories(_ context.Context, _, _ string) ([]gitprovider.Repo, error) {
	return nil, nil
}
func (sweepRefreshProvider) ListBranches(_ context.Context, _, _ string) ([]gitprovider.Branch, error) {
	return nil, nil
}

// TestRefreshExpiringOAuthTokens exercises the cron-driven sweep wired up in
// cmd/serve.go: a token within its expiry grace window must be refreshed, an
// unrelated basic-auth key must be left untouched, and a token that isn't
// due yet must not trigger a needless RefreshToken call.
func TestRefreshExpiringOAuthTokens(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	app, _, keys := newCredentialStoreTestApp(t)
	t.Setenv("SECRET_KEY", secret)

	provider := sweepRefreshProvider{}
	gitprovider.Register(provider)

	encryptedAccess, err := crypto.Encrypt([]byte("due-token"), []byte(secret))
	if err != nil {
		t.Fatalf("encrypt access token: %v", err)
	}
	encryptedRefresh, err := crypto.Encrypt([]byte("due-refresh"), []byte(secret))
	if err != nil {
		t.Fatalf("encrypt refresh token: %v", err)
	}

	due := core.NewRecord(keys)
	due.Set("name", "Due key")
	due.Set("auth_type", string(AuthTypeOAuthToken))
	due.Set("oauth_provider", provider.Slug())
	due.Set("oauth_token", encryptedAccess)
	due.Set("oauth_refresh_token", encryptedRefresh)
	due.Set("oauth_token_expires_at", time.Now().Add(5*time.Minute)) // inside the 15m grace window
	if err := app.Save(due); err != nil {
		t.Fatalf("save due key: %v", err)
	}

	notDue := core.NewRecord(keys)
	notDue.Set("name", "Not due key")
	notDue.Set("auth_type", string(AuthTypeOAuthToken))
	notDue.Set("oauth_provider", provider.Slug())
	notDue.Set("oauth_token", encryptedAccess)
	notDue.Set("oauth_refresh_token", encryptedRefresh)
	notDue.Set("oauth_token_expires_at", time.Now().Add(time.Hour))
	if err := app.Save(notDue); err != nil {
		t.Fatalf("save not-due key: %v", err)
	}

	basicKey := core.NewRecord(keys)
	basicKey.Set("name", "Basic key")
	basicKey.Set("auth_type", string(AuthTypeBasic))
	basicKey.Set("git_username", "git-user")
	if err := app.Save(basicKey); err != nil {
		t.Fatalf("save basic key: %v", err)
	}

	RefreshExpiringOAuthTokens(context.Background(), app)

	reloadedDue, err := app.FindRecordById("repository_keys", due.Id)
	if err != nil {
		t.Fatalf("reload due key: %v", err)
	}
	if reloadedDue.GetString("oauth_token") == encryptedAccess {
		t.Fatal("expected the due-for-refresh key's oauth_token to have been rotated by the sweep")
	}
	if reloadedDue.GetDateTime("oauth_last_refresh_at").Time().IsZero() {
		t.Fatal("expected oauth_last_refresh_at to be set on the swept key")
	}

	reloadedNotDue, err := app.FindRecordById("repository_keys", notDue.Id)
	if err != nil {
		t.Fatalf("reload not-due key: %v", err)
	}
	if reloadedNotDue.GetString("oauth_token") != encryptedAccess {
		t.Fatal("expected the not-yet-due key's oauth_token to be left untouched by the sweep")
	}

	// The sweep filters on auth_type = oauth_token, so a basic-auth key must
	// not even be considered; reaching here without a panic/lookup error on
	// its (nonexistent) oauth fields is the assertion.
	reloadedBasic, err := app.FindRecordById("repository_keys", basicKey.Id)
	if err != nil {
		t.Fatalf("reload basic key: %v", err)
	}
	if reloadedBasic.GetString("git_username") != "git-user" {
		t.Fatal("expected the basic-auth key to be left untouched by the sweep")
	}
}
