package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/constants"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/rbac"
)

func registryCredentialRoutesMux(t *testing.T, app core.App, auth *core.Record) http.Handler {
	t.Helper()

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
			Auth:  auth,
		}, nil
	})

	rr := routeRegistrar{r: r, app: app}
	rr.registerRegistryCredentialRoutes()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return mux
}

func registryCredentialTestApp(t *testing.T) (core.App, *core.Record) {
	t.Helper()
	t.Setenv(constants.EnvSecretKey, testSecretBackendKey)
	// Test registries are httptest servers on 127.0.0.1, which the SSRF
	// guard (validatePublicHost) rejects by default — allowlist loopback
	// here the same way an operator would opt in a real internal registry.
	t.Setenv(constants.EnvAllowedPrivateIPRanges, "127.0.0.1/32")
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	operator := createTestUser(t, app, "registry-operator@example.com", "Password1!", rbac.RoleOperator)
	return app, operator
}

func createRegistryCredentialRow(t *testing.T, app core.App, name, registryURL, authType, username, plainPassword string, insecure bool) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("registry_credentials")
	if err != nil {
		t.Fatalf("find registry_credentials collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("registry_url", registryURL)
	rec.Set("auth_type", authType)
	rec.Set("username", username)
	rec.Set("insecure", insecure)
	if plainPassword != "" {
		encrypted, err := crypto.Encrypt([]byte(plainPassword), []byte(testSecretBackendKey))
		if err != nil {
			t.Fatalf("encrypt password: %v", err)
		}
		rec.Set("password", encrypted)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save registry credential: %v", err)
	}
	return rec
}

func TestRegistryCredentialTestConnectionSuccess(t *testing.T) {
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "deploy" || pass != "hunter2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer registryServer.Close()

	app, operator := registryCredentialTestApp(t)
	mux := registryCredentialRoutesMux(t, app, operator)

	body := map[string]any{
		"registry_url": registryServer.URL,
		"auth_type":    "basic",
		"username":     "deploy",
		"password":     "hunter2",
		// The test registry is a plain httptest.NewServer (HTTP, no TLS), so
		// insecure=true is required for the route's HTTPS-first handshake to
		// fall back to HTTP the way it would for a real insecure registry.
		"insecure": true,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/registry-credentials/test", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["success"] != true {
		t.Fatalf("expected success=true, got %#v", resp)
	}
}

func TestRegistryCredentialTestConnectionFailure(t *testing.T) {
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer registryServer.Close()

	app, operator := registryCredentialTestApp(t)
	mux := registryCredentialRoutesMux(t, app, operator)

	body := map[string]any{
		"registry_url": registryServer.URL,
		"auth_type":    "basic",
		"username":     "deploy",
		"password":     "wrong",
		"insecure":     true,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/registry-credentials/test", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (failure reported in body, not status), got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["success"] != false {
		t.Fatalf("expected success=false, got %#v", resp)
	}
	if resp["error"] == nil || resp["error"] == "" {
		t.Fatalf("expected an error message, got %#v", resp)
	}
}

func TestRegistryCredentialTestConnectionMergesSavedCredential(t *testing.T) {
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "deploy" || pass != "saved-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer registryServer.Close()

	app, operator := registryCredentialTestApp(t)
	cred := createRegistryCredentialRow(t, app, "Saved", registryServer.URL, "basic", "deploy", "saved-secret", true)
	mux := registryCredentialRoutesMux(t, app, operator)

	// No password in the body — the route should fall back to the saved,
	// decrypted credential, mirroring /api/custom/credentials/test's merge
	// behavior for editing without re-entering an already-saved secret.
	body := map[string]any{"credential_id": cred.Id}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/registry-credentials/test", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["success"] != true {
		t.Fatalf("expected success=true using saved credential, got %#v", resp)
	}
}

