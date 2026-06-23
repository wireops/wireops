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

	"github.com/wireops/wireops/internal/audit"
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

func withBootstrapToken(t *testing.T, token string) {
	t.Helper()
	t.Setenv("BOOTSTRAP_TOKEN", token)
}

func TestSetupStatusWhenEmpty(t *testing.T) {
	app := newEmptySetupTestApp(t)
	withBootstrapToken(t, "bootstrap-secret")

	rec := callHandler(t, app, http.MethodGet, "/api/custom/setup/status", nil, handleSetupStatus(app))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp setupStatus
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.NeedsSetup {
		t.Error("expected needsSetup to be true when no users exist")
	}
	if !resp.SetupAllowed {
		t.Error("expected setupAllowed to be true when bootstrap token is configured")
	}
	if resp.Reason != "" {
		t.Errorf("expected empty reason, got %q", resp.Reason)
	}
	if !resp.RequiresBootstrapToken {
		t.Error("expected requiresBootstrapToken to be true")
	}
}

func TestSetupStatusWhenEmptyWithoutBootstrapToken(t *testing.T) {
	app := newEmptySetupTestApp(t)
	withBootstrapToken(t, "")

	rec := callHandler(t, app, http.MethodGet, "/api/custom/setup/status", nil, handleSetupStatus(app))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp setupStatus
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.NeedsSetup {
		t.Error("expected needsSetup to be true when no users exist")
	}
	if resp.SetupAllowed {
		t.Error("expected setupAllowed to be false when bootstrap token is missing")
	}
	if resp.Reason != "missing_bootstrap_token" {
		t.Errorf("expected missing_bootstrap_token reason, got %q", resp.Reason)
	}
}

func TestSetupStatusWhenAdminExists(t *testing.T) {
	app := newSetupTestApp(t)
	withBootstrapToken(t, "bootstrap-secret")
	createTestUser(t, app, "admin@example.com", "password123", "admin")

	rec := callHandler(t, app, http.MethodGet, "/api/custom/setup/status", nil, handleSetupStatus(app))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp setupStatus
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.NeedsSetup {
		t.Error("expected needsSetup to be false when an admin user exists")
	}
	if resp.SetupAllowed {
		t.Error("expected setupAllowed to be false when setup has already completed")
	}
	if resp.Reason != "already_configured" {
		t.Errorf("expected already_configured reason, got %q", resp.Reason)
	}
}

func TestSetupStatusFailurePath(t *testing.T) {
	app := newEmptySetupTestApp(t)
	withBootstrapToken(t, "bootstrap-secret")

	if _, err := app.DB().NewQuery("DROP TABLE users").Execute(); err != nil {
		t.Fatalf("failed to drop users table: %v", err)
	}

	rec := callHandler(t, app, http.MethodGet, "/api/custom/setup/status", nil, handleSetupStatus(app))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}

	var resp setupStatus
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Reason != "unknown" {
		t.Errorf("expected unknown reason, got %q", resp.Reason)
	}
}

func TestSetupCreateFirstAdmin(t *testing.T) {
	app := newEmptySetupTestApp(t)
	withBootstrapToken(t, "bootstrap-secret")

	body := map[string]string{
		"email":          "first@example.com",
		"password":       "securepassword",
		"bootstrapToken": "bootstrap-secret",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	created, err := app.FindAuthRecordByEmail("users", "first@example.com")
	if err != nil || created == nil {
		t.Fatal("expected user to exist after setup")
	}
	superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "first@example.com")
	if err != nil || superuser == nil {
		t.Fatal("expected superuser to exist after setup")
	}
	if created.GetString("role") != "admin" {
		t.Fatalf("expected first user to be admin, got %q", created.GetString("role"))
	}
	if !created.GetBool("protected") {
		t.Fatal("expected first admin to be protected")
	}
	if created.GetString("passwordHash") != superuser.GetString("passwordHash") {
		t.Fatal("expected user and superuser password hashes to match")
	}
	if created.GetString("tokenKey") != superuser.GetString("tokenKey") {
		t.Fatal("expected user and superuser token keys to match")
	}
	assertRouteAuditEvent(t, app, "setup.bootstrap_started", "success", "", "f***@example.com")
	assertRouteAuditEvent(t, app, "setup.bootstrap_completed", "success", "", "f***@example.com")
}

