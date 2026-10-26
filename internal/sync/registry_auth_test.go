package sync

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/constants"
	"github.com/wireops/wireops/internal/crypto"
)

func newResolveRegistryAuthTestApp(t *testing.T) (*tests.TestApp, *core.Collection) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	credentials := core.NewBaseCollection("registry_credentials")
	credentials.Fields.Add(&core.TextField{Name: "name"})
	credentials.Fields.Add(&core.TextField{Name: "registry_url"})
	credentials.Fields.Add(&core.TextField{Name: "registry_host"})
	credentials.Fields.Add(&core.TextField{Name: "auth_type"})
	credentials.Fields.Add(&core.TextField{Name: "username"})
	credentials.Fields.Add(&core.TextField{Name: "password"})
	credentials.Fields.Add(&core.BoolField{Name: "insecure"})
	if err := app.Save(credentials); err != nil {
		t.Fatalf("save registry_credentials collection: %v", err)
	}
	return app, credentials
}

func TestShouldRetryDeployHonorsAttemptLimit(t *testing.T) {
	if !shouldRetryDeploy(1, 3) {
		t.Fatal("ordinary first-attempt failures should be retried")
	}
	if shouldRetryDeploy(3, 3) {
		t.Fatal("the final attempt must not be retried")
	}
}

func renderedCompose(images ...string) []byte {
	content := "services:\n"
	for i, image := range images {
		content += "  service-" + string(rune('a'+i)) + ":\n    image: " + image + "\n"
	}
	return []byte(content)
}

func TestResolveRegistryAuthWithoutMatchingCredential(t *testing.T) {
	app, _ := newResolveRegistryAuthTestApp(t)
	r := &Reconciler{app: app}

	authB64, insecureHosts, err := r.resolveRegistryAuth(renderedCompose("registry.example.com/team/api:v1"), nil)
	if err != nil {
		t.Fatalf("resolveRegistryAuth: %v", err)
	}
	if authB64 != "" || insecureHosts != nil {
		t.Fatalf("expected no auth for a registry without a credential, got authB64=%q insecureHosts=%v", authB64, insecureHosts)
	}
}

func TestResolveRegistryAuthUsesRenderedImageHosts(t *testing.T) {
	app, credentials := newResolveRegistryAuthTestApp(t)
	r := &Reconciler{app: app}
	secret := "0123456789abcdef0123456789abcdef"
	t.Setenv(constants.EnvSecretKey, secret)

	addCredential := func(name, url string, insecure bool) {
		t.Helper()
		encrypted, err := crypto.Encrypt([]byte("hunter2"), []byte(secret))
		if err != nil {
			t.Fatalf("encrypt password: %v", err)
		}
		credential := core.NewRecord(credentials)
		credential.Set("name", name)
		credential.Set("registry_url", url)
		credential.Set("registry_host", url)
		credential.Set("auth_type", "basic")
		credential.Set("username", name)
		credential.Set("password", encrypted)
		credential.Set("insecure", insecure)
		if err := app.Save(credential); err != nil {
			t.Fatalf("save credential: %v", err)
		}
	}

	addCredential("private", "registry.example.com:5000", true)
	addCredential("wrong-port", "registry.example.com:5001", false)
	addCredential("ghcr", "ghcr.io", false)

	// This models a final rendered compose after an image override changed
	// service-b from Docker Hub to a private registry.
	authB64, insecureHosts, err := r.resolveRegistryAuth(renderedCompose(
		"registry.example.com:5000/team/api:v1",
		"ghcr.io/example/worker@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	), nil)
	if err != nil {
		t.Fatalf("resolveRegistryAuth: %v", err)
	}
	if len(insecureHosts) != 1 || insecureHosts[0] != "registry.example.com:5000" {
		t.Fatalf("unexpected insecure hosts: %v", insecureHosts)
	}

	decoded, err := base64.StdEncoding.DecodeString(authB64)
	if err != nil {
		t.Fatalf("decode auth config: %v", err)
	}
	var config struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(decoded, &config); err != nil {
		t.Fatalf("decode auth config JSON: %v", err)
	}
	if len(config.Auths) != 2 || config.Auths["registry.example.com:5000"].Auth == "" || config.Auths["ghcr.io"].Auth == "" {
		t.Fatalf("auth config = %#v, want exact credentials for rendered registries", config.Auths)
	}
	if _, found := config.Auths["registry.example.com:5001"]; found {
		t.Fatalf("auth config unexpectedly includes the different registry port: %#v", config.Auths)
	}
}

func TestResolveRegistryAuthInterpolatesWorkerEnvBeforeMatching(t *testing.T) {
	app, credentials := newResolveRegistryAuthTestApp(t)
	r := &Reconciler{app: app}
	secret := "0123456789abcdef0123456789abcdef"
	t.Setenv(constants.EnvSecretKey, secret)

	encrypted, err := crypto.Encrypt([]byte("hunter2"), []byte(secret))
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	credential := core.NewRecord(credentials)
	credential.Set("name", "Grom")
	credential.Set("registry_url", "https://registry.minilab.cloud")
	credential.Set("registry_host", "registry.minilab.cloud")
	credential.Set("auth_type", "basic")
	credential.Set("username", "wireops")
	credential.Set("password", encrypted)
	if err := app.Save(credential); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	authB64, _, err := r.resolveRegistryAuth([]byte("services:\n  indexer:\n    image: ${REGISTRY}/docker/ai/indexer:${TAG:-latest}\n"), []string{
		"REGISTRY=registry.minilab.cloud",
		"TAG=v0.1.1",
	})
	if err != nil {
		t.Fatalf("resolveRegistryAuth: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(authB64)
	if err != nil {
		t.Fatalf("decode auth config: %v", err)
	}
	var cfg struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(decoded, &cfg); err != nil {
		t.Fatalf("decode auth config JSON: %v", err)
	}
	if len(cfg.Auths) != 1 || cfg.Auths["registry.minilab.cloud"].Auth == "" {
		t.Fatalf("auth config = %#v, want Grom registry credential", cfg.Auths)
	}
}

func TestResolveRegistryAuthFailsForUnreadableMatchingCredential(t *testing.T) {
	app, credentials := newResolveRegistryAuthTestApp(t)
	r := &Reconciler{app: app}
	credential := core.NewRecord(credentials)
	credential.Set("name", "broken")
	credential.Set("registry_url", "registry.example.com")
	credential.Set("registry_host", "registry.example.com")
	credential.Set("auth_type", "basic")
	credential.Set("username", "deploy")
	credential.Set("password", "not-a-valid-ciphertext")
	if err := app.Save(credential); err != nil {
		t.Fatalf("save credential: %v", err)
	}
	t.Setenv(constants.EnvSecretKey, "0123456789abcdef0123456789abcdef")

	_, _, err := r.resolveRegistryAuth(renderedCompose("registry.example.com/team/api:v1"), nil)
	if err == nil {
		t.Fatal("expected matching unreadable credential to fail deployment preparation")
	}
}
