package pb_migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func TestMigrateReusableRepositoryKeys(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       t.TempDir(),
		EncryptionEnv: "pb_repository_keys_migration_test",
	})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap app: %v", err)
	}

	repositories := core.NewBaseCollection("repositories")
	repositories.Fields.Add(&core.TextField{Name: "name"})
	if err := app.Save(repositories); err != nil {
		t.Fatalf("save repositories collection: %v", err)
	}
	keys := core.NewBaseCollection("repository_keys")
	keys.Fields.Add(&core.RelationField{Name: "repository", CollectionId: repositories.Id, Required: true, MaxSelect: 1})
	keys.Fields.Add(&core.SelectField{Name: "auth_type", Values: []string{"none", "ssh_key", "basic"}})
	keys.Fields.Add(&core.TextField{Name: "git_password"})
	keys.Indexes = append(keys.Indexes, repositoryKeysRepositoryIndexSQL)
	if err := app.Save(keys); err != nil {
		t.Fatalf("save keys collection: %v", err)
	}

	privateRepository := core.NewRecord(repositories)
	privateRepository.Set("name", "Private API")
	if err := app.Save(privateRepository); err != nil {
		t.Fatalf("save private repository: %v", err)
	}
	publicRepository := core.NewRecord(repositories)
	publicRepository.Set("name", "Public UI")
	if err := app.Save(publicRepository); err != nil {
		t.Fatalf("save public repository: %v", err)
	}
	privateKey := core.NewRecord(keys)
	privateKey.Set("repository", privateRepository.Id)
	privateKey.Set("auth_type", "basic")
	privateKey.Set("git_password", "encrypted-value")
	if err := app.Save(privateKey); err != nil {
		t.Fatalf("save private key: %v", err)
	}
	publicKey := core.NewRecord(keys)
	publicKey.Set("repository", publicRepository.Id)
	publicKey.Set("auth_type", "none")
	if err := app.Save(publicKey); err != nil {
		t.Fatalf("save public key: %v", err)
	}

	app.OnRecordUpdate("repository_keys").BindFunc(func(e *core.RecordEvent) error {
		t.Fatal("migration must not trigger repository key update hooks")
		return e.Next()
	})
	app.OnRecordDelete("repository_keys").BindFunc(func(e *core.RecordEvent) error {
		t.Fatal("migration must not trigger repository key delete hooks")
		return e.Next()
	})

	if err := migrateReusableRepositoryKeys(app); err != nil {
		t.Fatalf("migrate reusable keys: %v", err)
	}

	migratedRepository, err := app.FindRecordById("repositories", privateRepository.Id)
	if err != nil {
		t.Fatalf("find migrated repository: %v", err)
	}
	if migratedRepository.GetString("repository_key") != privateKey.Id {
		t.Fatalf("repository_key = %q, want %q", migratedRepository.GetString("repository_key"), privateKey.Id)
	}
	migratedKey, err := app.FindRecordById("repository_keys", privateKey.Id)
	if err != nil {
		t.Fatalf("find migrated key: %v", err)
	}
	if migratedKey.GetString("name") != "Private API credentials" {
		t.Fatalf("key name = %q", migratedKey.GetString("name"))
	}
	if migratedKey.GetString("git_password") != "encrypted-value" {
		t.Fatalf("encrypted password changed during migration")
	}
	if _, err := app.FindRecordById("repository_keys", publicKey.Id); err == nil {
		t.Fatal("public auth placeholder key was not removed")
	}

	migratedKeys, err := app.FindCollectionByNameOrId("repository_keys")
	if err != nil {
		t.Fatalf("find migrated keys collection: %v", err)
	}
	if migratedKeys.Fields.GetByName("repository") != nil {
		t.Fatal("legacy repository field still exists")
	}
	nameField, ok := migratedKeys.Fields.GetByName("name").(*core.TextField)
	if !ok || !nameField.Required {
		t.Fatal("name field is not required")
	}
}
