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

	_ "github.com/wireops/wireops/internal/integrations/infisical"
	_ "github.com/wireops/wireops/internal/integrations/vault"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/secrets"
)

const testSecretBackendKey = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" // 32 bytes

func setupVaultBrowseTestApp(t *testing.T) (core.App, http.Handler) {
	t.Helper()
	t.Setenv("SECRET_KEY", testSecretBackendKey)

	app := newSetupTestApp(t)

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
		}, nil
	})

	rr := routeRegistrar{r: r, app: app}
	rr.registerIntegrationRoutes(crypto.NormalizeSecretKey(testSecretBackendKey))
	rr.registerVaultBrowseRoutes()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux
}

func setupVaultBrowseTestAppAuthenticated(t *testing.T) (core.App, http.Handler, *core.Record) {
	t.Helper()
	t.Setenv("SECRET_KEY", testSecretBackendKey)

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

	rr := routeRegistrar{r: r, app: app}
	rr.registerIntegrationRoutes(crypto.NormalizeSecretKey(testSecretBackendKey))
	rr.registerVaultBrowseRoutes()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux, admin
}

func assertUnauthenticated(t *testing.T, mux http.Handler, path string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET %s: expected 401 for unauthenticated request, got %d: %s", path, rec.Code, rec.Body.String())
	}
}

// Vault/Infisical connection config now lives in the same "integrations"
// collection/routes as every other integration (webhook, ntfy, ...), and the
// Vault browse endpoints sit alongside it under /api/custom/integrations/vault/*.
// Both are gated by RBAC (CapManageSettings for config, CapOperateStacks for
// browsing) — an unauthenticated request should never reach the handler,
// since /fields in particular talks to Vault using the stored token on the
// caller's behalf.
func TestVaultIntegrationRoutesRequireAuth(t *testing.T) {
	_, mux := setupVaultBrowseTestApp(t)

	for _, path := range []string{
		"/api/custom/integrations",
		"/api/custom/integrations/vault/mounts",
		"/api/custom/integrations/vault/browse?mount=secret",
		"/api/custom/integrations/vault/fields?mount=secret&path=myapp",
	} {
		assertUnauthenticated(t, mux, path)
	}
}

// TestVaultFieldsExtractionNeverLeaksValues exercises the exact field-name
// extraction the /fields handler performs against a mocked Vault KV v2
// secret, and asserts only field *names* come out — never the secret
// values — since this browse endpoint must not become a way to exfiltrate
// secret contents.
func TestVaultFieldsExtractionNeverLeaksValues(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretBackendKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"data":{"DB_PASS":"s3cr3t-value-must-not-leak","API_KEY":"another-secret-value"}}}`))
	}))
	defer srv.Close()

	app := newSetupTestApp(t)
	integrationsCol, err := app.FindCollectionByNameOrId("integrations")
	if err != nil {
		t.Fatalf("find integrations collection: %v", err)
	}
	rec := core.NewRecord(integrationsCol)
	rec.Set("slug", "vault")
	rec.Set("enabled", true)
	rec.Set("config", map[string]any{
		"address": srv.URL,
		"token":   mustEncryptForRouteTest(t, "s.mytoken"),
	})
	if err := app.Save(rec); err != nil {
		t.Fatalf("save vault integration config: %v", err)
	}

	client, _, err := secrets.BuildVaultClient(app)
	if err != nil {
		t.Fatalf("build vault client: %v", err)
	}
	secret, err := client.Logical().ReadWithContext(t.Context(), "secret/data/myapp")
	if err != nil {
		t.Fatalf("read secret: %v", err)
	}
	data := secret.Data["data"].(map[string]interface{})

	fields := make([]string, 0, len(data))
	for k := range data {
		fields = append(fields, k)
	}
	joined := strings.Join(fields, ",")

	if strings.Contains(joined, "s3cr3t-value-must-not-leak") || strings.Contains(joined, "another-secret-value") {
		t.Fatalf("field name list leaked a secret value: %v", fields)
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 field names, got %v", fields)
	}
}

func setVaultBackendConfig(t *testing.T, app core.App, config map[string]any) {
	t.Helper()
	integrationsCol, err := app.FindCollectionByNameOrId("integrations")
	if err != nil {
		t.Fatalf("find integrations collection: %v", err)
	}
	rec := core.NewRecord(integrationsCol)
	rec.Set("slug", "vault")
	rec.Set("enabled", true)
	rec.Set("config", config)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save vault integration config: %v", err)
	}
}

func TestVaultMountsFiltersToAllowedMount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"secret/":{"type":"kv","options":{"version":"2"}},"other/":{"type":"kv","options":{"version":"2"}}}`))
	}))
	defer srv.Close()

	app, mux, _ := setupVaultBrowseTestAppAuthenticated(t)
	setVaultBackendConfig(t, app, map[string]any{
		"address":       srv.URL,
		"token":         mustEncryptForRouteTest(t, "s.mytoken"),
		"allowed_mount": "secret",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/custom/integrations/vault/mounts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out []vaultMountInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out) != 1 || out[0].Path != "secret" {
		t.Fatalf("expected only 'secret' mount, got %+v", out)
	}
}

