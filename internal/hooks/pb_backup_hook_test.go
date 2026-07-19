package hooks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/crypto"
)

// fakeS3PutOKServer answers every request with 200 OK — enough for the real
// aws-sdk-go-v2 s3 client's PutObject calls (EnsurePrefix + the actual
// mirror upload) to succeed end-to-end without hitting real S3.
func fakeS3PutOKServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"fake"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const testBackupHookSecretKey = "dddddddddddddddddddddddddddddddd" // 32 bytes

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

func TestOnBackupCreateAuditsSuccessfulMirrorAttempt(t *testing.T) {
	app := newBackupHookTestApp(t)
	server := fakeS3PutOKServer(t)

	encryptedSecret, err := crypto.Encrypt([]byte("fake-secret-key"), crypto.NormalizeSecretKey(testBackupHookSecretKey))
	if err != nil {
		t.Fatalf("encrypt fake secret: %v", err)
	}

	col, err := app.FindCollectionByNameOrId("integrations")
	if err != nil {
		t.Fatalf("find integrations collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("slug", "s3")
	rec.Set("enabled", true)
	rec.Set("config", map[string]any{
		"bucket":           "wireops-backups",
		"region":           "us-east-1",
		"endpoint":         server.URL,
		"force_path_style": true,
		"encrypt_content":  false,
		"access_key":       "fake-access-key",
		"secret":           encryptedSecret,
	})
	if err := app.Save(rec); err != nil {
		t.Fatalf("save s3 integration: %v", err)
	}

	if err := app.CreateBackup(context.Background(), "wireops_hook_success_test.zip"); err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	records, err := app.FindAllRecords("audit_logs", dbx.HashExp{"action": "backup.mirror"})
	if err != nil {
		t.Fatalf("query audit logs: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected exactly 1 backup.mirror audit event, got %d", len(records))
	}
	if records[0].GetString("status") != "success" {
		t.Fatalf("expected status success, got %q", records[0].GetString("status"))
	}
	if records[0].GetString("resource_id") != "wireops_hook_success_test.zip" {
		t.Fatalf("expected resource_id %q, got %q", "wireops_hook_success_test.zip", records[0].GetString("resource_id"))
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
