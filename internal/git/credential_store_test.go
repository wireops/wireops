package git

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/gitprovider"
)

// fakeOAuthProvider is a minimal gitprovider.Provider used to exercise the
// oauth_token branch of LoadCredentialByID/LoadOAuthToken without depending
// on the real GitHub implementation or network access.
type fakeOAuthProvider struct{}

func (fakeOAuthProvider) Slug() string                    { return "fake-provider" }
func (fakeOAuthProvider) Name() string                    { return "Fake Provider" }
func (fakeOAuthProvider) Configured() bool                { return true }
func (fakeOAuthProvider) BasicAuthUsername() string       { return "x-access-token" }
func (fakeOAuthProvider) AuthorizeURL(_, _ string) string { return "" }
func (fakeOAuthProvider) ExchangeCode(_ context.Context, _, _ string) (*gitprovider.Token, error) {
	return nil, nil
}
func (fakeOAuthProvider) RefreshToken(_ context.Context, _ string) (*gitprovider.Token, error) {
	return nil, gitprovider.ErrRefreshNotSupported
}
func (fakeOAuthProvider) ListOrganizations(_ context.Context, _ string) ([]gitprovider.Org, error) {
	return nil, nil
}
func (fakeOAuthProvider) ListRepositories(_ context.Context, _, _ string) ([]gitprovider.Repo, error) {
	return nil, nil
}
func (fakeOAuthProvider) ListBranches(_ context.Context, _, _ string) ([]gitprovider.Branch, error) {
	return nil, nil
}

var registerFakeProviderOnce sync.Once

func registerFakeProvider() {
	registerFakeProviderOnce.Do(func() {
		gitprovider.Register(fakeOAuthProvider{})
	})
}

func TestLoadRepositoryCredentialWithoutKeyUsesPublicAuth(t *testing.T) {
	app, repositories, _ := newCredentialStoreTestApp(t)
	repository := core.NewRecord(repositories)
	repository.Set("name", "public")
	if err := app.Save(repository); err != nil {
		t.Fatalf("save repository: %v", err)
	}

	credential, err := LoadRepositoryCredential(context.Background(), app, repository.Id)
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}
	if credential.AuthType != AuthTypeNone {
		t.Fatalf("auth type = %q, want %q", credential.AuthType, AuthTypeNone)
	}
}

func TestLoadRepositoryCredentialDecryptsReusableBasicKey(t *testing.T) {
	app, repositories, keys := newCredentialStoreTestApp(t)
	secret := "0123456789abcdef0123456789abcdef"
	t.Setenv("SECRET_KEY", secret)
	encrypted, err := crypto.Encrypt([]byte("token-value"), []byte(secret))
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}

	key := core.NewRecord(keys)
	key.Set("name", "GitHub")
	key.Set("auth_type", "basic")
	key.Set("git_username", "git-user")
	key.Set("git_password", encrypted)
	if err := app.Save(key); err != nil {
		t.Fatalf("save key: %v", err)
	}
	repository := core.NewRecord(repositories)
	repository.Set("name", "private")
	repository.Set("repository_key", key.Id)
	if err := app.Save(repository); err != nil {
		t.Fatalf("save repository: %v", err)
	}

	credential, err := LoadRepositoryCredential(context.Background(), app, repository.Id)
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}
	if credential.GitUsername != "git-user" || credential.GitPassword != "token-value" {
		t.Fatalf("unexpected basic credential: %#v", credential)
	}
}

func newCredentialStoreTestApp(t *testing.T) (*tests.TestApp, *core.Collection, *core.Collection) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	keys := core.NewBaseCollection("repository_keys")
	keys.Fields.Add(&core.TextField{Name: "name"})
	keys.Fields.Add(&core.TextField{Name: "auth_type"})
	keys.Fields.Add(&core.TextField{Name: "git_username"})
	keys.Fields.Add(&core.TextField{Name: "git_password"})
	keys.Fields.Add(&core.TextField{Name: "oauth_provider"})
	keys.Fields.Add(&core.TextField{Name: "oauth_token"})
	keys.Fields.Add(&core.TextField{Name: "oauth_refresh_token"})
	keys.Fields.Add(&core.TextField{Name: "oauth_account_login"})
	keys.Fields.Add(&core.DateField{Name: "oauth_token_expires_at"})
	if err := app.Save(keys); err != nil {
		t.Fatalf("save keys collection: %v", err)
	}
	repositories := core.NewBaseCollection("repositories")
	repositories.Fields.Add(&core.TextField{Name: "name"})
	repositories.Fields.Add(&core.RelationField{Name: "repository_key", CollectionId: keys.Id, MaxSelect: 1})
	if err := app.Save(repositories); err != nil {
		t.Fatalf("save repositories collection: %v", err)
	}
	return app, repositories, keys
}

