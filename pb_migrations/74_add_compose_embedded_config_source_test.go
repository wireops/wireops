package pb_migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

const migration74File = "74_add_compose_embedded_config_source.go"

func migration74(t *testing.T) *core.Migration {
	t.Helper()
	for _, item := range core.AppMigrations.Items() {
		if item.File == migration74File {
			return item
		}
	}
	t.Fatalf("migration %q not found in core.AppMigrations", migration74File)
	return nil
}

func newComposeEmbeddedTestApp(t *testing.T, values ...string) (core.App, *core.Collection) {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       t.TempDir(),
		EncryptionEnv: "pb_compose_embedded_migration_test",
	})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap app: %v", err)
	}

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	stacks.Fields.Add(&core.SelectField{Name: "config_source", Values: values, MaxSelect: 1})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("save stacks collection: %v", err)
	}
	return app, stacks
}

func configSourceValuesOf(t *testing.T, app core.App) []string {
	t.Helper()
	stacks, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatalf("find stacks collection: %v", err)
	}
	field, ok := stacks.Fields.GetByName("config_source").(*core.SelectField)
	if !ok {
		t.Fatalf("config_source field not found or wrong type")
	}
	return field.Values
}

func TestMigration74UpAddsComposeEmbeddedValue(t *testing.T) {
	app, _ := newComposeEmbeddedTestApp(t, "manual", "wireops_file")
	mig := migration74(t)

	if err := mig.Up(app); err != nil {
		t.Fatalf("up(): %v", err)
	}

	got := configSourceValuesOf(t, app)
	want := []string{"manual", "wireops_file", "compose_embedded"}
	if len(got) != len(want) {
		t.Fatalf("config_source values = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("config_source values = %v, want %v", got, want)
		}
	}
}

// TestMigration74RollbackNormalizesExistingRecords covers the fix for a bug
// where down() narrowed config_source's allowed values back to
// [manual, wireops_file] without first rewriting stacks still holding
// "compose_embedded" — a later API update on those rows would then fail
// SelectField validation. Down() must normalize them to "wireops_file" (the
// two config sources are handled identically everywhere else) before
// narrowing the values.
func TestMigration74RollbackNormalizesExistingRecords(t *testing.T) {
	app, stacks := newComposeEmbeddedTestApp(t, "manual", "wireops_file", "compose_embedded")
	mig := migration74(t)

	embedded := core.NewRecord(stacks)
	embedded.Set("name", "embedded-stack")
	embedded.Set("config_source", "compose_embedded")
	if err := app.Save(embedded); err != nil {
		t.Fatalf("save compose_embedded stack: %v", err)
	}

	fileManaged := core.NewRecord(stacks)
	fileManaged.Set("name", "wireops-file-stack")
	fileManaged.Set("config_source", "wireops_file")
	if err := app.Save(fileManaged); err != nil {
		t.Fatalf("save wireops_file stack: %v", err)
	}

	if err := mig.Down(app); err != nil {
		t.Fatalf("down(): %v", err)
	}

	got := configSourceValuesOf(t, app)
	want := []string{"manual", "wireops_file"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("config_source values after down() = %v, want %v", got, want)
	}

	reloadedEmbedded, err := app.FindRecordById("stacks", embedded.Id)
	if err != nil {
		t.Fatalf("find embedded stack: %v", err)
	}
	if got := reloadedEmbedded.GetString("config_source"); got != "wireops_file" {
		t.Fatalf("compose_embedded stack config_source after down() = %q, want %q", got, "wireops_file")
	}

	reloadedFileManaged, err := app.FindRecordById("stacks", fileManaged.Id)
	if err != nil {
		t.Fatalf("find wireops_file stack: %v", err)
	}
	if got := reloadedFileManaged.GetString("config_source"); got != "wireops_file" {
		t.Fatalf("wireops_file stack config_source after down() = %q, want %q (should be untouched)", got, "wireops_file")
	}
}
