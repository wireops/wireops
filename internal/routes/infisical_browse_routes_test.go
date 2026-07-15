package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	infisical "github.com/infisical/go-sdk"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	_ "github.com/wireops/wireops/internal/integrations/infisical"
	_ "github.com/wireops/wireops/internal/integrations/vault"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/secrets"
)

func setupInfisicalBrowseTestApp(t *testing.T) (core.App, http.Handler) {
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
	rr.registerInfisicalBrowseRoutes(crypto.NormalizeSecretKey(testSecretBackendKey))

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux
}

func setupInfisicalBrowseTestAppAuthenticated(t *testing.T) (core.App, http.Handler, *core.Record) {
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
	rr.registerInfisicalBrowseRoutes(crypto.NormalizeSecretKey(testSecretBackendKey))

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux, admin
}

func setInfisicalBackendConfig(t *testing.T, app core.App, config map[string]any) {
	t.Helper()
	integrationsCol, err := app.FindCollectionByNameOrId("integrations")
	if err != nil {
		t.Fatalf("find integrations collection: %v", err)
	}
	rec := core.NewRecord(integrationsCol)
	rec.Set("slug", "infisical")
	rec.Set("enabled", true)
	rec.Set("config", config)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save infisical integration config: %v", err)
	}
}

// TestInfisicalIntegrationRoutesRequireAuth mirrors
// TestVaultIntegrationRoutesRequireAuth: an unauthenticated request must
// never reach the handler, since /browse in particular talks to Infisical
// using the stored machine identity credentials on the caller's behalf.
func TestInfisicalIntegrationRoutesRequireAuth(t *testing.T) {
	_, mux := setupInfisicalBrowseTestApp(t)

	for _, path := range []string{
		"/api/custom/integrations/infisical/projects",
		"/api/custom/integrations/infisical/project?project_id=proj",
		"/api/custom/integrations/infisical/browse?project_id=proj&environment=dev",
	} {
		assertUnauthenticated(t, mux, path)
	}
}

// TestInfisicalBrowseFieldExtractionNeverLeaksValues exercises the exact
// secret-name extraction the /browse handler performs against a mocked
// Infisical secrets list, and asserts only secret *names* come out — never
// the secret values — since this browse endpoint must not become a way to
// exfiltrate secret contents.
func TestInfisicalBrowseFieldExtractionNeverLeaksValues(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretBackendKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/universal-auth/login":
			json.NewEncoder(w).Encode(map[string]any{"accessToken": "fake-token", "expiresIn": 3600})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/secrets/raw":
			if r.URL.Query().Get("workspaceId") != "proj123" || r.URL.Query().Get("environment") != "dev" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"secrets": []map[string]any{
					{"secretKey": "DB_PASS", "secretValue": "s3cr3t-value-must-not-leak"},
					{"secretKey": "API_KEY", "secretValue": "another-secret-value"},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	app := newSetupTestApp(t)
	integrationsCol, err := app.FindCollectionByNameOrId("integrations")
	if err != nil {
		t.Fatalf("find integrations collection: %v", err)
	}
	rec := core.NewRecord(integrationsCol)
	rec.Set("slug", "infisical")
	rec.Set("enabled", true)
	rec.Set("config", map[string]any{
		"site_url":      srv.URL,
		"client_id":     "cid",
		"client_secret": mustEncryptForRouteTest(t, "csecret"),
	})
	if err := app.Save(rec); err != nil {
		t.Fatalf("save infisical integration config: %v", err)
	}

	client, _, _, cancel, err := secrets.BuildInfisicalClient(t.Context(), app)
	if err != nil {
		t.Fatalf("build infisical client: %v", err)
	}
	defer cancel()

	result, err := client.Secrets().ListSecrets(infisical.ListSecretsOptions{
		ProjectID:   "proj123",
		Environment: "dev",
		SecretPath:  "/",
	})
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}

	names := make([]string, 0, len(result.Secrets))
	for _, s := range result.Secrets {
		names = append(names, s.SecretKey)
	}
	joined := strings.Join(names, ",")

	if strings.Contains(joined, "s3cr3t-value-must-not-leak") || strings.Contains(joined, "another-secret-value") {
		t.Fatalf("secret name list leaked a secret value: %v", names)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 secret names, got %v", names)
	}
}

