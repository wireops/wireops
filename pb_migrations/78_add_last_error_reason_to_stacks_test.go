package pb_migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

const migration78File = "78_add_last_error_reason_to_stacks.go"

func migration78(t *testing.T) *core.Migration {
	t.Helper()
	for _, item := range core.AppMigrations.Items() {
		if item.File == migration78File {
			return item
		}
	}
	t.Fatalf("migration %q not found", migration78File)
	return nil
}

func newLastErrorReasonMigrationTestApp(t *testing.T) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir(), EncryptionEnv: "pb_last_error_reason_migration_test"})
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

func TestMigration78AddsAndRemovesLastErrorReason(t *testing.T) {
	app := newLastErrorReasonMigrationTestApp(t)
	migration := migration78(t)
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
	if stacks.Fields.GetByName("last_error_reason") == nil {
		t.Fatal("last_error_reason field was not added")
	}

	record := core.NewRecord(stacks)
	record.Set("name", "example")
	record.Set("last_error_reason", "timeout")
	if err := app.Save(record); err != nil {
		t.Fatalf("saving last_error_reason: %v", err)
	}

	// Empty is the "not known" case every pre-existing row carries, so it has
	// to stay savable alongside the four real values.
	record.Set("last_error_reason", "")
	if err := app.Save(record); err != nil {
		t.Fatalf("clearing last_error_reason: %v", err)
	}

	// A value outside the option set must be rejected — the whole point of a
	// select field here is that a caller can branch on it.
	record.Set("last_error_reason", "not-a-reason")
	if err := app.Save(record); err == nil {
		t.Fatal("saving an unlisted last_error_reason succeeded, want a validation error")
	}

	if err := migration.Down(app); err != nil {
		t.Fatalf("down(): %v", err)
	}
	stacks, err = app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatal(err)
	}
	if stacks.Fields.GetByName("last_error_reason") != nil {
		t.Fatal("last_error_reason field was not removed")
	}
}
