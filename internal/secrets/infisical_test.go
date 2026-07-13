package secrets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInfisicalResolveSuccess(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/universal-auth/login":
			var body struct {
				ClientID     string `json:"clientId"`
				ClientSecret string `json:"clientSecret"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.ClientID != "cid" || body.ClientSecret != "csecret" {
				t.Fatalf("unexpected auth body: %+v", body)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"accessToken": "fake-access-token",
				"expiresIn":   3600,
				"tokenType":   "Bearer",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/secrets/raw/DB_PASS":
			if r.URL.Query().Get("workspaceId") != "proj123" {
				t.Fatalf("unexpected workspaceId: %s", r.URL.Query().Get("workspaceId"))
			}
			if r.URL.Query().Get("environment") != "prod" {
				t.Fatalf("unexpected environment: %s", r.URL.Query().Get("environment"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"secret": map[string]any{
					"secretKey":   "DB_PASS",
					"secretValue": "s3cr3t",
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "infisical", true, map[string]any{
		"site_url":      srv.URL,
		"client_id":     "cid",
		"client_secret": encryptForTest(t, "csecret"),
	})

	p := NewInfisicalProvider(app)
	got, err := p.Resolve(context.Background(), "proj123/prod#DB_PASS")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("Resolve = %q, want s3cr3t", got)
	}
}

func TestInfisicalResolveMissingBackendConfig(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)
	app := newSecretBackendsTestApp(t)

	p := NewInfisicalProvider(app)
	_, err := p.Resolve(context.Background(), "proj123/prod#DB_PASS")
	if err == nil {
		t.Fatal("expected error for unconfigured infisical backend, got nil")
	}
}

func TestInfisicalResolveDisabledBackend(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)
	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "infisical", false, map[string]any{
		"site_url":      "http://example.invalid",
		"client_id":     "cid",
		"client_secret": encryptForTest(t, "csecret"),
	})

	p := NewInfisicalProvider(app)
	_, err := p.Resolve(context.Background(), "proj123/prod#DB_PASS")
	if err == nil {
		t.Fatal("expected error for disabled infisical backend, got nil")
	}
}

func TestInfisicalResolveMalformedRawValue(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)
	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "infisical", true, map[string]any{
		"site_url":      "http://example.invalid",
		"client_id":     "cid",
		"client_secret": encryptForTest(t, "csecret"),
	})

	p := NewInfisicalProvider(app)
	for _, raw := range []string{"", "no-hash-here", "onlyproject#field", "proj#", "#field"} {
		if _, err := p.Resolve(context.Background(), raw); err == nil {
			t.Fatalf("Resolve(%q) expected error, got nil", raw)
		}
	}
}

func TestInfisicalResolveRejectsOutOfScopeProject(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/universal-auth/login":
			json.NewEncoder(w).Encode(map[string]any{"accessToken": "fake-token", "expiresIn": 3600})
		default:
			t.Fatalf("unexpected request: %s %s (should be rejected before retrieval)", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "infisical", true, map[string]any{
		"site_url":           srv.URL,
		"client_id":          "cid",
		"client_secret":      encryptForTest(t, "csecret"),
		"allowed_project_id": "proj123",
	})

	p := NewInfisicalProvider(app)
	_, err := p.Resolve(context.Background(), "other-proj/prod#DB_PASS")
	if err == nil {
		t.Fatal("expected error for out-of-scope project, got nil")
	}
}

func TestInfisicalResolveAllowsMatchingProject(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/auth/universal-auth/login":
			json.NewEncoder(w).Encode(map[string]any{"accessToken": "fake-token", "expiresIn": 3600})
		case r.URL.Path == "/api/v3/secrets/raw/DB_PASS":
			json.NewEncoder(w).Encode(map[string]any{
				"secret": map[string]any{"secretKey": "DB_PASS", "secretValue": "s3cr3t"},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "infisical", true, map[string]any{
		"site_url":           srv.URL,
		"client_id":          "cid",
		"client_secret":      encryptForTest(t, "csecret"),
		"allowed_project_id": "proj123",
	})

	p := NewInfisicalProvider(app)
	got, err := p.Resolve(context.Background(), "proj123/prod#DB_PASS")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("Resolve = %q, want s3cr3t", got)
	}
}

func TestInfisicalResolveWithSecretPath(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/auth/universal-auth/login":
			json.NewEncoder(w).Encode(map[string]any{"accessToken": "fake-token", "expiresIn": 3600})
		case r.URL.Path == "/api/v3/secrets/raw/API_KEY":
			if r.URL.Query().Get("secretPath") != "/nested/path" {
				t.Fatalf("unexpected secretPath: %s", r.URL.Query().Get("secretPath"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"secret": map[string]any{"secretKey": "API_KEY", "secretValue": "abc"},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "infisical", true, map[string]any{
		"site_url":      srv.URL,
		"client_id":     "cid",
		"client_secret": encryptForTest(t, "csecret"),
	})

	p := NewInfisicalProvider(app)
	got, err := p.Resolve(context.Background(), "proj123/prod/nested/path#API_KEY")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if got != "abc" {
		t.Fatalf("Resolve = %q, want abc", got)
	}
}
