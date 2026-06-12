package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/dbx"
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
	ensureTestUsersRoleField(t, app)
	t.Cleanup(func() { app.Cleanup() })
	return app
}

// newEmptySetupTestApp returns a test app with all RBAC users removed,
// simulating a fresh, unconfigured instance.
func newEmptySetupTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app := newSetupTestApp(t)
	clearAllSuperusers(t, app)
	clearAllUsers(t, app)
	return app
}

func clearAllSuperusers(t *testing.T, app core.App) {
	t.Helper()
	// Use raw SQL to bypass the "can't delete last superuser" guard that
	// app.Delete() enforces — this is intentional in a test-only context.
	// The PocketBase installer account is preserved so countRealSuperusers
	// continues to ignore it, correctly modeling a fresh-install state.
	_, err := app.DB().
		NewQuery("DELETE FROM _superusers WHERE email != {:installer}").
		Bind(dbx.Params{"installer": core.DefaultInstallerEmail}).
		Execute()
	if err != nil {
		t.Fatalf("failed to clear superusers: %v", err)
	}
}

func clearAllUsers(t *testing.T, app core.App) {
	t.Helper()
	_, err := app.DB().NewQuery("DELETE FROM users").Execute()
	if err != nil {
		t.Fatalf("failed to clear users: %v", err)
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
	req.RemoteAddr = "127.0.0.1:1234"
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
		t.Fatalf("handler returned error: %v", err)
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
		t.Error("expected needsSetup to be true when no users exist")
	}
}

func TestSetupStatusWhenAdminExists(t *testing.T) {
	app := newSetupTestApp(t)
	createTestUser(t, app, "admin@example.com", "password123", "admin")

	rec := callHandler(t, app, http.MethodGet, "/api/custom/setup/status", nil, handleSetupStatus(app))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["needsSetup"] {
		t.Error("expected needsSetup to be false when an admin user exists")
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

	created, err := app.FindAuthRecordByEmail("users", "first@example.com")
	if err != nil || created == nil {
		t.Fatal("expected user to exist after setup")
	}
	if created.GetString("role") != "admin" {
		t.Fatalf("expected first user to be admin, got %q", created.GetString("role"))
	}
	if !created.GetBool("protected") {
		t.Fatal("expected first admin to be protected")
	}
}

func TestSetupBlockedAfterAdminExists(t *testing.T) {
	app := newSetupTestApp(t)
	createTestUser(t, app, "existing@example.com", "password123", "admin")

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

func createTestUser(t *testing.T, app core.App, email, password, role string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("failed to find users collection: %v", err)
	}
	record := core.NewRecord(col)
	record.Set("email", email)
	record.Set("password", password)
	record.Set("role", role)
	record.Set("verified", true)
	if err := app.Save(record); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	return record
}

func ensureTestUsersRoleField(t *testing.T, app core.App) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return
	}
	changed := false
	if col.Fields.GetByName("role") == nil {
		col.Fields.Add(&core.SelectField{
			Name:      "role",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"viewer", "operator", "admin"},
		})
		changed = true
	}
	if col.Fields.GetByName("disabled") == nil {
		col.Fields.Add(&core.BoolField{Name: "disabled"})
		changed = true
	}
	if col.Fields.GetByName("protected") == nil {
		col.Fields.Add(&core.BoolField{Name: "protected", Hidden: true})
		changed = true
	}
	if changed {
		if err := app.Save(col); err != nil {
			t.Fatalf("failed to add fields to users fixture: %v", err)
		}
	}
}