func TestLoadCredentialByIDOAuthToken(t *testing.T) {
	registerFakeProvider()
	secret := "0123456789abcdef0123456789abcdef"

	newKey := func(t *testing.T, app *tests.TestApp, keys *core.Collection, provider, plainToken string) *core.Record {
		t.Helper()
		key := core.NewRecord(keys)
		key.Set("name", "OAuth key")
		key.Set("auth_type", string(AuthTypeOAuthToken))
		key.Set("oauth_provider", provider)
		if plainToken != "" {
			encrypted, err := crypto.Encrypt([]byte(plainToken), []byte(secret))
			if err != nil {
				t.Fatalf("encrypt token: %v", err)
			}
			key.Set("oauth_token", encrypted)
		}
		if err := app.Save(key); err != nil {
			t.Fatalf("save key: %v", err)
		}
		return key
	}

	t.Run("decrypts and downgrades a valid oauth token to basic auth", func(t *testing.T) {
		app, _, keys := newCredentialStoreTestApp(t)
		t.Setenv("SECRET_KEY", secret)
		key := newKey(t, app, keys, "fake-provider", "gho_token-value")

		credential, err := LoadCredentialByID(context.Background(), app, key.Id)
		if err != nil {
			t.Fatalf("load credential: %v", err)
		}
		if credential.AuthType != AuthTypeBasic {
			t.Fatalf("auth type = %q, want %q", credential.AuthType, AuthTypeBasic)
		}
		if credential.GitUsername != "x-access-token" || credential.GitPassword != "gho_token-value" {
			t.Fatalf("unexpected oauth-derived credential: %#v", credential)
		}
	})

	t.Run("errors on an unknown provider", func(t *testing.T) {
		app, _, keys := newCredentialStoreTestApp(t)
		t.Setenv("SECRET_KEY", secret)
		key := newKey(t, app, keys, "does-not-exist", "gho_token-value")

		if _, err := LoadCredentialByID(context.Background(), app, key.Id); err == nil {
			t.Fatal("expected error for unknown git provider, got nil")
		}
	})

	t.Run("errors when the stored token is empty", func(t *testing.T) {
		app, _, keys := newCredentialStoreTestApp(t)
		t.Setenv("SECRET_KEY", secret)
		key := newKey(t, app, keys, "fake-provider", "")

		if _, err := LoadCredentialByID(context.Background(), app, key.Id); err == nil {
			t.Fatal("expected error for missing oauth token, got nil")
		}
	})
}

func TestLoadOAuthToken(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"

	t.Run("decrypts a stored token", func(t *testing.T) {
		app, _, keys := newCredentialStoreTestApp(t)
		t.Setenv("SECRET_KEY", secret)
		encrypted, err := crypto.Encrypt([]byte("gho_token-value"), []byte(secret))
		if err != nil {
			t.Fatalf("encrypt token: %v", err)
		}
		key := core.NewRecord(keys)
		key.Set("name", "OAuth key")
		key.Set("auth_type", string(AuthTypeOAuthToken))
		key.Set("oauth_provider", "fake-provider")
		key.Set("oauth_token", encrypted)
		if err := app.Save(key); err != nil {
			t.Fatalf("save key: %v", err)
		}

		provider, token, err := LoadOAuthToken(context.Background(), app, key.Id)
		if err != nil {
			t.Fatalf("load oauth token: %v", err)
		}
		if provider != "fake-provider" || token != "gho_token-value" {
			t.Fatalf("unexpected oauth token result: provider=%q token=%q", provider, token)
		}
	})

	t.Run("errors when the key is not an oauth credential", func(t *testing.T) {
		app, _, keys := newCredentialStoreTestApp(t)
		t.Setenv("SECRET_KEY", secret)
		key := core.NewRecord(keys)
		key.Set("name", "Basic key")
		key.Set("auth_type", string(AuthTypeBasic))
		if err := app.Save(key); err != nil {
			t.Fatalf("save key: %v", err)
		}

		if _, _, err := LoadOAuthToken(context.Background(), app, key.Id); err == nil {
			t.Fatal("expected error for non-oauth key, got nil")
		}
	})

	t.Run("errors when no oauth token is stored", func(t *testing.T) {
		app, _, keys := newCredentialStoreTestApp(t)
		t.Setenv("SECRET_KEY", secret)
		key := core.NewRecord(keys)
		key.Set("name", "OAuth key")
		key.Set("auth_type", string(AuthTypeOAuthToken))
		key.Set("oauth_provider", "fake-provider")
		if err := app.Save(key); err != nil {
			t.Fatalf("save key: %v", err)
		}

		if _, _, err := LoadOAuthToken(context.Background(), app, key.Id); err == nil {
			t.Fatal("expected error for missing oauth token, got nil")
		}
	})
}

