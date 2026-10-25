package pb_migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

const migration77File = "77_add_last_error_commit_to_stacks.go"

func migration77(t *testing.T) *core.Migration {
	t.Helper()
	for _, item := range core.AppMigrations.Items() {
		if item.File == migration77File {
			return item
		}
	}
	t.Fatalf("migration %q not found", migration77File)
	return nil
}

func newLastErrorCommitMigrationTestApp(t *testing.T) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir(), EncryptionEnv: "pb_last_error_commit_migration_test"})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	if err := app.Save(stacks); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestMigration77AddsAndRemovesLastErrorCommit(t *testing.T) {
	app := newLastErrorCommitMigrationTestApp(t)
	migration := migration77(t)
	if err := migration.Up(app); err != nil {
		t.Fatalf("up(): %v", err)
	}
	// Up is safe to replay when a database was migrated before an interrupted
	// startup completed.
	if err := migration.Up(app); err != nil {
		t.Fatalf("second up(): %v", err)
	}
	stacks, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatal(err)
	}
	if stacks.Fields.GetByName("last_error_commit") == nil {
		t.Fatal("last_error_commit field was not added")
	}
	record := core.NewRecord(stacks)
	record.Set("name", "example")
	record.Set("last_error_commit", "attempted-sha")
	if err := app.Save(record); err != nil {
		t.Fatalf("saving last_error_commit: %v", err)
	}
	if err := migration.Down(app); err != nil {
		t.Fatalf("down(): %v", err)
	}
	stacks, err = app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatal(err)
	}
	if stacks.Fields.GetByName("last_error_commit") != nil {
		t.Fatal("last_error_commit field was not removed")
	}
}
