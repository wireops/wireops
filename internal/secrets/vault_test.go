package secrets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wireops/wireops/internal/crypto"
)

const testSecretKey = "12345678901234567890123456789012"

func encryptForTest(t *testing.T, plaintext string) string {
	t.Helper()
	enc, err := crypto.Encrypt([]byte(plaintext), []byte(testSecretKey))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return enc
}

func TestVaultResolveSuccess(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/data/myapp" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Vault-Token") != "s.mytoken" {
			t.Fatalf("unexpected token header: %s", r.Header.Get("X-Vault-Token"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"data": map[string]any{
					"DB_PASS": "s3cr3t",
				},
			},
		})
	}))
	defer srv.Close()

	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "vault", true, map[string]any{
		"address": srv.URL,
		"token":   encryptForTest(t, "s.mytoken"),
	})

	p := NewVaultProvider(app)
	got, err := p.Resolve(context.Background(), "secret/data/myapp#DB_PASS")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("Resolve = %q, want s3cr3t", got)
	}
}

func TestVaultResolveMissingBackendConfig(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)
	app := newSecretBackendsTestApp(t)

	p := NewVaultProvider(app)
	_, err := p.Resolve(context.Background(), "secret/data/myapp#DB_PASS")
	if err == nil {
		t.Fatal("expected error for unconfigured vault backend, got nil")
	}
}

func TestVaultResolveDisabledBackend(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)
	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "vault", false, map[string]any{
		"address": "http://example.invalid",
		"token":   encryptForTest(t, "s.mytoken"),
	})

	p := NewVaultProvider(app)
	_, err := p.Resolve(context.Background(), "secret/data/myapp#DB_PASS")
	if err == nil {
		t.Fatal("expected error for disabled vault backend, got nil")
	}
}

func TestVaultResolveMalformedRawValue(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)
	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "vault", true, map[string]any{
		"address": "http://example.invalid",
		"token":   encryptForTest(t, "s.mytoken"),
	})

	p := NewVaultProvider(app)
	for _, raw := range []string{"", "no-hash-here", "mount/path#", "#field"} {
		if _, err := p.Resolve(context.Background(), raw); err == nil {
			t.Fatalf("Resolve(%q) expected error, got nil", raw)
		}
	}
}

func TestVaultResolveFieldNotFound(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"data": map[string]any{
					"OTHER_FIELD": "value",
				},
			},
		})
	}))
	defer srv.Close()

	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "vault", true, map[string]any{
		"address": srv.URL,
		"token":   encryptForTest(t, "s.mytoken"),
	})

	p := NewVaultProvider(app)
	_, err := p.Resolve(context.Background(), "secret/data/myapp#DB_PASS")
	if err == nil {
		t.Fatal("expected error for missing field, got nil")
	}
}

func TestVaultResolveRejectsOutOfScopeMount(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)
	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "vault", true, map[string]any{
		"address":       "http://example.invalid",
		"token":         encryptForTest(t, "s.mytoken"),
		"allowed_mount": "secret",
	})

	p := NewVaultProvider(app)
	_, err := p.Resolve(context.Background(), "other/data/myapp#DB_PASS")
	if err == nil {
		t.Fatal("expected error for out-of-scope mount, got nil")
	}
}

func TestVaultResolveAllowsMatchingMount(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"data": map[string]any{
					"DB_PASS": "s3cr3t",
				},
			},
		})
	}))
	defer srv.Close()

	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "vault", true, map[string]any{
		"address":       srv.URL,
		"token":         encryptForTest(t, "s.mytoken"),
		"allowed_mount": "secret",
	})

	p := NewVaultProvider(app)
	got, err := p.Resolve(context.Background(), "secret/data/myapp#DB_PASS")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("Resolve = %q, want s3cr3t", got)
	}
}

func TestVaultResolveSecretNotFound(t *testing.T) {
	t.Setenv("SECRET_KEY", testSecretKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"errors": []string{}})
	}))
	defer srv.Close()

	app := newSecretBackendsTestApp(t)
	mustCreateBackendRecord(t, app, "vault", true, map[string]any{
		"address": srv.URL,
		"token":   encryptForTest(t, "s.mytoken"),
	})

	p := NewVaultProvider(app)
	_, err := p.Resolve(context.Background(), "secret/data/myapp#DB_PASS")
	if err == nil {
		t.Fatal("expected error for 404 secret, got nil")
	}
}
