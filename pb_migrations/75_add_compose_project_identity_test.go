package pb_migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

const migration75File = "75_add_compose_project_identity.go"

func migration75(t *testing.T) *core.Migration {
	t.Helper()
	for _, item := range core.AppMigrations.Items() {
		if item.File == migration75File {
			return item
		}
	}
	t.Fatalf("migration %q not found in core.AppMigrations", migration75File)
	return nil
}

func newProjectIdentityMigrationTestApp(t *testing.T) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       t.TempDir(),
		EncryptionEnv: "pb_project_identity_migration_test",
	})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	workers := core.NewBaseCollection("workers")
	workers.Fields.Add(&core.TextField{Name: "hostname"})
	if err := app.Save(workers); err != nil {
		t.Fatal(err)
	}
	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	stacks.Fields.Add(&core.RelationField{Name: "worker", CollectionId: workers.Id, MaxSelect: 1})
	if err := app.Save(stacks); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestMigration75AddsFieldsAndWorkerScopedUniqueIndex(t *testing.T) {
	app := newProjectIdentityMigrationTestApp(t)
	if err := migration75(t).Up(app); err != nil {
		t.Fatalf("up(): %v", err)
	}

	stacks, _ := app.FindCollectionByNameOrId("stacks")
	workers, _ := app.FindCollectionByNameOrId("workers")
	if stacks.Fields.GetByName("compose_project_name") == nil || workers.Fields.GetByName("capabilities") == nil {
		t.Fatal("migration did not add project identity fields")
	}
	workerA := core.NewRecord(workers)
	workerA.Set("hostname", "worker-a")
	if err := app.Save(workerA); err != nil {
		t.Fatal(err)
	}
	workerB := core.NewRecord(workers)
	workerB.Set("hostname", "worker-b")
	if err := app.Save(workerB); err != nil {
		t.Fatal(err)
	}

	createStack := func(name, workerID, project string) error {
		record := core.NewRecord(stacks)
		record.Set("name", name)
		record.Set("worker", workerID)
		record.Set("compose_project_name", project)
		return app.Save(record)
	}
	if err := createStack("first", workerA.Id, "stable-project"); err != nil {
		t.Fatal(err)
	}
	if err := createStack("same-host-collision", workerA.Id, "stable-project"); err == nil {
		t.Fatal("same worker accepted a duplicate non-empty Compose identity")
	}
	if err := createStack("other-host", workerB.Id, "stable-project"); err != nil {
		t.Fatalf("same identity on another worker should be allowed: %v", err)
	}
	if err := createStack("empty-one", workerA.Id, ""); err != nil {
		t.Fatal(err)
	}
	if err := createStack("empty-two", workerA.Id, ""); err != nil {
		t.Fatalf("partial index should allow multiple empty identities: %v", err)
	}
}

func TestMigration75DownRemovesFields(t *testing.T) {
	app := newProjectIdentityMigrationTestApp(t)
	migration := migration75(t)
	if err := migration.Up(app); err != nil {
		t.Fatal(err)
	}
	if err := migration.Down(app); err != nil {
		t.Fatalf("down(): %v", err)
	}
	stacks, _ := app.FindCollectionByNameOrId("stacks")
	workers, _ := app.FindCollectionByNameOrId("workers")
	if stacks.Fields.GetByName("compose_project_name") != nil || workers.Fields.GetByName("capabilities") != nil {
		t.Fatal("rollback did not remove project identity fields")
	}
}