func TestVaultBrowseRejectsOutOfScopeMount(t *testing.T) {
	app, mux, _ := setupVaultBrowseTestAppAuthenticated(t)
	setVaultBackendConfig(t, app, map[string]any{
		"address":       "http://example.invalid",
		"token":         mustEncryptForRouteTest(t, "s.mytoken"),
		"allowed_mount": "secret",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/custom/integrations/vault/browse?mount=other", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestVaultFieldsRejectsOutOfScopeMount(t *testing.T) {
	app, mux, _ := setupVaultBrowseTestAppAuthenticated(t)
	setVaultBackendConfig(t, app, map[string]any{
		"address":       "http://example.invalid",
		"token":         mustEncryptForRouteTest(t, "s.mytoken"),
		"allowed_mount": "secret",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/custom/integrations/vault/fields?mount=other&path=myapp", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestVaultTestConnectionSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/auth/token/lookup-self"):
			w.Write([]byte(`{"data":{"id":"s.mytoken"}}`))
		case strings.HasPrefix(r.URL.Path, "/v1/sys/mounts"):
			w.Write([]byte(`{"secret/":{"type":"kv","options":{"version":"2"}}}`))
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	_, mux, _ := setupVaultBrowseTestAppAuthenticated(t)

	body, _ := json.Marshal(map[string]string{"address": srv.URL, "token": "s.mytoken", "allowed_mount": "secret"})
	req := httptest.NewRequest(http.MethodPost, "/api/custom/integrations/vault/test", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["success"] != "true" {
		t.Fatalf("expected success, got %+v", out)
	}
}

func TestVaultTestConnectionBadToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"errors":["permission denied"]}`))
	}))
	defer srv.Close()

	_, mux, _ := setupVaultBrowseTestAppAuthenticated(t)

	body, _ := json.Marshal(map[string]string{"address": srv.URL, "token": "s.badtoken"})
	req := httptest.NewRequest(http.MethodPost, "/api/custom/integrations/vault/test", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["success"] != "false" {
		t.Fatalf("expected failure, got %+v", out)
	}
}

func TestVaultTestConnectionMountNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/auth/token/lookup-self"):
			w.Write([]byte(`{"data":{"id":"s.mytoken"}}`))
		case strings.HasPrefix(r.URL.Path, "/v1/sys/mounts"):
			w.Write([]byte(`{"other/":{"type":"kv","options":{"version":"2"}}}`))
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	_, mux, _ := setupVaultBrowseTestAppAuthenticated(t)

	body, _ := json.Marshal(map[string]string{"address": srv.URL, "token": "s.mytoken", "allowed_mount": "secret"})
	req := httptest.NewRequest(http.MethodPost, "/api/custom/integrations/vault/test", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["success"] != "false" {
		t.Fatalf("expected failure for missing mount, got %+v", out)
	}
}

// TestVaultTestConnectionResolvesMaskedToken covers the exact bug where the
// frontend resubmits the "••••••••" placeholder for an unchanged, already
// saved token: the masked value must be resolved back to the stored
// ciphertext AND decrypted before being used to authenticate, not passed to
// Vault as-is (which previously caused every test-connection call on an
// existing integration to fail with "invalid credentials").
func TestVaultTestConnectionResolvesMaskedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/auth/token/lookup-self"):
			if r.Header.Get("X-Vault-Token") != "s.realtoken" {
				t.Fatalf("expected decrypted token, got %q", r.Header.Get("X-Vault-Token"))
			}
			w.Write([]byte(`{"data":{"id":"s.realtoken"}}`))
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	app, mux, _ := setupVaultBrowseTestAppAuthenticated(t)
	setVaultBackendConfig(t, app, map[string]any{
		"address": srv.URL,
		"token":   mustEncryptForRouteTest(t, "s.realtoken"),
	})

	body, _ := json.Marshal(map[string]string{"address": srv.URL, "token": "••••••••"})
	req := httptest.NewRequest(http.MethodPost, "/api/custom/integrations/vault/test", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["success"] != "true" {
		t.Fatalf("expected success, got %+v", out)
	}
}

func mustEncryptForRouteTest(t *testing.T, plaintext string) string {
	t.Helper()
	enc, err := crypto.Encrypt([]byte(plaintext), []byte(testSecretBackendKey))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return enc
}
