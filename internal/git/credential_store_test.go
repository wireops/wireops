package git

import (
	"context"
	"sync"
	"testing"

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

	credential, err := LoadRepositoryCredential(app, repository.Id)
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

	credential, err := LoadRepositoryCredential(app, repository.Id)
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

		credential, err := LoadCredentialByID(app, key.Id)
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

		if _, err := LoadCredentialByID(app, key.Id); err == nil {
			t.Fatal("expected error for unknown git provider, got nil")
		}
	})

	t.Run("errors when the stored token is empty", func(t *testing.T) {
		app, _, keys := newCredentialStoreTestApp(t)
		t.Setenv("SECRET_KEY", secret)
		key := newKey(t, app, keys, "fake-provider", "")

		if _, err := LoadCredentialByID(app, key.Id); err == nil {
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

		provider, token, err := LoadOAuthToken(app, key.Id)
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

		if _, _, err := LoadOAuthToken(app, key.Id); err == nil {
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

		if _, _, err := LoadOAuthToken(app, key.Id); err == nil {
			t.Fatal("expected error for missing oauth token, got nil")
		}
	})
}
