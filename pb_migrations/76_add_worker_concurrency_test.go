package pb_migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

const migration76File = "76_add_worker_concurrency.go"

func migration76(t *testing.T) *core.Migration {
	t.Helper()
	for _, item := range core.AppMigrations.Items() {
		if item.File == migration76File {
			return item
		}
	}
	t.Fatalf("migration %q not found in core.AppMigrations", migration76File)
	return nil
}

func newWorkerConcurrencyMigrationTestApp(t *testing.T) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       t.TempDir(),
		EncryptionEnv: "pb_worker_concurrency_migration_test",
	})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	workers := core.NewBaseCollection("workers")
	workers.Fields.Add(&core.TextField{Name: "hostname"})
	if err := app.Save(workers); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestMigration76AddsConcurrencyFields(t *testing.T) {
	app := newWorkerConcurrencyMigrationTestApp(t)
	if err := migration76(t).Up(app); err != nil {
		t.Fatalf("up(): %v", err)
	}

	workers, err := app.FindCollectionByNameOrId("workers")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range workerConcurrencyFields {
		if workers.Fields.GetByName(name) == nil {
			t.Fatalf("migration did not add the %q field", name)
		}
	}

	record := core.NewRecord(workers)
	record.Set("hostname", "worker-a")
	record.Set("heavy_concurrency", 3)
	record.Set("active_heavy", 1)
	record.Set("queued_heavy", 0)
	if err := app.Save(record); err != nil {
		t.Fatalf("saving a worker with concurrency values failed: %v", err)
	}
	if got := record.GetInt("heavy_concurrency"); got != 3 {
		t.Fatalf("heavy_concurrency = %d, want 3", got)
	}
}

// Re-running up() must be a no-op: the fields may already exist on a
// database that was migrated before.
func TestMigration76UpIsIdempotent(t *testing.T) {
	app := newWorkerConcurrencyMigrationTestApp(t)
	migration := migration76(t)
	if err := migration.Up(app); err != nil {
		t.Fatal(err)
	}
	if err := migration.Up(app); err != nil {
		t.Fatalf("second up(): %v", err)
	}
	workers, _ := app.FindCollectionByNameOrId("workers")
	count := 0
	for _, f := range workers.Fields {
		if f.GetName() == "heavy_concurrency" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("heavy_concurrency defined %d times, want 1", count)
	}
}

func TestMigration76DownRemovesFields(t *testing.T) {
	app := newWorkerConcurrencyMigrationTestApp(t)
	migration := migration76(t)
	if err := migration.Up(app); err != nil {
		t.Fatal(err)
	}
	if err := migration.Down(app); err != nil {
		t.Fatalf("down(): %v", err)
	}
	workers, _ := app.FindCollectionByNameOrId("workers")
	for _, name := range workerConcurrencyFields {
		if workers.Fields.GetByName(name) != nil {
			t.Fatalf("rollback left the %q field behind", name)
		}
	}
}
