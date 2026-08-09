package executor

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareRegistryAuthEmptyIsNoop(t *testing.T) {
	dir, cleanup, err := prepareRegistryAuth("stack-1", "cmd-1", "")
	if err != nil {
		t.Fatalf("prepareRegistryAuth: %v", err)
	}
	if dir != "" {
		t.Fatalf("expected empty dir, got %q", dir)
	}
	cleanup() // must not panic
}

func TestPrepareRegistryAuthWritesConfigAndCleansUp(t *testing.T) {
	tmpDir := t.TempDir()
	oldStackDir := stackDir
	stackDir = tmpDir
	defer func() { stackDir = oldStackDir }()

	content := `{"auths":{"ghcr.io":{"auth":"ZGVwbG95Omh1bnRlcjI="}}}`
	authB64 := base64.StdEncoding.EncodeToString([]byte(content))

	dir, cleanup, err := prepareRegistryAuth("stack-1", "cmd-1", authB64)
	if err != nil {
		t.Fatalf("prepareRegistryAuth: %v", err)
	}
	if dir == "" {
		t.Fatal("expected non-empty dir")
	}

	written, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if string(written) != content {
		t.Fatalf("config.json content = %q, want %q", written, content)
	}

	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir %s to be removed after cleanup, stat err = %v", dir, err)
	}
}

func TestPrepareRegistryAuthInvalidBase64(t *testing.T) {
	tmpDir := t.TempDir()
	oldStackDir := stackDir
	stackDir = tmpDir
	defer func() { stackDir = oldStackDir }()

	if _, _, err := prepareRegistryAuth("stack-1", "cmd-1", "not-base64!!"); err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}
}

func TestValidateRegistryAuthEmptyIsNoop(t *testing.T) {
	if err := validateRegistryAuth("", nil); err != nil {
		t.Fatalf("expected no error for empty authB64, got %v", err)
	}
}

func TestValidateRegistryAuthSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "deploy" || pass != "hunter2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "http://")
	auth := base64.StdEncoding.EncodeToString([]byte("deploy:hunter2"))
	content := `{"auths":{"` + host + `":{"auth":"` + auth + `"}}}`
	authB64 := base64.StdEncoding.EncodeToString([]byte(content))

	if err := validateRegistryAuth(authB64, []string{host}); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
}

func TestValidateRegistryAuthRejectsBadCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "http://")
	auth := base64.StdEncoding.EncodeToString([]byte("deploy:wrong"))
	content := `{"auths":{"` + host + `":{"auth":"` + auth + `"}}}`
	authB64 := base64.StdEncoding.EncodeToString([]byte(content))

	err := validateRegistryAuth(authB64, []string{host})
	if err == nil {
		t.Fatal("expected error for rejected credential, got nil")
	}
}

func TestPreflightInsecureRegistriesEmptyHostsIsNoop(t *testing.T) {
	// Zero hosts must short-circuit before shelling out to `docker info`, so
	// this passes even on a machine without Docker installed/running.
	if err := preflightInsecureRegistries(nil); err != nil {
		t.Fatalf("expected no error for empty host list, got %v", err)
	}
}

func TestCheckInsecureRegistriesConfigured(t *testing.T) {
	configured := []byte(`{"IndexConfigs":{"registry.example.com:5000":{"Insecure":true},"ghcr.io":{"Insecure":false}}}`)

	if err := checkInsecureRegistriesConfigured(nil, configured); err != nil {
		t.Fatalf("expected no error for empty host list, got %v", err)
	}
	if err := checkInsecureRegistriesConfigured([]string{"registry.example.com:5000"}, configured); err != nil {
		t.Fatalf("expected host already configured as insecure to pass, got %v", err)
	}
	if err := checkInsecureRegistriesConfigured([]string{"ghcr.io"}, configured); err == nil {
		t.Fatal("expected error for host present but not marked insecure")
	}
	if err := checkInsecureRegistriesConfigured([]string{"unknown.example.com"}, configured); err == nil {
		t.Fatal("expected error for host missing from daemon's registry config")
	}
}
