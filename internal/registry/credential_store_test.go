package registry

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/constants"
	"github.com/wireops/wireops/internal/crypto"
)

func newRegistryCredentialTestApp(t *testing.T) (*tests.TestApp, *core.Collection) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	col := core.NewBaseCollection("registry_credentials")
	col.Fields.Add(&core.TextField{Name: "name"})
	col.Fields.Add(&core.TextField{Name: "registry_url"})
	col.Fields.Add(&core.TextField{Name: "registry_host"})
	col.Fields.Add(&core.TextField{Name: "auth_type"})
	col.Fields.Add(&core.TextField{Name: "username"})
	col.Fields.Add(&core.TextField{Name: "password"})
	col.Fields.Add(&core.BoolField{Name: "insecure"})
	if err := app.Save(col); err != nil {
		t.Fatalf("save registry_credentials collection: %v", err)
	}
	return app, col
}

func TestLoadCredentialByIDDecryptsPassword(t *testing.T) {
	app, col := newRegistryCredentialTestApp(t)
	secret := "0123456789abcdef0123456789abcdef"
	t.Setenv(constants.EnvSecretKey, secret)

	encrypted, err := crypto.Encrypt([]byte("s3cr3t-token"), []byte(secret))
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}

	rec := core.NewRecord(col)
	rec.Set("name", "GHCR")
	rec.Set("registry_url", "https://ghcr.io")
	rec.Set("auth_type", "token")
	rec.Set("username", "deploy-bot")
	rec.Set("password", encrypted)
	rec.Set("insecure", false)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	cred, err := LoadCredentialByID(app, rec.Id)
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}
	if cred.Username != "deploy-bot" || cred.Password != "s3cr3t-token" {
		t.Fatalf("unexpected credential: %#v", cred)
	}
	if cred.RegistryURL != "https://ghcr.io" {
		t.Fatalf("unexpected registry url: %q", cred.RegistryURL)
	}
}

// TestLoadCredentialByIDDecryptsMultilineJSONKey exercises the GCP service
// account use case: a multi-KB multiline JSON blob stored as the opaque
// "password" field, round-tripping through encryption exactly like a short
// token does — see internal/hooks/pb_hooks.go encryptField, which treats
// this field as opaque regardless of content.
func TestLoadCredentialByIDDecryptsMultilineJSONKey(t *testing.T) {
	app, col := newRegistryCredentialTestApp(t)
	secret := "0123456789abcdef0123456789abcdef"
	t.Setenv(constants.EnvSecretKey, secret)

	jsonKey := `{
  "type": "service_account",
  "project_id": "example-project",
  "private_key": "-----BEGIN PRIVATE KEY-----\nMIIExamp...\n-----END PRIVATE KEY-----\n",
  "client_email": "deploy@example-project.iam.gserviceaccount.com"
}`
	encrypted, err := crypto.Encrypt([]byte(jsonKey), []byte(secret))
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}

	rec := core.NewRecord(col)
	rec.Set("name", "GAR")
	rec.Set("registry_url", "https://us-docker.pkg.dev")
	rec.Set("auth_type", "gcp_service_account")
	rec.Set("username", "_json_key")
	rec.Set("password", encrypted)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	cred, err := LoadCredentialByID(app, rec.Id)
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}
	if cred.Password != jsonKey {
		t.Fatalf("decrypted password does not match original JSON key:\ngot:  %q\nwant: %q", cred.Password, jsonKey)
	}
	if !json.Valid([]byte(cred.Password)) {
		t.Fatalf("decrypted password is not valid JSON")
	}
}