func TestRegistryCredentialTestConnectionWarnsOnInvalidJSON(t *testing.T) {
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer registryServer.Close()

	app, operator := registryCredentialTestApp(t)
	mux := registryCredentialRoutesMux(t, app, operator)

	body := map[string]any{
		"registry_url": registryServer.URL,
		"auth_type":    "gcp_service_account",
		"username":     "_json_key",
		"password":     "not-json",
		"insecure":     true,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/registry-credentials/test", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["warning"] == nil || resp["warning"] == "" {
		t.Fatalf("expected a JSON validity warning, got %#v", resp)
	}
	// The warning is informational only — it must not block the handshake
	// from still reporting success.
	if resp["success"] != true {
		t.Fatalf("expected success=true despite the JSON warning, got %#v", resp)
	}
}

// TestRegistryCredentialTestConnectionBlocksPrivateHost pins the SSRF
// guard: without an explicit ALLOWED_PRIVATE_IP_RANGES opt-in, a
// registry_url resolving to a loopback/private address must be rejected
// before the server ever dials it — otherwise an authenticated operator
// could point this endpoint at internal services (or cloud metadata
// endpoints) and have the server make the request on their behalf.
func TestRegistryCredentialTestConnectionBlocksPrivateHost(t *testing.T) {
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer registryServer.Close()

	app, operator := registryCredentialTestApp(t)
	t.Setenv(constants.EnvAllowedPrivateIPRanges, "")
	mux := registryCredentialRoutesMux(t, app, operator)

	body := map[string]any{
		"registry_url": registryServer.URL,
		"auth_type":    "basic",
		"username":     "deploy",
		"password":     "hunter2",
		"insecure":     true,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/registry-credentials/test", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["success"] != false {
		t.Fatalf("expected the private-host request to be rejected, got %#v", resp)
	}
	if resp["error"] != "connecting to private or loopback addresses is not allowed" {
		t.Fatalf("unexpected error message: %#v", resp)
	}
}

func TestRegistryCredentialTestConnectionRequiresRegistryURL(t *testing.T) {
	app, operator := registryCredentialTestApp(t)
	mux := registryCredentialRoutesMux(t, app, operator)

	body := map[string]any{"auth_type": "basic", "username": "deploy", "password": "hunter2"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/registry-credentials/test", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["success"] != false || resp["error"] != "registry_url is required" {
		t.Fatalf("expected registry_url validation error, got %#v", resp)
	}
}

func TestRegistryCredentialTestConnectionUnknownCredentialID(t *testing.T) {
	app, operator := registryCredentialTestApp(t)
	mux := registryCredentialRoutesMux(t, app, operator)

	body := map[string]any{"credential_id": "does-not-exist"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/registry-credentials/test", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["success"] != false || resp["error"] == nil || resp["error"] == "" {
		t.Fatalf("expected a credential load error, got %#v", resp)
	}
}

// TestRegistryCredentialTestConnectionExplicitFalseOverridesSavedInsecure
// pins the *bool merge semantics: an explicit `"insecure": false` in the
// request body must NOT be overwritten by a saved credential's
// `insecure: true` — only omitting the field entirely should inherit the
// saved value. Before this was a plain bool, false (the zero value) was
// indistinguishable from "not provided" and got silently overridden.
func TestRegistryCredentialTestConnectionExplicitFalseOverridesSavedInsecure(t *testing.T) {
	// TLS server so InsecureSkipVerify is required — if the explicit
	// `insecure: false` were (bugged) overridden back to true by the saved
	// credential, the request would still incorrectly succeed.
	registryServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer registryServer.Close()

	app, operator := registryCredentialTestApp(t)
	cred := createRegistryCredentialRow(t, app, "Saved", registryServer.URL, "basic", "deploy", "saved-secret", true)
	mux := registryCredentialRoutesMux(t, app, operator)

	body := map[string]any{"credential_id": cred.Id, "insecure": false}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/registry-credentials/test", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["success"] != false {
		t.Fatalf("expected the explicit insecure=false to be honored (self-signed cert rejected), got %#v", resp)
	}
}
