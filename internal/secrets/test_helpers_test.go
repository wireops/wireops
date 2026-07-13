package secrets

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// newSecretBackendsTestApp returns a test PocketBase app with a minimal
// integrations collection, matching pb_migrations/01_init_collections.go's
// createIntegrations. Vault/Infisical connection config is stored there
// (slug "vault"/"infisical"), alongside notification integrations.
func newSecretBackendsTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col := core.NewBaseCollection("integrations")
	col.Fields.Add(&core.TextField{Name: "slug", Required: true})
	col.Fields.Add(&core.BoolField{Name: "enabled"})
	col.Fields.Add(&core.JSONField{Name: "config"})
	if err := app.Save(col); err != nil {
		t.Fatalf("save integrations collection: %v", err)
	}

	return app
}

func mustCreateBackendRecord(t *testing.T, app core.App, slug string, enabled bool, config map[string]any) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("integrations")
	if err != nil {
		t.Fatalf("find integrations collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("slug", slug)
	rec.Set("enabled", enabled)
	rec.Set("config", config)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save integrations record: %v", err)
	}
	return rec
}