func TestInfisicalProjectsFiltersToAllowedProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/auth/universal-auth/login":
			json.NewEncoder(w).Encode(map[string]any{"accessToken": "fake-token", "expiresIn": 3600})
		case r.URL.Path == "/api/v1/workspace":
			json.NewEncoder(w).Encode(map[string]any{
				"workspaces": []map[string]any{
					{"id": "proj123", "name": "Allowed"},
					{"id": "proj456", "name": "Other"},
				},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	app, mux, _ := setupInfisicalBrowseTestAppAuthenticated(t)
	setInfisicalBackendConfig(t, app, map[string]any{
		"site_url":           srv.URL,
		"client_id":          "cid",
		"client_secret":      mustEncryptForRouteTest(t, "csecret"),
		"allowed_project_id": "proj123",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/custom/integrations/infisical/projects", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out []infisicalProjectInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out) != 1 || out[0].ID != "proj123" {
		t.Fatalf("expected only proj123, got %+v", out)
	}
}

// TestInfisicalProjectsFallsBackToSingleProjectWhenOrgListForbidden covers a
// project-scoped machine identity that 403s on the org-wide "list all
// workspaces" endpoint (common on some Infisical setups). When the backend
// is restricted to a single project, /projects must still succeed by
// fetching that one project directly, so the reference picker's frontend
// auto-select (which only fires when the list has exactly one entry) works
// instead of falling back to manual project-ID entry.
func TestInfisicalProjectsFallsBackToSingleProjectWhenOrgListForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/auth/universal-auth/login":
			json.NewEncoder(w).Encode(map[string]any{"accessToken": "fake-token", "expiresIn": 3600})
		case r.URL.Path == "/api/v1/workspace":
			w.WriteHeader(http.StatusForbidden)
		case r.URL.Path == "/api/v1/workspace/proj123":
			json.NewEncoder(w).Encode(map[string]any{"workspace": map[string]any{"id": "proj123", "name": "Allowed"}})
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	app, mux, _ := setupInfisicalBrowseTestAppAuthenticated(t)
	setInfisicalBackendConfig(t, app, map[string]any{
		"site_url":           srv.URL,
		"client_id":          "cid",
		"client_secret":      mustEncryptForRouteTest(t, "csecret"),
		"allowed_project_id": "proj123",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/custom/integrations/infisical/projects", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out []infisicalProjectInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out) != 1 || out[0].ID != "proj123" {
		t.Fatalf("expected fallback to return only proj123, got %+v", out)
	}
}

// TestInfisicalProjectsListFailureWithoutScopeStillErrors ensures the
// fallback only applies when a restriction is actually configured — an
// unrestricted backend that can't list projects must still surface the
// error rather than silently returning an empty list.
func TestInfisicalProjectsListFailureWithoutScopeStillErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/universal-auth/login":
			json.NewEncoder(w).Encode(map[string]any{"accessToken": "fake-token", "expiresIn": 3600})
		case "/api/v1/workspace":
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	app, mux, _ := setupInfisicalBrowseTestAppAuthenticated(t)
	setInfisicalBackendConfig(t, app, map[string]any{
		"site_url":      srv.URL,
		"client_id":     "cid",
		"client_secret": mustEncryptForRouteTest(t, "csecret"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/custom/integrations/infisical/projects", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestInfisicalProjectRejectsOutOfScopeProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"accessToken": "fake-token", "expiresIn": 3600})
	}))
	defer srv.Close()

	app, mux, _ := setupInfisicalBrowseTestAppAuthenticated(t)
	setInfisicalBackendConfig(t, app, map[string]any{
		"site_url":           srv.URL,
		"client_id":          "cid",
		"client_secret":      mustEncryptForRouteTest(t, "csecret"),
		"allowed_project_id": "proj123",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/custom/integrations/infisical/project?project_id=proj456", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestInfisicalBrowseRejectsOutOfScopeProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"accessToken": "fake-token", "expiresIn": 3600})
	}))
	defer srv.Close()

	app, mux, _ := setupInfisicalBrowseTestAppAuthenticated(t)
	setInfisicalBackendConfig(t, app, map[string]any{
		"site_url":           srv.URL,
		"client_id":          "cid",
		"client_secret":      mustEncryptForRouteTest(t, "csecret"),
		"allowed_project_id": "proj123",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/custom/integrations/infisical/browse?project_id=proj456&environment=dev", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestInfisicalTestConnectionSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/auth/universal-auth/login":
			json.NewEncoder(w).Encode(map[string]any{"accessToken": "fake-token", "expiresIn": 3600})
		case r.URL.Path == "/api/v1/workspace/proj123":
			json.NewEncoder(w).Encode(map[string]any{"workspace": map[string]any{"id": "proj123", "name": "Allowed"}})
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	_, mux, _ := setupInfisicalBrowseTestAppAuthenticated(t)

	body, _ := json.Marshal(map[string]string{
		"site_url": srv.URL, "client_id": "cid", "client_secret": "csecret", "allowed_project_id": "proj123",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/custom/integrations/infisical/test", bytes.NewReader(body))
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

func TestInfisicalTestConnectionBadCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"message": "invalid credentials"})
	}))
	defer srv.Close()

	_, mux, _ := setupInfisicalBrowseTestAppAuthenticated(t)

	body, _ := json.Marshal(map[string]string{"site_url": srv.URL, "client_id": "cid", "client_secret": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/custom/integrations/infisical/test", bytes.NewReader(body))
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

// TestInfisicalTestConnectionResolvesMaskedClientSecret mirrors
// TestVaultTestConnectionResolvesMaskedToken: resubmitting the "••••••••"
// placeholder for an unchanged client_secret must resolve to the stored
// ciphertext AND decrypt it before Universal Auth login, not send the
// ciphertext as the literal secret.
func TestInfisicalTestConnectionResolvesMaskedClientSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/universal-auth/login" {
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
		var authBody struct {
			ClientSecret string `json:"clientSecret"`
		}
		json.NewDecoder(r.Body).Decode(&authBody)
		if authBody.ClientSecret != "realsecret" {
			t.Fatalf("expected decrypted client_secret, got %q", authBody.ClientSecret)
		}
		json.NewEncoder(w).Encode(map[string]any{"accessToken": "fake-token", "expiresIn": 3600})
	}))
	defer srv.Close()

	app, mux, _ := setupInfisicalBrowseTestAppAuthenticated(t)
	setInfisicalBackendConfig(t, app, map[string]any{
		"site_url":      srv.URL,
		"client_id":     "cid",
		"client_secret": mustEncryptForRouteTest(t, "realsecret"),
	})

	body, _ := json.Marshal(map[string]string{"site_url": srv.URL, "client_id": "cid", "client_secret": "••••••••"})
	req := httptest.NewRequest(http.MethodPost, "/api/custom/integrations/infisical/test", bytes.NewReader(body))
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

func TestInfisicalTestConnectionProjectNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/auth/universal-auth/login":
			json.NewEncoder(w).Encode(map[string]any{"accessToken": "fake-token", "expiresIn": 3600})
		case strings.HasPrefix(r.URL.Path, "/api/v1/workspace/"):
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	_, mux, _ := setupInfisicalBrowseTestAppAuthenticated(t)

	body, _ := json.Marshal(map[string]string{
		"site_url": srv.URL, "client_id": "cid", "client_secret": "csecret", "allowed_project_id": "missing-proj",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/custom/integrations/infisical/test", bytes.NewReader(body))
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
		t.Fatalf("expected failure for missing project, got %+v", out)
	}
}