func TestSetupBlockedAfterAdminExists(t *testing.T) {
	app := newSetupTestApp(t)
	withBootstrapToken(t, "bootstrap-secret")
	createTestUser(t, app, "existing@example.com", "password123", "admin")

	body := map[string]string{
		"email":          "attacker@example.com",
		"password":       "hackpassword",
		"bootstrapToken": "bootstrap-secret",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	assertRouteAuditEvent(t, app, "setup.bootstrap_rejected", "error", "already_configured", "a***@example.com")
}

func TestSetupValidationMissingFields(t *testing.T) {
	app := newEmptySetupTestApp(t)
	withBootstrapToken(t, "bootstrap-secret")

	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", map[string]string{
		"email": "missing@example.com",
	}, handleSetupCreate(app))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSetupValidationInvalidRequestBody(t *testing.T) {
	app := newEmptySetupTestApp(t)
	withBootstrapToken(t, "bootstrap-secret")

	req := httptest.NewRequest(http.MethodPost, "/api/custom/setup", bytes.NewBufferString("{"))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e := &core.RequestEvent{
		App: app,
		Event: router.Event{
			Response: rec,
			Request:  req,
		},
	}

	if err := handleSetupCreate(app)(e); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSetupValidationInvalidEmail(t *testing.T) {
	app := newEmptySetupTestApp(t)
	withBootstrapToken(t, "bootstrap-secret")

	body := map[string]string{
		"email":          "not-an-email",
		"password":       "password123",
		"bootstrapToken": "bootstrap-secret",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSetupValidationShortPassword(t *testing.T) {
	app := newEmptySetupTestApp(t)
	withBootstrapToken(t, "bootstrap-secret")

	body := map[string]string{
		"email":          "user@example.com",
		"password":       "short",
		"bootstrapToken": "bootstrap-secret",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSetupValidationInvalidBootstrapToken(t *testing.T) {
	app := newEmptySetupTestApp(t)
	withBootstrapToken(t, "bootstrap-secret")

	body := map[string]string{
		"email":          "user@example.com",
		"password":       "password123",
		"bootstrapToken": "wrong-secret",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestSetupValidationMissingBootstrapTokenConfiguration(t *testing.T) {
	app := newEmptySetupTestApp(t)
	withBootstrapToken(t, "")

	body := map[string]string{
		"email":          "user@example.com",
		"password":       "password123",
		"bootstrapToken": "bootstrap-secret",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	assertRouteAuditEvent(t, app, "setup.bootstrap_rejected", "error", "missing_bootstrap_token", "u***@example.com")
}

func TestSetupCreateReturnsInternalErrorWhenBootstrapFails(t *testing.T) {
	app := newEmptySetupTestApp(t)
	withBootstrapToken(t, "bootstrap-secret")

	if _, err := app.DB().NewQuery("DROP TABLE _superusers").Execute(); err != nil {
		t.Fatalf("failed to drop superusers table: %v", err)
	}

	body := map[string]string{
		"email":          "broken@example.com",
		"password":       "securepassword",
		"bootstrapToken": "bootstrap-secret",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	assertRouteAuditEvent(t, app, "setup.bootstrap_started", "success", "", "b***@example.com")
	assertRouteAuditEvent(t, app, "setup.bootstrap_failed", "error", "bootstrap_failed", "b***@example.com")
	assertRouteAuditEvent(t, app, "setup.bootstrap_failed", "error", "internal_error", "b***@example.com")
}

func TestSetupValidationInvalidBootstrapTokenAuditsRejection(t *testing.T) {
	app := newEmptySetupTestApp(t)
	withBootstrapToken(t, "bootstrap-secret")

	body := map[string]string{
		"email":          "user@example.com",
		"password":       "password123",
		"bootstrapToken": "wrong-secret",
	}
	rec := callHandler(t, app, http.MethodPost, "/api/custom/setup", body, handleSetupCreate(app))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	assertRouteAuditEvent(t, app, "setup.bootstrap_rejected", "error", "invalid_bootstrap_token", "u***@example.com")
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

func assertRouteAuditEvent(t *testing.T, app core.App, action, status, errorCode, maskedEmail string) {
	t.Helper()

	records, err := app.FindAllRecords("audit_logs", dbx.HashExp{"action": action})
	if err != nil {
		t.Fatalf("failed to query audit logs for %s: %v", action, err)
	}
	if len(records) == 0 {
		t.Fatalf("expected audit event %s to exist", action)
	}

	for _, rec := range records {
		if rec.GetString("status") != status {
			continue
		}
		if rec.GetString("error_code") != errorCode {
			continue
		}
		if rec.GetString("origin") != audit.OriginSetup {
			continue
		}

		meta := audit.MetadataJSON(rec.Get("metadata_json"))
		if len(meta) == 0 {
			meta = audit.MetadataJSON(rec.GetString("metadata_json"))
		}
		if meta["email_masked"] == maskedEmail {
			return
		}
	}

	t.Fatalf("expected audit event %s with status=%q error_code=%q maskedEmail=%q", action, status, errorCode, maskedEmail)
}
