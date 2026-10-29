package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestRegistryCredentialTestConnectionVerifiesSavedCredential(t *testing.T) {
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
	if resp["source"] != "persisted" || resp["credential_id"] != cred.Id || resp["credential_name"] != "Saved" {
		t.Fatalf("expected persisted-source metadata, got %#v", resp)
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
	errMessage, _ := resp["error"].(string)
	if !strings.Contains(errMessage, "restricted address") || !strings.Contains(errMessage, "ALLOWED_PRIVATE_IP_RANGES") || !strings.Contains(errMessage, "restart") {
		t.Fatalf("expected private-registry configuration guidance, got %#v", resp)
	}
	registryURL, err := url.Parse(registryServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errMessage, registryURL.Hostname()) {
		t.Fatalf("expected blocked address %q in error, got %#v", registryURL.Hostname(), resp)
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

// TestRegistryCredentialTestConnectionRejectsMixedDraftAndPersisted pins the
// strict persisted contract: credential_id probes exactly what is stored, so
// any draft field alongside it is refused rather than silently merged — the
// merge was the mechanism behind "the form test passed but the saved
// credential failed" (#186).
func TestRegistryCredentialTestConnectionRejectsMixedDraftAndPersisted(t *testing.T) {
	app, operator := registryCredentialTestApp(t)
	cred := createRegistryCredentialRow(t, app, "Saved", "registry.example.com", "basic", "deploy", "saved-secret", true)
	mux := registryCredentialRoutesMux(t, app, operator)

	for name, body := range map[string]map[string]any{
		"registry_url": {"credential_id": cred.Id, "registry_url": "registry.example.com"},
		"auth_type":    {"credential_id": cred.Id, "auth_type": "basic"},
		"username":     {"credential_id": cred.Id, "username": "deploy"},
		"password":     {"credential_id": cred.Id, "password": "draft-secret"},
		"insecure":     {"credential_id": cred.Id, "insecure": false},
	} {
		t.Run(name, func(t *testing.T) {
			b, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/api/custom/registry-credentials/test", bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestRegistryCredentialTestConnectionPersistedUsesSavedInsecure pins the
// other side of the old *bool merge semantics: in persisted mode the saved
// insecure flag drives the probe, so a saved insecure=true credential
// succeeds against a self-signed TLS registry.
func TestRegistryCredentialTestConnectionPersistedUsesSavedInsecure(t *testing.T) {
	registryServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer registryServer.Close()

	app, operator := registryCredentialTestApp(t)
	cred := createRegistryCredentialRow(t, app, "Saved", registryServer.URL, "basic", "deploy", "saved-secret", true)
	mux := registryCredentialRoutesMux(t, app, operator)

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
	if resp["success"] != true || resp["source"] != "persisted" {
		t.Fatalf("expected the saved insecure=true credential to succeed against the TLS registry, got %#v", resp)
	}
}

// TestRegistryCredentialTestConnectionDraftOkThenPersistedFails is the #188
// acceptance pair: the entered values pass the draft probe while the stored
// value (as deployed) fails the persisted probe — the two can only be told
// apart because credential_id no longer merges draft fields over the saved
// record.
func TestRegistryCredentialTestConnectionDraftOkThenPersistedFails(t *testing.T) {
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pass, ok := r.BasicAuth()
		if !ok || pass != "draft-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer registryServer.Close()

	app, operator := registryCredentialTestApp(t)
	cred := createRegistryCredentialRow(t, app, "Saved", registryServer.URL, "basic", "deploy", "saved-key", true)
	mux := registryCredentialRoutesMux(t, app, operator)

	post := func(t *testing.T, body map[string]any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/custom/registry-credentials/test", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	draft := post(t, map[string]any{
		"registry_url": registryServer.URL,
		"auth_type":    "basic",
		"username":     "deploy",
		"password":     "draft-key",
		"insecure":     true,
	})
	if draft["success"] != true || draft["source"] != "draft" {
		t.Fatalf("expected the entered values to pass the draft probe, got %#v", draft)
	}

	persisted := post(t, map[string]any{"credential_id": cred.Id})
	if persisted["success"] != false || persisted["source"] != "persisted" {
		t.Fatalf("expected the persisted probe to fail against the stored value, got %#v", persisted)
	}
	diagnostic, ok := persisted["diagnostic"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured diagnostics on the persisted failure, got %#v", persisted)
	}
	if diagnostic["stage"] != "challenge" || diagnostic["category"] != "auth_rejected" || diagnostic["http_status"] != float64(401) {
		t.Fatalf("expected challenge/auth_rejected/401 diagnostics, got %#v", diagnostic)
	}
	encoded, _ := json.Marshal(persisted)
	if strings.Contains(string(encoded), "draft-key") || strings.Contains(string(encoded), "saved-key") {
		t.Fatalf("diagnostic leaks a credential value: %s", encoded)
	}
}

func TestRegistryConnectionBearerChallenge(t *testing.T) {
	t.Setenv("ALLOWED_PRIVATE_IP_RANGES", "127.0.0.1/32")
	var endpoint string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/token" {
			u, p, ok := r.BasicAuth()
			if !ok || u != "wireops" || p != "valid-key" {
				w.WriteHeader(401)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"token":"valid-token"}`))
			return
		}
		if r.Header.Get("Authorization") == "Bearer valid-token" {
			w.WriteHeader(200)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+endpoint+`/auth/token",service="grom-registry"`)
		w.WriteHeader(401)
	}))
	defer server.Close()
	endpoint = server.URL
	host := strings.TrimPrefix(endpoint, "https://")
	for _, tc := range []struct {
		name, password string
		success        bool
	}{{"valid", "valid-key", true}, {"wrong", "wrong-key", false}} {
		t.Run(tc.name, func(t *testing.T) {
			err := testRegistryConnection(context.Background(), host, "wireops", tc.password, true)
			if (err == nil) != tc.success {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
