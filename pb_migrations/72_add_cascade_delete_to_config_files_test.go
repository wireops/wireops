package pb_migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

const migration72File = "72_add_cascade_delete_to_config_files.go"

// migration72 looks up the actual Up/Down callbacks this package's init()
// registered into the global core.AppMigrations list, so tests exercise the
// real registered migration instead of only the enableConfigFilesCascade
// helper it happens to share with them.
func migration72(t *testing.T) *core.Migration {
	t.Helper()
	for _, item := range core.AppMigrations.Items() {
		if item.File == migration72File {
			return item
		}
	}
	t.Fatalf("migration %q not found in core.AppMigrations", migration72File)
	return nil
}

// newConfigFilesCascadeTestApp builds a minimal app with stacks/jobs
// collections and stack_config_files.stack / job_config_files.job relations
// whose CascadeDelete starts at startCascade — false simulates a legacy
// instance whose migration 65 ran before CascadeDelete was added to its
// source; true simulates a fresh install, where migration 65's current
// source already creates both relations with CascadeDelete: true.
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

	stackConfigFiles := core.NewBaseCollection("stack_config_files")
	stackConfigFiles.Fields.Add(&core.RelationField{
		Name:          "stack",
		CollectionId:  stacks.Id,
		Required:      true,
		MaxSelect:     1,
		CascadeDelete: startCascade,
	})
	if err := app.Save(stackConfigFiles); err != nil {
		t.Fatalf("save stack_config_files collection: %v", err)
	}

	jobs := core.NewBaseCollection("scheduled_jobs")
	jobs.Fields.Add(&core.TextField{Name: "name"})
	if err := app.Save(jobs); err != nil {
		t.Fatalf("save scheduled_jobs collection: %v", err)
	}

	jobConfigFiles := core.NewBaseCollection("job_config_files")
	jobConfigFiles.Fields.Add(&core.RelationField{
		Name:          "job",
		CollectionId:  jobs.Id,
		Required:      true,
		MaxSelect:     1,
		CascadeDelete: startCascade,
	})
	if err := app.Save(jobConfigFiles); err != nil {
		t.Fatalf("save job_config_files collection: %v", err)
	}

	return app
}

func cascadeDeleteOf(t *testing.T, app core.App, collectionName, fieldName string) bool {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		t.Fatalf("find %s collection: %v", collectionName, err)
	}
	field, ok := col.Fields.GetByName(fieldName).(*core.RelationField)
	if !ok {
		t.Fatalf("%s.%s field not found or wrong type", collectionName, fieldName)
	}
	return field.CascadeDelete
}

func assertBothCascadesEqual(t *testing.T, app core.App, want bool, context string) {
	t.Helper()
	if got := cascadeDeleteOf(t, app, "stack_config_files", "stack"); got != want {
		t.Fatalf("%s: stack_config_files.stack CascadeDelete = %v, want %v", context, got, want)
	}
	if got := cascadeDeleteOf(t, app, "job_config_files", "job"); got != want {
		t.Fatalf("%s: job_config_files.job CascadeDelete = %v, want %v", context, got, want)
	}
}

// TestEnableConfigFilesCascadeFixesLegacyInstances covers a legacy instance
// whose migration 65 ran before CascadeDelete was added to its source —
// up() must flip both relations to true.
func TestEnableConfigFilesCascadeFixesLegacyInstances(t *testing.T) {
	app := newConfigFilesCascadeTestApp(t, false)

	if err := enableConfigFilesCascade(app, "stack_config_files", "stack", true); err != nil {
		t.Fatalf("enableConfigFilesCascade(stack, true): %v", err)
	}
	if err := enableConfigFilesCascade(app, "job_config_files", "job", true); err != nil {
		t.Fatalf("enableConfigFilesCascade(job, true): %v", err)
	}
	assertBothCascadesEqual(t, app, true, "after up() on a legacy instance")
}

// TestEnableConfigFilesCascadeUpIsNoopOnFreshInstances covers a fresh
// install, where migration 65's current source already creates both
// relations with CascadeDelete: true — up() must be a harmless no-op.
func TestEnableConfigFilesCascadeUpIsNoopOnFreshInstances(t *testing.T) {
	app := newConfigFilesCascadeTestApp(t, true)

	if err := enableConfigFilesCascade(app, "stack_config_files", "stack", true); err != nil {
		t.Fatalf("enableConfigFilesCascade(stack, true): %v", err)
	}
	if err := enableConfigFilesCascade(app, "job_config_files", "job", true); err != nil {
		t.Fatalf("enableConfigFilesCascade(job, true): %v", err)
	}
	assertBothCascadesEqual(t, app, true, "after up() on a fresh install")
}

// TestMigration72UpEnablesBothCascades exercises the actual registered up()
// callback (not just the enableConfigFilesCascade helper) end-to-end on a
// legacy instance, covering both stack_config_files.stack and
// job_config_files.job in the same pass.
func TestMigration72UpEnablesBothCascades(t *testing.T) {
	app := newConfigFilesCascadeTestApp(t, false)
	mig := migration72(t)

	if err := mig.Up(app); err != nil {
		t.Fatalf("up(): %v", err)
	}
	assertBothCascadesEqual(t, app, true, "after the registered up()")
}

// TestMigration72RollbackDoesNotDisableCascade guards the fix for the bug
// this migration originally had: an unconditional down() that forced
// CascadeDelete back to false would break stack/job deletion on any install
// where migration 65 already sets it true (every fresh install, since 65's
// source now bakes that in) — and reintroduces the very bug this migration
// fixes on legacy instances too. This exercises the actual registered
// up()/down() callbacks (not just the shared helper) for both relations, on
// both a legacy (started false) and a fresh (started true) instance.
func TestMigration72RollbackDoesNotDisableCascade(t *testing.T) {
	mig := migration72(t)

	for _, startCascade := range []bool{false, true} {
		app := newConfigFilesCascadeTestApp(t, startCascade)

		if err := mig.Up(app); err != nil {
			t.Fatalf("up() (started at %v): %v", startCascade, err)
		}
		assertBothCascadesEqual(t, app, true, "after up()")

		if err := mig.Down(app); err != nil {
			t.Fatalf("down() (started at %v): %v", startCascade, err)
		}
		assertBothCascadesEqual(t, app, true, "after down() (a no-op rollback)")
	}
}
