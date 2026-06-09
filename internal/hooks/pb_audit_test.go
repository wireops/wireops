package hooks

import (
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
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
