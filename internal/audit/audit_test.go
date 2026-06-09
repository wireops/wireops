package audit

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

func TestMatchCustomRoute(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		action       string
		resourceType string
		resourceID   string
		ok           bool
	}{
		{
			name:         "manual sync",
			method:       http.MethodPost,
			path:         "/api/custom/stacks/stack123/sync",
			action:       "stack.sync",
			resourceType: "stack",
			resourceID:   "stack123",
			ok:           true,
		},
		{
			name:         "force redeploy",
			method:       http.MethodPost,
			path:         "/api/custom/stacks/stack123/force-redeploy",
			action:       "stack.force_redeploy",
			resourceType: "stack",
			resourceID:   "stack123",
			ok:           true,
		},
		{
			name:         "worker token",
			method:       http.MethodPost,
			path:         "/api/custom/worker/tokens",
			action:       "worker_token.create",
			resourceType: "worker_token",
			ok:           true,
		},
		{
			name:         "app settings",
			method:       http.MethodPut,
			path:         "/api/custom/settings/app-settings",
			action:       "settings.app.update",
			resourceType: "app_settings",
			resourceID:   "global",
			ok:           true,
		},
		{
			name:         "user invite",
			method:       http.MethodPost,
			path:         "/api/custom/users/invite",
			action:       "user.invite",
			resourceType: "user",
			ok:           true,
		},
		{
			name:         "auth elevate",
			method:       http.MethodPost,
			path:         "/api/custom/auth/elevate",
			action:       "auth.elevate",
			resourceType: "auth",
			ok:           true,
		},
		{
			name:         "setup create admin",
			method:       http.MethodPost,
			path:         "/api/custom/setup",
			action:       "setup.create_admin",
			resourceType: "setup",
			resourceID:   "initial",
			ok:           true,
		},
		{
			name:         "credential test",
			method:       http.MethodPost,
			path:         "/api/custom/credentials/test",
			action:       "credential.test",
			resourceType: "credential",
			ok:           true,
		},
		{
			name:         "credential keyscan",
			method:       http.MethodPost,
			path:         "/api/custom/credentials/keyscan",
			action:       "credential.keyscan",
			resourceType: "credential",
			ok:           true,
		},
		{
			name:   "read route ignored",
			method: http.MethodGet,
			path:   "/api/custom/audit-logs",
			ok:     false,
		},
		{
			name:         "worker policy update",
			method:       http.MethodPut,
			path:         "/api/custom/workers/worker123/policy",
			action:       "worker_policy.update",
			resourceType: "worker_policy",
			resourceID:   "worker123",
			ok:           true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := MatchCustomRoute(tc.method, tc.path)
			if ok != tc.ok {
				t.Fatalf("expected ok=%v, got %v", tc.ok, ok)
			}
			if !ok {
				return
			}
			if ev.Action != tc.action || ev.ResourceType != tc.resourceType || ev.ResourceID != tc.resourceID {
				t.Fatalf("unexpected event: %#v", ev)
			}
		})
	}
}

func TestEventContractHasNoPayloadLikeFields(t *testing.T) {
	eventType := reflect.TypeOf(Event{})
	forbidden := []string{"payload", "changes", "label", "email", "ip", "method", "path", "header"}
	for i := 0; i < eventType.NumField(); i++ {
		fieldName := strings.ToLower(eventType.Field(i).Name)
		for _, word := range forbidden {
			if strings.Contains(fieldName, word) {
				t.Fatalf("event field %q must not include payload-like data", eventType.Field(i).Name)
			}
		}
	}
}

func TestRequestOriginUsesHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/stack123/sync", nil)
	req.Header.Set("X-Wireops-Origin", "ui")
	e := &core.RequestEvent{Event: router.Event{Request: req}}

	if got := RequestOrigin(e); got != OriginUI {
		t.Fatalf("expected origin %q, got %q", OriginUI, got)
	}
}

func TestRequestOriginInfersSetupAndWebhook(t *testing.T) {
	setupReq := httptest.NewRequest(http.MethodPost, "/api/custom/setup", nil)
	setupEvent := &core.RequestEvent{Event: router.Event{Request: setupReq}}
	if got := RequestOrigin(setupEvent); got != OriginSetup {
		t.Fatalf("expected setup origin, got %q", got)
	}

	webhookReq := httptest.NewRequest(http.MethodPost, "/api/custom/webhook/stack123", nil)
	webhookEvent := &core.RequestEvent{Event: router.Event{Request: webhookReq}}
	if got := RequestOrigin(webhookEvent); got != OriginWebhook {
		t.Fatalf("expected webhook origin, got %q", got)
	}
}

func TestRequestMetadataOnlyPersistsFieldNames(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/stack123/rollback?force=true", strings.NewReader(`{"commit_sha":"abc123","token":"secret-value"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "req-123")

	e := &core.RequestEvent{Event: router.Event{Request: req}}
	metadata := RequestMetadata(e)

	changedFields, ok := metadata["changed_fields"].([]string)
	if !ok || len(changedFields) != 2 {
		t.Fatalf("expected changed_fields metadata, got %#v", metadata["changed_fields"])
	}
	if changedFields[0] != "commit_sha" || changedFields[1] != "token" {
		t.Fatalf("unexpected changed_fields: %#v", changedFields)
	}

	sensitiveFields, ok := metadata["sensitive_fields"].([]string)
	if !ok || len(sensitiveFields) != 1 || sensitiveFields[0] != "token" {
		t.Fatalf("unexpected sensitive_fields: %#v", metadata["sensitive_fields"])
	}

	if metadata["request_id"] != "req-123" {
		t.Fatalf("expected request_id metadata, got %#v", metadata["request_id"])
	}
	if _, exists := metadata["commit_sha"]; exists {
		t.Fatalf("metadata must not include raw values: %#v", metadata)
	}
}
