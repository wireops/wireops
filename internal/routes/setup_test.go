package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/router"
)

func newSetupTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })
	return app
}

// newEmptySetupTestApp returns a test app with all superusers removed,
// simulating a fresh, unconfigured instance.
func newEmptySetupTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app := newSetupTestApp(t)
	clearAllSuperusers(t, app)
	return app
}

func clearAllSuperusers(t *testing.T, app core.App) {
	t.Helper()
	// Use raw SQL to bypass the "can't delete last superuser" guard that
	// app.Delete() enforces — this is intentional in a test-only context.
	if _, err := app.DB().NewQuery("DELETE FROM _superusers").Execute(); err != nil {
		t.Fatalf("failed to clear superusers: %v", err)
	}
}

func callHandler(t *testing.T, app core.App, method, target string, body any, handler func(*core.RequestEvent) error) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, target, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e := &core.RequestEvent{
		App: app,
		Event: router.Event{
			Response: rec,
			Request:  req,
		},
	}
	if err := handler(e); err != nil {
		t.Logf("handler returned error: %v", err)
	}
	return rec
}

func TestSetupStatusWhenEmpty(t *testing.T) {
	app := newEmptySetupTestApp(t)

	rec := callHandler(t, app, http.MethodGet, "/api/custom/setup/status", nil, handleSetupStatus(app))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp["needsSetup"] {
		t.Error("expected needsSetup to be true when no superusers exist")
	}
}

func TestSetupStatusWhenAdminExists(t *testing.T) {
	app := newSetupTestApp(t)
	createTestSuperuser(t, app, "admin@example.com", "password123")

	rec := callHandler(t, app, http.MethodGet, "/api/custom/setup/status", nil, handleSetupStatus(app))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["needsSetup"] {
		t.Error("expected needsSetup to be false when a superuser exists")
	}
}

func TestSetupCreateFirstAdmin(t *testing.T) {
	app := newEmptySetupTestApp(t)

	body := map[string]string{
		"email":           "first@example.com",
		"password":        "securepassword",
		"passwordConfirm": "securepassword",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	created, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "first@example.com")
	if err != nil || created == nil {
		t.Fatal("expected superuser to exist after setup")
	}
}

func TestSetupBlockedAfterAdminExists(t *testing.T) {
	app := newSetupTestApp(t)
	createTestSuperuser(t, app, "existing@example.com", "password123")

	body := map[string]string{
		"email":           "attacker@example.com",
		"password":        "hackpassword",
		"passwordConfirm": "hackpassword",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestSetupValidationMissingFields(t *testing.T) {
	app := newEmptySetupTestApp(t)

	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", map[string]string{
		"email": "missing@example.com",
	}, handleSetupCreate(app))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSetupValidationInvalidEmail(t *testing.T) {
	app := newEmptySetupTestApp(t)

	body := map[string]string{
		"email":           "not-an-email",
		"password":        "password123",
		"passwordConfirm": "password123",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSetupValidationShortPassword(t *testing.T) {
	app := newEmptySetupTestApp(t)

	body := map[string]string{
		"email":           "user@example.com",
		"password":        "short",
		"passwordConfirm": "short",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSetupValidationPasswordMismatch(t *testing.T) {
	app := newEmptySetupTestApp(t)

	body := map[string]string{
		"email":           "user@example.com",
		"password":        "password123",
		"passwordConfirm": "different123",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func createTestSuperuser(t *testing.T, app core.App, email, password string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatalf("failed to find superusers collection: %v", err)
	}
	record := core.NewRecord(col)
	record.Set("email", email)
	record.Set("password", password)
	if err := app.Save(record); err != nil {
		t.Fatalf("failed to create superuser: %v", err)
	}
	return record
}
