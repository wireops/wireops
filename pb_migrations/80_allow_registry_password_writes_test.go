package pb_migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/wireops/wireops/internal/crypto"
)

func TestMigration80PreservesCredentialsAndPermissions(t *testing.T) {
	app, collection := newRegistryHostMigrationTestApp(t)
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	collection.Fields.GetByName("password").SetHidden(true)
	collection.ListRule = strPtr(rbacReadRule)
	collection.ViewRule = strPtr(rbacReadRule)
	collection.CreateRule = strPtr(rbacOperatorRule)
	collection.UpdateRule = strPtr(rbacOperatorRule)
	collection.DeleteRule = strPtr(rbacOperatorRule)
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := crypto.Encrypt([]byte("example-existing-credential"), []byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("name", "Existing")
	record.Set("registry_url", "https://registry.example")
	record.Set("password", ciphertext)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	var migration *core.Migration
	for _, candidate := range core.AppMigrations.Items() {
		if candidate.File == "80_allow_registry_password_writes.go" {
			migration = candidate
		}
	}
	if migration == nil {
		t.Fatal("registry password migration is missing")
	}
	for _, phase := range []struct {
		name   string
		run    func(core.App) error
		hidden bool
	}{{"Up", migration.Up, false}, {"Replay", migration.Up, false}, {"Rollback", migration.Down, true}, {"UpgradeAgain", migration.Up, false}} {
		t.Run(phase.name, func(t *testing.T) {
			if err := phase.run(app); err != nil {
				t.Fatal(err)
			}
			collection, err := app.FindCollectionByNameOrId("registry_credentials")
			if err != nil {
				t.Fatal(err)
			}
			if collection.Fields.GetByName("password").GetHidden() != phase.hidden {
				t.Fatal("unexpected password write policy")
			}
			if *collection.ListRule != rbacReadRule || *collection.ViewRule != rbacReadRule || *collection.CreateRule != rbacOperatorRule || *collection.UpdateRule != rbacOperatorRule || *collection.DeleteRule != rbacOperatorRule {
				t.Fatal("migration changed credential access rules")
			}
			reloaded, err := app.FindRecordById(collection, record.Id)
			if err != nil || reloaded.GetString("password") != ciphertext || reloaded.GetString("name") != "Existing" {
				t.Fatal("migration changed existing credential data")
			}
		})
	}
	collection.Fields.RemoveByName("password")
	if err := app.Save(collection); err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(app); err == nil {
		t.Fatal("migration silently accepted a missing password field")
	}
}
