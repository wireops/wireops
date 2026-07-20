package sync

import (
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// newOverridesTestApp builds a minimal schema (stacks + audit_logs) rather than
// importing pb_migrations: that package is blank-imported by other _test.go files
// in this package with their own hand-rolled "repositories"/"stacks" collections,
// and migrations are process-global, so pulling it in here would collide with them.
func newOverridesTestApp(t *testing.T) (*tests.TestApp, *core.Record) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	stacks.Fields.Add(&core.JSONField{Name: "render_overrides"})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("failed to create stacks collection: %v", err)
	}

	auditLogs := core.NewBaseCollection("audit_logs")
	auditLogs.Fields.Add(&core.SelectField{Name: "actor_type", Required: true, MaxSelect: 1, Values: []string{"user", "system", "agent"}})
	auditLogs.Fields.Add(&core.TextField{Name: "actor_id"})
	auditLogs.Fields.Add(&core.TextField{Name: "action", Required: true})
	auditLogs.Fields.Add(&core.TextField{Name: "resource_type", Required: true})
	auditLogs.Fields.Add(&core.TextField{Name: "resource_id"})
	auditLogs.Fields.Add(&core.TextField{Name: "origin"})
	auditLogs.Fields.Add(&core.SelectField{Name: "status", Required: true, MaxSelect: 1, Values: []string{"success", "error"}})
	auditLogs.Fields.Add(&core.TextField{Name: "error_code"})
	auditLogs.Fields.Add(&core.JSONField{Name: "metadata_json"})
	auditLogs.Fields.Add(&core.DateField{Name: "expires_at", Required: true})
	if err := app.Save(auditLogs); err != nil {
		t.Fatalf("failed to create audit_logs collection: %v", err)
	}

	col, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatalf("find stacks collection: %v", err)
	}
	stack := core.NewRecord(col)
	stack.Set("name", "overrides-stack")
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to create stack: %v", err)
	}
	return app, stack
}

func TestLoadRenderOverridesReturnsNilWhenUnset(t *testing.T) {
	_, stack := newOverridesTestApp(t)

	if got := LoadRenderOverrides(stack); got != nil {
		t.Fatalf("expected nil overrides, got %#v", got)
	}
}

func TestLoadRenderOverridesReadsPersistedValue(t *testing.T) {
	app, stack := newOverridesTestApp(t)

	overrides := map[string]ServiceOverride{
		"web": {Image: "nginx:test", Ports: []string{"8081:80"}, Networks: []string{"proxy"}},
	}
	stack.Set("render_overrides", overrides)
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to save stack overrides: %v", err)
	}

	stack, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("failed to reload stack: %v", err)
	}

	got := LoadRenderOverrides(stack)
	if len(got) != 1 {
		t.Fatalf("expected 1 override, got %d: %#v", len(got), got)
	}
	web, ok := got["web"]
	if !ok {
		t.Fatalf("expected override for service %q, got %#v", "web", got)
	}
	if web.Image != "nginx:test" {
		t.Errorf("image = %q, want nginx:test", web.Image)
	}
	if len(web.Ports) != 1 || web.Ports[0] != "8081:80" {
		t.Errorf("ports = %#v, want [8081:80]", web.Ports)
	}
	if len(web.Networks) != 1 || web.Networks[0] != "proxy" {
		t.Errorf("networks = %#v, want [proxy]", web.Networks)
	}
}

func TestClearStaleRenderOverridesClearsRecordAndAudits(t *testing.T) {
	app, stack := newOverridesTestApp(t)

	overrides := map[string]ServiceOverride{
		"removed-service": {Image: "nginx:test"},
	}
	stack.Set("render_overrides", overrides)
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to save stack overrides: %v", err)
	}

	r := &Reconciler{app: app}
	r.clearStaleRenderOverrides(stack, stack.Id, "render override targets unknown service \"removed-service\"")

	reloaded, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("failed to reload stack: %v", err)
	}
	if got := LoadRenderOverrides(reloaded); len(got) != 0 {
		t.Fatalf("expected overrides to be cleared, got %#v", got)
	}

	logs, err := app.FindAllRecords("audit_logs", dbx.HashExp{
		"action":        "stack.render_overrides.auto_cleared",
		"resource_type": "stack",
		"resource_id":   stack.Id,
	})
	if err != nil {
		t.Fatalf("failed to query audit logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 auto_cleared audit log, got %d", len(logs))
	}
	if got := logs[0].GetString("status"); got != "success" {
		t.Errorf("audit log status = %q, want success", got)
	}
	if got := logs[0].GetString("actor_type"); got != "system" {
		t.Errorf("audit log actor_type = %q, want system", got)
	}
}

func TestClearStaleRenderOverridesIsNoOpWhenAlreadyEmpty(t *testing.T) {
	app, stack := newOverridesTestApp(t)

	r := &Reconciler{app: app}
	r.clearStaleRenderOverrides(stack, stack.Id, "no overrides to begin with")

	reloaded, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("failed to reload stack: %v", err)
	}
	if got := LoadRenderOverrides(reloaded); len(got) != 0 {
		t.Fatalf("expected overrides to remain empty, got %#v", got)
	}

	logs, err := app.FindAllRecords("audit_logs", dbx.HashExp{
		"action":      "stack.render_overrides.auto_cleared",
		"resource_id": stack.Id,
	})
	if err != nil {
		t.Fatalf("failed to query audit logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected auto-clear to still audit even when already empty, got %d logs", len(logs))
	}
}
