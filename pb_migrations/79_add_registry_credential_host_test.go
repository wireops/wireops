package pb_migrations

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

const migration79File = "79_add_registry_credential_host.go"

func migration79(t *testing.T) *core.Migration {
	t.Helper()
	for _, item := range core.AppMigrations.Items() {
		if item.File == migration79File {
			return item
		}
	}
	t.Fatalf("migration %q not found", migration79File)
	return nil
}

func newRegistryHostMigrationTestApp(t *testing.T) (core.App, *core.Collection) {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir(), EncryptionEnv: "pb_registry_host_migration_test"})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	credentials := core.NewBaseCollection("registry_credentials")
	credentials.Fields.Add(&core.TextField{Name: "name"})
	credentials.Fields.Add(&core.TextField{Name: "registry_url"})
	credentials.Fields.Add(&core.TextField{Name: "password"})
	if err := app.Save(credentials); err != nil {
		t.Fatal(err)
	}
	return app, credentials
}

func TestMigration79BackfillsRegistryHostAndPreventsDuplicates(t *testing.T) {
	app, credentials := newRegistryHostMigrationTestApp(t)
	credential := core.NewRecord(credentials)
	credential.Set("name", "Private")
	credential.Set("registry_url", "HTTPS://REGISTRY.EXAMPLE.COM:5000/v2/")
	credential.Set("password", "already-encrypted-ciphertext")
	if err := app.Save(credential); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	migration := migration79(t)
	if err := migration.Up(app); err != nil {
		t.Fatalf("up(): %v", err)
	}
	if err := migration.Up(app); err != nil {
		t.Fatalf("second up(): %v", err)
	}

	credentials, err := app.FindCollectionByNameOrId("registry_credentials")
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Fields.GetByName("registry_host") == nil {
		t.Fatal("registry_host field was not added")
	}
	reloaded, err := app.FindRecordById("registry_credentials", credential.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.GetString("registry_host"); got != "registry.example.com:5000" {
		t.Fatalf("registry_host = %q, want registry.example.com:5000", got)
	}
	if got := reloaded.GetString("password"); got != "already-encrypted-ciphertext" {
		t.Fatalf("migration rewrote password = %q", got)
	}

	duplicate := core.NewRecord(credentials)
	duplicate.Set("name", "Duplicate")
	duplicate.Set("registry_url", "https://registry.example.com")
	duplicate.Set("registry_host", "registry.example.com:5000")
	if err := app.Save(duplicate); err == nil {
		t.Fatal("unique registry_host index allowed a duplicate")
	}

	if err := migration.Down(app); err != nil {
		t.Fatalf("down(): %v", err)
	}
	credentials, err = app.FindCollectionByNameOrId("registry_credentials")
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Fields.GetByName("registry_host") != nil {
		t.Fatal("registry_host field was not removed")
	}
}

func TestMigration79RejectsExistingDuplicateRegistryHosts(t *testing.T) {
	app, credentials := newRegistryHostMigrationTestApp(t)
	for _, input := range []struct{ name, url string }{
		{"First", "https://registry.example.com/v2"},
		{"Second", "REGISTRY.EXAMPLE.COM"},
	} {
		record := core.NewRecord(credentials)
		record.Set("name", input.name)
		record.Set("registry_url", input.url)
		if err := app.Save(record); err != nil {
			t.Fatalf("save %s: %v", input.name, err)
		}
	}

	err := migration79(t).Up(app)
	if err == nil {
		t.Fatal("migration accepted duplicate normalized registry hosts")
	}
	if !strings.Contains(err.Error(), "registry.example.com") || !strings.Contains(err.Error(), "First") || !strings.Contains(err.Error(), "Second") {
		t.Fatalf("duplicate migration error should identify the affected records, got %v", err)
	}

	credentials, err = app.FindCollectionByNameOrId("registry_credentials")
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Fields.GetByName("registry_host") == nil {
		t.Fatal("registry_host field should remain available for migration recovery")
	}
	records, err := app.FindAllRecords("registry_credentials")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("migration changed credential rows while rejecting duplicates: got %d, want 2", len(records))
	}
}
