package hooks

import (
	"context"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

const testBackupHookSecretKey = "ddddddddddddddddddddddddddddddd" // 32 bytes

func newBackupHookTestApp(t *testing.T) core.App {
	t.Helper()
	t.Setenv("SECRET_KEY", testBackupHookSecretKey)
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	auditLogs := core.NewBaseCollection("audit_logs")
	auditLogs.Fields.Add(&core.SelectField{Name: "actor_type", Required: true, MaxSelect: 1, Values: []string{"anonymous", "user", "system", "worker"}})
	auditLogs.Fields.Add(&core.TextField{Name: "actor_id"})
	auditLogs.Fields.Add(&core.TextField{Name: "action", Required: true})
	auditLogs.Fields.Add(&core.TextField{Name: "resource_type", Required: true})
	auditLogs.Fields.Add(&core.TextField{Name: "resource_id"})
	auditLogs.Fields.Add(&core.SelectField{Name: "origin", Required: true, MaxSelect: 1, Values: []string{"api", "setup", "system", "ui", "webhook", "worker"}})
	auditLogs.Fields.Add(&core.SelectField{Name: "status", Required: true, MaxSelect: 1, Values: []string{"success", "error"}})
	auditLogs.Fields.Add(&core.TextField{Name: "error_code"})
	auditLogs.Fields.Add(&core.JSONField{Name: "metadata_json"})
	auditLogs.Fields.Add(&core.DateField{Name: "expires_at", Required: true})
	auditLogs.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	if err := app.Save(auditLogs); err != nil {
		t.Fatalf("save audit_logs collection: %v", err)
	}

	integrations := core.NewBaseCollection("integrations")
	integrations.Fields.Add(&core.TextField{Name: "slug", Required: true})
	integrations.Fields.Add(&core.BoolField{Name: "enabled"})
	integrations.Fields.Add(&core.JSONField{Name: "config"})
	if err := app.Save(integrations); err != nil {
		t.Fatalf("save integrations collection: %v", err)
	}

	Register(app, nil, nil, nil)
	return app
}

func TestOnBackupCreateAuditsFailedMirrorAttempt(t *testing.T) {
	app := newBackupHookTestApp(t)

	// Enabled but missing required fields — the mirror attempt fails
	// deterministically without any real network call.
	col, err := app.FindCollectionByNameOrId("integrations")
	if err != nil {
		t.Fatalf("find integrations collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("slug", "s3")
	rec.Set("enabled", true)
	rec.Set("config", map[string]any{"bucket": "b"})
	if err := app.Save(rec); err != nil {
		t.Fatalf("save s3 integration: %v", err)
	}

	if err := app.CreateBackup(context.Background(), "wireops_hook_test.zip"); err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	records, err := app.FindAllRecords("audit_logs", dbx.HashExp{"action": "backup.mirror"})
	if err != nil {
		t.Fatalf("query audit logs: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected exactly 1 backup.mirror audit event, got %d", len(records))
	}
	if records[0].GetString("resource_id") != "wireops_hook_test.zip" {
		t.Fatalf("expected resource_id %q, got %q", "wireops_hook_test.zip", records[0].GetString("resource_id"))
	}
	if records[0].GetString("status") != "error" {
		t.Fatalf("expected status error, got %q", records[0].GetString("status"))
	}
	if records[0].GetString("error_code") == "" {
		t.Fatal("expected a non-empty error_code describing the mirror failure")
	}
}

func TestOnBackupCreateNotAuditedWhenRemoteDisabled(t *testing.T) {
	app := newBackupHookTestApp(t)

	if err := app.CreateBackup(context.Background(), "wireops_hook_local_test.zip"); err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	records, err := app.FindAllRecords("audit_logs", dbx.HashExp{"action": "backup.mirror"})
	if err != nil {
		t.Fatalf("query audit logs: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected no backup.mirror audit events with remote storage disabled, got %d", len(records))
	}
}