func TestNormalizeRegistryHost(t *testing.T) {
	cases := map[string]string{
		"https://ghcr.io/":                      "ghcr.io",
		"https://registry.example.com:5000":     "registry.example.com:5000",
		"HTTPS://REGISTRY.EXAMPLE.COM:5000/v2/": "registry.example.com:5000",
		"docker.io":                             "docker.io",
		"registry.example.com/":                 "registry.example.com",
		"registry.example.com/v2":               "registry.example.com",
	}
	for input, want := range cases {
		if got := NormalizeRegistryHost(input); got != want {
			t.Errorf("NormalizeRegistryHost(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRegistryHostsFromComposeYAML(t *testing.T) {
	content := []byte(`
services:
  api:
    image: registry.example.com:5000/team/api:v1
  worker:
    image: ghcr.io/example/worker@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  frontend:
    image: nginx:1.27
  helper:
    build: .
`)

	hosts, err := RegistryHostsFromComposeYAML(content)
	if err != nil {
		t.Fatalf("RegistryHostsFromComposeYAML: %v", err)
	}
	want := []string{"docker.io", "ghcr.io", "registry.example.com:5000"}
	if !slices.Equal(hosts, want) {
		t.Fatalf("hosts = %v, want %v", hosts, want)
	}
}

func TestRegistryHostsFromComposeYAMLRejectsInvalidImage(t *testing.T) {
	_, err := RegistryHostsFromComposeYAML([]byte("services:\n  api:\n    image: not valid\n"))
	if err == nil || !strings.Contains(err.Error(), `service "api"`) {
		t.Fatalf("expected service-specific image parsing error, got %v", err)
	}
}

func TestBuildDockerAuth(t *testing.T) {
	app, col := newRegistryCredentialTestApp(t)
	secret := "0123456789abcdef0123456789abcdef"
	t.Setenv(constants.EnvSecretKey, secret)

	encrypted, err := crypto.Encrypt([]byte("hunter2"), []byte(secret))
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}

	rec := core.NewRecord(col)
	rec.Set("name", "Private")
	rec.Set("registry_url", "https://registry.example.com:5000")
	rec.Set("auth_type", "basic")
	rec.Set("username", "deploy")
	rec.Set("password", encrypted)
	rec.Set("insecure", true)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	authB64, insecureHosts, err := BuildDockerAuth(app, rec.Id)
	if err != nil {
		t.Fatalf("build docker auth: %v", err)
	}
	if len(insecureHosts) != 1 || insecureHosts[0] != "registry.example.com:5000" {
		t.Fatalf("unexpected insecure hosts: %v", insecureHosts)
	}

	decoded, err := base64.StdEncoding.DecodeString(authB64)
	if err != nil {
		t.Fatalf("decode config json: %v", err)
	}
	var cfg dockerConfig
	if err := json.Unmarshal(decoded, &cfg); err != nil {
		t.Fatalf("unmarshal config json: %v", err)
	}
	entry, ok := cfg.Auths["registry.example.com:5000"]
	if !ok {
		t.Fatalf("config json missing auths entry: %s", decoded)
	}
	rawAuth, err := base64.StdEncoding.DecodeString(entry.Auth)
	if err != nil {
		t.Fatalf("decode auth entry: %v", err)
	}
	if !strings.HasPrefix(string(rawAuth), "deploy:hunter2") {
		t.Fatalf("unexpected auth entry: %q", rawAuth)
	}
}

func TestBuildDockerAuthEmptyCredentialID(t *testing.T) {
	app, _ := newRegistryCredentialTestApp(t)
	authB64, insecureHosts, err := BuildDockerAuth(app, "")
	if err != nil {
		t.Fatalf("expected no error for empty credential id, got %v", err)
	}
	if authB64 != "" || insecureHosts != nil {
		t.Fatalf("expected empty result, got authB64=%q insecureHosts=%v", authB64, insecureHosts)
	}
}

func TestBuildDockerAuthForHostsAllowsMissingOptionalCollection(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	authB64, insecureHosts, err := BuildDockerAuthForHosts(app, []string{"docker.io"})
	if err != nil {
		t.Fatalf("BuildDockerAuthForHosts without registry collection: %v", err)
	}
	if authB64 != "" || insecureHosts != nil {
		t.Fatalf("expected anonymous result, got authB64=%q insecureHosts=%v", authB64, insecureHosts)
	}
}

func TestBuildDockerAuthForHostsMatchesOnlyExactRegistries(t *testing.T) {
	app, col := newRegistryCredentialTestApp(t)
	secret := "0123456789abcdef0123456789abcdef"
	t.Setenv(constants.EnvSecretKey, secret)

	add := func(name, url, password string, insecure bool) {
		t.Helper()
		encrypted, err := crypto.Encrypt([]byte(password), []byte(secret))
		if err != nil {
			t.Fatalf("encrypt password: %v", err)
		}
		record := core.NewRecord(col)
		record.Set("name", name)
		record.Set("registry_url", url)
		record.Set("registry_host", NormalizeRegistryHost(url))
		record.Set("auth_type", "basic")
		record.Set("username", name)
		record.Set("password", encrypted)
		record.Set("insecure", insecure)
		if err := app.Save(record); err != nil {
			t.Fatalf("save credential: %v", err)
		}
	}

	add("primary", "https://registry.example.com:5000", "one", true)
	add("other-port", "registry.example.com:5001", "two", false)
	add("unrelated", "ghcr.io", "three", false)

	authB64, insecureHosts, err := BuildDockerAuthForHosts(app, []string{"registry.example.com:5000", "docker.io"})
	if err != nil {
		t.Fatalf("BuildDockerAuthForHosts: %v", err)
	}
	if !slices.Equal(insecureHosts, []string{"registry.example.com:5000"}) {
		t.Fatalf("insecure hosts = %v", insecureHosts)
	}

	decoded, err := base64.StdEncoding.DecodeString(authB64)
	if err != nil {
		t.Fatalf("decode config json: %v", err)
	}
	var cfg dockerConfig
	if err := json.Unmarshal(decoded, &cfg); err != nil {
		t.Fatalf("unmarshal config json: %v", err)
	}
	if len(cfg.Auths) != 1 || cfg.Auths["registry.example.com:5000"].Auth == "" {
		t.Fatalf("unexpected auths: %#v", cfg.Auths)
	}
}
