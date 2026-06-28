package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	_ "github.com/wireops/wireops/pb_migrations"

	wireauth "github.com/wireops/wireops/internal/auth"
	"github.com/wireops/wireops/internal/rbac"
)

// setupTestSAApp initializes a setup test application, clears users,
// creates an admin user, configures router with admin auth, and registers SA routes.
func setupTestSAApp(t *testing.T) (core.App, http.Handler, *core.Record) {
	t.Setenv("SECRET_KEY", strings.Repeat("a", 32))

	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "admin@example.com", "Password1!", rbac.RoleAdmin)

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
			Auth:  admin,
		}, nil
	})
	RegisterServiceAccountRoutes(r, app)
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux, admin
}

// createTestSA creates a service account record in pocketbase with given details.
func createTestSA(t *testing.T, app core.App, name, desc, role string, enabled bool) *core.Record {
	col, err := app.FindCollectionByNameOrId("service_accounts")
	if err != nil {
		t.Fatalf("find service_accounts collection: %v", err)
	}
	sa := core.NewRecord(col)
	sa.Set("name", name)
	sa.Set("description", desc)
	sa.Set("role", role)
	sa.Set("enabled", enabled)
	if err := app.Save(sa); err != nil {
		t.Fatalf("create test service account: %v", err)
	}
	return sa
}

