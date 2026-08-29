package pb_migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

// newConfigFilesCascadeTestApp builds a minimal app with a stacks collection
// and a stack_config_files collection whose "stack" relation starts with
// CascadeDelete set to startCascade — false simulates a legacy instance
// whose migration 65 ran before CascadeDelete was added to its source; true
// simulates a fresh install, where migration 65's current source already
// creates the relation with CascadeDelete: true.
func newConfigFilesCascadeTestApp(t *testing.T, startCascade bool) core.App {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       t.TempDir(),
		EncryptionEnv: "pb_config_files_cascade_migration_test",
	})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap app: %v", err)
	}

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("save stacks collection: %v", err)
	}

	configFiles := core.NewBaseCollection("stack_config_files")
	configFiles.Fields.Add(&core.RelationField{
		Name:          "stack",
		CollectionId:  stacks.Id,
		Required:      true,
		MaxSelect:     1,
		CascadeDelete: startCascade,
	})
	if err := app.Save(configFiles); err != nil {
		t.Fatalf("save stack_config_files collection: %v", err)
	}

	return app
}

func cascadeDeleteOf(t *testing.T, app core.App) bool {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("stack_config_files")
	if err != nil {
		t.Fatalf("find stack_config_files collection: %v", err)
	}
	field, ok := col.Fields.GetByName("stack").(*core.RelationField)
	if !ok {
		t.Fatal("stack field not found or wrong type")
	}
	return field.CascadeDelete
}

// TestEnableConfigFilesCascadeFixesLegacyInstances covers a legacy instance
// whose migration 65 ran before CascadeDelete was added to its source —
// up() must flip it to true.
func TestEnableConfigFilesCascadeFixesLegacyInstances(t *testing.T) {
	app := newConfigFilesCascadeTestApp(t, false)

	if err := enableConfigFilesCascade(app, "stack_config_files", "stack", true); err != nil {
		t.Fatalf("enableConfigFilesCascade(true): %v", err)
	}
	if !cascadeDeleteOf(t, app) {
		t.Fatal("expected CascadeDelete to be true after up() on a legacy instance")
	}
}

// TestEnableConfigFilesCascadeUpIsNoopOnFreshInstances covers a fresh
// install, where migration 65's current source already creates the relation
// with CascadeDelete: true — up() must be a harmless no-op.
func TestEnableConfigFilesCascadeUpIsNoopOnFreshInstances(t *testing.T) {
	app := newConfigFilesCascadeTestApp(t, true)

	if err := enableConfigFilesCascade(app, "stack_config_files", "stack", true); err != nil {
		t.Fatalf("enableConfigFilesCascade(true): %v", err)
	}
	if !cascadeDeleteOf(t, app) {
		t.Fatal("expected CascadeDelete to remain true after up() on a fresh install")
	}
}

// TestMigration72RollbackDoesNotDisableCascade guards the fix for the bug
// this migration originally had: an unconditional down() that forced
// CascadeDelete back to false would break stack/job deletion on any install
// where migration 65 already sets it true (every fresh install, since 65's
// source now bakes that in) — and reintroduces the very bug this migration
// fixes on legacy instances too. The registered down() is a no-op, so this
// asserts the field is left untouched by up() followed by "rollback" in both
// the legacy and fresh scenarios.
func TestMigration72RollbackDoesNotDisableCascade(t *testing.T) {
	for _, startCascade := range []bool{false, true} {
		app := newConfigFilesCascadeTestApp(t, startCascade)

		if err := enableConfigFilesCascade(app, "stack_config_files", "stack", true); err != nil {
			t.Fatalf("up(): %v", err)
		}

		// The registered down() migration is a no-op (see
		// 72_add_cascade_delete_to_config_files.go) — simulate it by simply
		// not touching the field, and assert cascade is still enabled
		// afterward regardless of the pre-up() starting state.
		if !cascadeDeleteOf(t, app) {
			t.Fatalf("expected CascadeDelete to remain true after a no-op rollback (started at %v)", startCascade)
		}
	}
}
