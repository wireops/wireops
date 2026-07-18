package hooks

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/router"
)

func TestShouldSkipRequestAudit(t *testing.T) {
	customReq := httptest.NewRequest("PUT", "/api/custom/settings/app-settings", nil)
	customEvent := &core.RequestEvent{Event: router.Event{Request: customReq}}
	if !shouldSkipRequestAudit(customEvent) {
		t.Fatal("expected custom request audit to be skipped in record hooks")
	}

	stdReq := httptest.NewRequest("PATCH", "/api/collections/repositories/records/abc", nil)
	stdEvent := &core.RequestEvent{Event: router.Event{Request: stdReq}}
	if shouldSkipRequestAudit(stdEvent) {
		t.Fatal("expected standard collection request audit to be handled in record hooks")
	}
}

func TestBlockAuditLogMutation(t *testing.T) {
	if err := blockAuditLogMutation("audit_logs"); err == nil {
		t.Fatal("expected audit_logs mutation to be rejected")
	}
	if err := blockAuditLogMutation("stacks"); err != nil {
		t.Fatalf("expected non-audit collection to be allowed, got %v", err)
	}
}

func TestSuperuserLoginIsAudited(t *testing.T) {
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

	Register(app, nil, nil, nil)

	col, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatalf("find superusers collection: %v", err)
	}
	superuser := core.NewRecord(col)
	superuser.Set("email", "root@example.com")
	superuser.Set("password", "password123")
	if err := app.Save(superuser); err != nil {
		t.Fatalf("create superuser: %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/collections/_superusers/auth-with-password", nil)
	event := &core.RecordAuthRequestEvent{
		RequestEvent: &core.RequestEvent{App: app, Event: router.Event{Request: req}},
		Record:       superuser,
		Token:        "test-token",
		AuthMethod:   "password",
	}
	event.Collection = col

	if err := app.OnRecordAuthRequest().Trigger(event, func(e *core.RecordAuthRequestEvent) error {
		return nil
	}); err != nil {
		t.Fatalf("trigger auth hook: %v", err)
	}

	records, err := app.FindAllRecords("audit_logs", dbx.HashExp{"action": "superuser.login"})
	if err != nil {
		t.Fatalf("query audit logs: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected exactly 1 superuser.login audit event, got %d", len(records))
	}
	if records[0].GetString("actor_id") != superuser.Id {
		t.Fatalf("expected actor_id %q, got %q", superuser.Id, records[0].GetString("actor_id"))
	}
	if records[0].GetString("status") != "success" {
		t.Fatalf("expected status success, got %q", records[0].GetString("status"))
	}
}

func TestSuperuserFailedLoginIsAuditedAsAuthFailure(t *testing.T) {
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

	Register(app, nil, nil, nil)

	col, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatalf("find superusers collection: %v", err)
	}
	superuser := core.NewRecord(col)
	superuser.Set("email", "root2@example.com")
	superuser.Set("password", "password123")
	if err := app.Save(superuser); err != nil {
		t.Fatalf("create superuser: %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/api/collections/_superusers/auth-with-password", nil)
	event := &core.RecordAuthRequestEvent{
		RequestEvent: &core.RequestEvent{App: app, Event: router.Event{Request: req}},
		Record:       superuser,
		Token:        "test-token",
		AuthMethod:   "password",
	}
	event.Collection = col

	wantErr := errors.New("invalid credentials")
	if err := app.OnRecordAuthRequest().Trigger(event, func(e *core.RecordAuthRequestEvent) error {
		return wantErr
	}); err == nil {
		t.Fatal("expected auth hook to propagate the failure")
	}

	records, err := app.FindAllRecords("audit_logs", dbx.HashExp{"action": "superuser.login"})
	if err != nil {
		t.Fatalf("query audit logs: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected exactly 1 superuser.login audit event, got %d", len(records))
	}
	if records[0].GetString("status") != "error" {
		t.Fatalf("expected status error, got %q", records[0].GetString("status"))
	}
	if records[0].GetString("error_code") != "auth_failed" {
		t.Fatalf("expected error_code auth_failed, got %q", records[0].GetString("error_code"))
	}
}