// countingRefreshProvider is a gitprovider.Provider whose RefreshToken counts
// invocations and sleeps briefly, widening the window for concurrent callers
// to race on the same refresh_token if the caller doesn't serialize them.
type countingRefreshProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *countingRefreshProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *countingRefreshProvider) Slug() string                    { return "counting-provider" }
func (p *countingRefreshProvider) Name() string                    { return "Counting Provider" }
func (p *countingRefreshProvider) Configured() bool                { return true }
func (p *countingRefreshProvider) BasicAuthUsername() string       { return "oauth2" }
func (p *countingRefreshProvider) AuthorizeURL(_, _ string) string { return "" }
func (p *countingRefreshProvider) ExchangeCode(_ context.Context, _, _ string) (*gitprovider.Token, error) {
	return nil, nil
}
func (p *countingRefreshProvider) RefreshToken(_ context.Context, refreshToken string) (*gitprovider.Token, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if refreshToken == "" {
		return nil, gitprovider.ErrRefreshNotSupported
	}
	time.Sleep(20 * time.Millisecond)
	return &gitprovider.Token{
		AccessToken:  "refreshed-token",
		RefreshToken: "refreshed-refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		AccountLogin: "octocat",
	}, nil
}
func (p *countingRefreshProvider) ListOrganizations(_ context.Context, _ string) ([]gitprovider.Org, error) {
	return nil, nil
}
func (p *countingRefreshProvider) ListRepositories(_ context.Context, _, _ string) ([]gitprovider.Repo, error) {
	return nil, nil
}
func (p *countingRefreshProvider) ListBranches(_ context.Context, _, _ string) ([]gitprovider.Branch, error) {
	return nil, nil
}

// TestLoadCredentialByIDDedupesConcurrentRefresh guards against the race
// where several repos sharing one provider's credential all notice the same
// expired token at once: without singleflight, each would redeem the same
// refresh_token independently, and since GitLab rotates refresh tokens on
// use, all but one redeem — and any Save() that lands after a later one —
// would corrupt the stored credential. Every concurrent caller here must
// observe exactly one RefreshToken call and the same refreshed access token.
func TestLoadCredentialByIDDedupesConcurrentRefresh(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	app, _, keys := newCredentialStoreTestApp(t)
	t.Setenv("SECRET_KEY", secret)

	provider := &countingRefreshProvider{}
	gitprovider.Register(provider)

	encryptedAccess, err := crypto.Encrypt([]byte("stale-token"), []byte(secret))
	if err != nil {
		t.Fatalf("encrypt access token: %v", err)
	}
	encryptedRefresh, err := crypto.Encrypt([]byte("stale-refresh"), []byte(secret))
	if err != nil {
		t.Fatalf("encrypt refresh token: %v", err)
	}

	key := core.NewRecord(keys)
	key.Set("name", "OAuth key")
	key.Set("auth_type", string(AuthTypeOAuthToken))
	key.Set("oauth_provider", provider.Slug())
	key.Set("oauth_token", encryptedAccess)
	key.Set("oauth_refresh_token", encryptedRefresh)
	key.Set("oauth_token_expires_at", time.Now().Add(-time.Minute))
	if err := app.Save(key); err != nil {
		t.Fatalf("save key: %v", err)
	}

	const concurrency = 20
	var wg sync.WaitGroup
	results := make([]string, concurrency)
	errs := make([]error, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cred, err := LoadCredentialByID(context.Background(), app, key.Id)
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = cred.GitPassword
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}
	for i, got := range results {
		if got != "refreshed-token" {
			t.Fatalf("goroutine %d: got access token %q, want %q", i, got, "refreshed-token")
		}
	}
	if got := provider.callCount(); got != 1 {
		t.Fatalf("RefreshToken called %d times, want exactly 1", got)
	}
}
