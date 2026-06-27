package git

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/crypto"
)

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
