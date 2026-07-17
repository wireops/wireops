package pb_migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func newSopsIntegrationTestApp(t *testing.T) *core.BaseApp {
	t.Helper()
	app := core.NewBaseApp(core.BaseAppConfig{
		DataDir:       t.TempDir(),
		EncryptionEnv: "pb_sops_integration_migration_test",
	})
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("bootstrap app: %v", err)
	}

	integrations := core.NewBaseCollection("integrations")
	integrations.Fields.Add(&core.TextField{Name: "slug", Required: true})
	integrations.Fields.Add(&core.BoolField{Name: "enabled"})
	integrations.Fields.Add(&core.BoolField{Name: "locked"})
	integrations.Fields.Add(&core.JSONField{Name: "config"})
	if err := app.Save(integrations); err != nil {
		t.Fatalf("save integrations collection: %v", err)
	}
	return app
}

func TestRemoveSopsIntegrationNoRecordIsNoop(t *testing.T) {
	app := newSopsIntegrationTestApp(t)

	if err := removeSopsIntegration(app); err != nil {
		t.Fatalf("removeSopsIntegration with no matching record: %v", err)
	}
}

func TestRemoveSopsIntegrationDeletesExistingRecord(t *testing.T) {
	app := newSopsIntegrationTestApp(t)

	col, err := app.FindCollectionByNameOrId("integrations")
	if err != nil {
		t.Fatalf("find integrations collection: %v", err)
	}
	record := core.NewRecord(col)
	record.Set("slug", "sops")
	record.Set("enabled", true)
	record.Set("locked", true)
	if err := app.Save(record); err != nil {
		t.Fatalf("save sops integration record: %v", err)
	}

	if err := removeSopsIntegration(app); err != nil {
		t.Fatalf("removeSopsIntegration: %v", err)
	}

	if _, err := app.FindRecordById("integrations", record.Id); err == nil {
		t.Fatal("sops integration record was not deleted")
	}
}