func TestCreateServiceAccountRoleValidation(t *testing.T) {
	_, mux, _ := setupTestSAApp(t)

	// 1. Try role: admin (should fail)
	body := map[string]any{
		"name":        "Test Admin SA",
		"description": "Admin SA description",
		"role":        rbac.RoleAdmin,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/service-accounts", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for admin role, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Try role: viewer (should succeed)
	body = map[string]any{
		"name":        "Test Viewer SA",
		"description": "Viewer SA description",
		"role":        rbac.RoleViewer,
	}
	b, _ = json.Marshal(body)
	req = httptest.NewRequest(http.MethodPost, "/api/custom/service-accounts", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for viewer role, got %d: %s", rec.Code, rec.Body.String())
	}

	var viewerResp struct {
		ApiKey    string `json:"api_key"`
		KeyPrefix string `json:"key_prefix"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&viewerResp); err != nil {
		t.Fatalf("decode viewer create response: %v", err)
	}
	if viewerResp.ApiKey == "" || viewerResp.KeyPrefix == "" {
		t.Fatal("expected api_key and key_prefix in viewer creation response")
	}

	// 3. Try role: operator (should succeed)
	body = map[string]any{
		"name":        "Test Operator SA",
		"description": "Operator SA description",
		"role":        rbac.RoleOperator,
	}
	b, _ = json.Marshal(body)
	req = httptest.NewRequest(http.MethodPost, "/api/custom/service-accounts", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for operator role, got %d: %s", rec.Code, rec.Body.String())
	}

	var operatorResp struct {
		ApiKey    string `json:"api_key"`
		KeyPrefix string `json:"key_prefix"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&operatorResp); err != nil {
		t.Fatalf("decode operator create response: %v", err)
	}
	if operatorResp.ApiKey == "" || operatorResp.KeyPrefix == "" {
		t.Fatal("expected api_key and key_prefix in operator creation response")
	}
}

func TestUpdateServiceAccountRoleValidation(t *testing.T) {
	app, mux, _ := setupTestSAApp(t)
	sa := createTestSA(t, app, "Initial SA", "Initial SA description", rbac.RoleViewer, true)

	// 1. Try updating to admin role (should fail)
	body := map[string]any{
		"role": rbac.RoleAdmin,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/custom/service-accounts/"+sa.Id, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when updating to admin, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Try updating to operator role (should succeed)
	body = map[string]any{
		"role": rbac.RoleOperator,
	}
	b, _ = json.Marshal(body)
	req = httptest.NewRequest(http.MethodPut, "/api/custom/service-accounts/"+sa.Id, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when updating to operator, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIssueAndRevokeAPIKeyEmbedded(t *testing.T) {
	app, mux, _ := setupTestSAApp(t)
	sa := createTestSA(t, app, "Test Auth SA", "Test Auth SA description", rbac.RoleViewer, true)

	// 1. Issue key
	req := httptest.NewRequest(http.MethodPost, "/api/custom/service-accounts/"+sa.Id+"/keys", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var keyResp struct {
		ApiKey    string `json:"api_key"`
		KeyPrefix string `json:"key_prefix"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&keyResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if keyResp.ApiKey == "" || keyResp.KeyPrefix == "" {
		t.Fatal("expected api_key and key_prefix in response")
	}

	// Reload service account and check key fields
	reloaded, err := app.FindRecordById("service_accounts", sa.Id)
	if err != nil {
		t.Fatalf("reload service account: %v", err)
	}
	if reloaded.GetString("key_prefix") != keyResp.KeyPrefix {
		t.Fatalf("expected key_prefix %s, got %s", keyResp.KeyPrefix, reloaded.GetString("key_prefix"))
	}
	if reloaded.GetString("key_hash") == "" {
		t.Fatal("expected key_hash to be populated")
	}
	if reloaded.GetBool("key_revoked") {
		t.Fatal("expected key to not be revoked")
	}

	// Test middleware authentication with valid key
	rAuth := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
		}, nil
	})
	rAuth.BindFunc(wireauth.APIKeyMiddleware(app))
	rAuth.GET("/test-auth", func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		return e.JSON(http.StatusOK, map[string]string{"id": e.Auth.Id})
	})
	muxAuth, err := rAuth.BuildMux()
	if err != nil {
		t.Fatalf("build auth mux: %v", err)
	}

	reqAuth := httptest.NewRequest(http.MethodGet, "/test-auth", nil)
	reqAuth.Header.Set("X-Wireops-Api-Key", keyResp.ApiKey)
	recAuth := httptest.NewRecorder()
	muxAuth.ServeHTTP(recAuth, reqAuth)

	if recAuth.Code != http.StatusOK {
		t.Fatalf("expected auth 200, got %d: %s", recAuth.Code, recAuth.Body.String())
	}

	// Verify last used is updated
	reloadedUsed, _ := app.FindRecordById("service_accounts", sa.Id)
	if reloadedUsed.GetString("key_last_used_at") == "" {
		t.Fatal("expected key_last_used_at to be populated after auth")
	}

	// Rotate key to verify key_last_used_at is reset
	reqRotate := httptest.NewRequest(http.MethodPost, "/api/custom/service-accounts/"+sa.Id+"/keys", nil)
	recRotate := httptest.NewRecorder()
	mux.ServeHTTP(recRotate, reqRotate)
	if recRotate.Code != http.StatusCreated {
		t.Fatalf("expected 201 for key rotation, got %d: %s", recRotate.Code, recRotate.Body.String())
	}

	// Reload and verify key_last_used_at is reset to nil/empty
	reloadedRotated, _ := app.FindRecordById("service_accounts", sa.Id)
	if reloadedRotated.GetString("key_last_used_at") != "" {
		t.Fatal("expected key_last_used_at to be reset to nil/empty after key rotation")
	}

	// 2. Revoke key
	reqRevoke := httptest.NewRequest(http.MethodDelete, "/api/custom/service-accounts/"+sa.Id+"/keys", nil)
	recRevoke := httptest.NewRecorder()
	mux.ServeHTTP(recRevoke, reqRevoke)

	if recRevoke.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recRevoke.Code, recRevoke.Body.String())
	}

	// Reload and verify key is revoked
	reloadedRevoked, _ := app.FindRecordById("service_accounts", sa.Id)
	if !reloadedRevoked.GetBool("key_revoked") {
		t.Fatal("expected key_revoked to be true")
	}

	// Test middleware authentication with revoked key (should fail)
	reqFail := httptest.NewRequest(http.MethodGet, "/test-auth", nil)
	reqFail.Header.Set("X-Wireops-Api-Key", keyResp.ApiKey)
	recAuthFail := httptest.NewRecorder()
	muxAuth.ServeHTTP(recAuthFail, reqFail)

	if recAuthFail.Code != http.StatusUnauthorized {
		t.Fatalf("expected auth 401 for revoked key, got %d: %s", recAuthFail.Code, recAuthFail.Body.String())
	}
}

func TestDisableServiceAccountWipesKey(t *testing.T) {
	app, mux, _ := setupTestSAApp(t)
	sa := createTestSA(t, app, "Wipe Key SA", "Wipe Key SA description", rbac.RoleViewer, true)

	// 1. Issue a key by hitting the issue key endpoint
	reqIssue := httptest.NewRequest(http.MethodPost, "/api/custom/service-accounts/"+sa.Id+"/keys", nil)
	recIssue := httptest.NewRecorder()
	mux.ServeHTTP(recIssue, reqIssue)
	if recIssue.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", recIssue.Code, recIssue.Body.String())
	}

	// Reload to verify it has key prefix and hash
	reloaded, _ := app.FindRecordById("service_accounts", sa.Id)
	if reloaded.GetString("key_prefix") == "" || reloaded.GetString("key_hash") == "" {
		t.Fatal("expected key prefix and hash to be set")
	}

	// 2. Disable the service account by hitting the PUT update endpoint
	body := map[string]any{
		"enabled": false,
	}
	b, _ := json.Marshal(body)
	reqDisable := httptest.NewRequest(http.MethodPut, "/api/custom/service-accounts/"+sa.Id, bytes.NewBuffer(b))
	reqDisable.Header.Set("Content-Type", "application/json")
	recDisable := httptest.NewRecorder()
	mux.ServeHTTP(recDisable, reqDisable)
	if recDisable.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recDisable.Code, recDisable.Body.String())
	}

	// 3. Reload and verify key fields are wiped/revoked
	reloadedWiped, _ := app.FindRecordById("service_accounts", sa.Id)
	if reloadedWiped.GetBool("enabled") {
		t.Fatal("expected service account to be disabled")
	}
	if reloadedWiped.GetString("key_prefix") != "" {
		t.Fatalf("expected key_prefix to be empty, got %q", reloadedWiped.GetString("key_prefix"))
	}
	if reloadedWiped.GetString("key_hash") != "" {
		t.Fatalf("expected key_hash to be empty, got %q", reloadedWiped.GetString("key_hash"))
	}
	if !reloadedWiped.GetBool("key_revoked") {
		t.Fatal("expected key_revoked to be true")
	}
	if reloadedWiped.GetString("key_expires_at") != "" {
		t.Fatalf("expected key_expires_at to be empty, got %s", reloadedWiped.GetString("key_expires_at"))
	}
	if reloadedWiped.GetString("key_last_used_at") != "" {
		t.Fatalf("expected key_last_used_at to be empty, got %s", reloadedWiped.GetString("key_last_used_at"))
	}

	// 4. Try to re-enable the service account and expect 400 Bad Request
	bodyEnable := map[string]any{
		"enabled": true,
	}
	bEnable, _ := json.Marshal(bodyEnable)
	reqEnable := httptest.NewRequest(http.MethodPut, "/api/custom/service-accounts/"+sa.Id, bytes.NewBuffer(bEnable))
	reqEnable.Header.Set("Content-Type", "application/json")
	recEnable := httptest.NewRecorder()
	mux.ServeHTTP(recEnable, reqEnable)
	if recEnable.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when trying to re-enable disabled service account, got %d: %s", recEnable.Code, recEnable.Body.String())
	}
}

func TestDuplicateServiceAccountNameConstraint(t *testing.T) {
	_, mux, _ := setupTestSAApp(t)

	// 1. Create first service account with name "duplicate-sa"
	body1 := map[string]any{
		"name":        "duplicate-sa",
		"description": "Duplicate SA description",
		"role":        rbac.RoleViewer,
	}
	b1, _ := json.Marshal(body1)
	req1 := httptest.NewRequest(http.MethodPost, "/api/custom/service-accounts", bytes.NewBuffer(b1))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("expected 201 for first sa, got %d: %s", rec1.Code, rec1.Body.String())
	}

	// 2. Try to create second service account with the same name "duplicate-sa" (should fail)
	body2 := map[string]any{
		"name":        "duplicate-sa",
		"description": "Duplicate SA description",
		"role":        rbac.RoleViewer,
	}
	b2, _ := json.Marshal(body2)
	req2 := httptest.NewRequest(http.MethodPost, "/api/custom/service-accounts", bytes.NewBuffer(b2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	if rec2.Code == http.StatusCreated {
		t.Fatal("expected creation of duplicate service account to fail, but it succeeded")
	}
}

func TestDeleteServiceAccountBlocked(t *testing.T) {
	app, mux, _ := setupTestSAApp(t)
	sa := createTestSA(t, app, "Deletable SA", "Deletable SA description", rbac.RoleViewer, true)

	req := httptest.NewRequest(http.MethodDelete, "/api/custom/service-accounts/"+sa.Id, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(resp["error"], "deleting service accounts is not allowed") {
		t.Fatalf("expected error message about preserving audit logs, got: %s", resp["error"])
	}
}

func TestCreateDisabledServiceAccountBlocked(t *testing.T) {
	_, mux, _ := setupTestSAApp(t)

	body := map[string]any{
		"name":        "Test Disabled SA",
		"description": "Disabled SA description",
		"role":        rbac.RoleViewer,
		"enabled":     false,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/service-accounts", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when creating disabled service account, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "keys cannot be generated for disabled service accounts" {
		t.Fatalf("expected error message about disabled service accounts, got: %s", resp["error"])
	}
}
