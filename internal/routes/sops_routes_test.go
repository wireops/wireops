package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	sopscore "github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/cmd/sops/formats"
	sopsconfig "github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/keyservice"
	"github.com/getsops/sops/v3/version"

	_ "github.com/wireops/wireops/internal/integrations/sops"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/secrets"
)

func setupSopsTestApp(t *testing.T) (core.App, http.Handler) {
	t.Helper()
	t.Setenv("SECRET_KEY", testSecretBackendKey)

	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "sops-admin@example.com", "Password1!", rbac.RoleAdmin)

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
			Auth:  admin,
		}, nil
	})

	rr := routeRegistrar{r: r, app: app}
	rr.registerIntegrationRoutes(crypto.NormalizeSecretKey(testSecretBackendKey))
	rr.registerSopsRoutes()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux
}

// TestSopsIntegrationSeededLockedAndEnabled covers migration 53's seed row:
// SOPS shows up in the integrations list always enabled, and flagged locked
// so the frontend knows to grey out its toggle.
func TestSopsIntegrationSeededLockedAndEnabled(t *testing.T) {
	_, mux := setupSopsTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/custom/integrations", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out []struct {
		Slug    string `json:"slug"`
		Enabled bool   `json:"enabled"`
		Locked  bool   `json:"locked"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var found bool
	for _, item := range out {
		if item.Slug != "sops" {
			continue
		}
		found = true
		if !item.Enabled {
			t.Error("expected sops integration to be enabled")
		}
		if !item.Locked {
			t.Error("expected sops integration to be locked")
		}
	}
	if !found {
		t.Fatal("expected sops integration in list")
	}
}

func TestSopsIntegrationCannotBeToggled(t *testing.T) {
	_, mux := setupSopsTestApp(t)

	body := strings.NewReader(`{"enabled":false,"config":{}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/custom/integrations/sops", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSopsIntegrationCannotBeDeleted(t *testing.T) {
	_, mux := setupSopsTestApp(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/custom/integrations/sops", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// encryptSopsFixture builds a SOPS-encrypted YAML fixture for the given age
// recipient — sops-wrapper's own Encrypt doesn't support the age platform,
// so the fixture is built directly against getsops/sops/v3.
func encryptSopsFixture(t *testing.T, publicKey string, plaintext []byte) []byte {
	t.Helper()
	store := common.StoreForFormat(formats.Yaml, sopsconfig.NewStoresConfig())
	branches, err := store.LoadPlainFile(plaintext)
	if err != nil {
		t.Fatalf("LoadPlainFile: %v", err)
	}
	masterKey, err := sopsage.MasterKeyFromRecipient(publicKey)
	if err != nil {
		t.Fatalf("MasterKeyFromRecipient: %v", err)
	}
	tree := sopscore.Tree{
		Branches: branches,
		Metadata: sopscore.Metadata{
			KeyGroups: []sopscore.KeyGroup{{masterKey}},
			Version:   version.Version,
		},
	}
	dataKey, errs := tree.GenerateDataKeyWithKeyServices([]keyservice.KeyServiceClient{keyservice.NewLocalClient()})
	if len(errs) > 0 {
		t.Fatalf("GenerateDataKeyWithKeyServices: %v", errs)
	}
	if err := common.EncryptTree(common.EncryptTreeOpts{DataKey: dataKey, Tree: &tree, Cipher: aes.NewCipher()}); err != nil {
		t.Fatalf("EncryptTree: %v", err)
	}
	encBytes, err := store.EmitEncryptedFile(tree)
	if err != nil {
		t.Fatalf("EmitEncryptedFile: %v", err)
	}
	return encBytes
}

func createSopsTestRepo(t *testing.T, app core.App, ageKeyEncrypted, ageKeyPublic string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		t.Fatalf("find repositories collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "sops-test-repo")
	rec.Set("git_url", "https://example.com/repo.git")
	rec.Set("sops_age_key", ageKeyEncrypted)
	rec.Set("sops_age_public_key", ageKeyPublic)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save repo: %v", err)
	}
	return rec
}

func createSopsTestStack(t *testing.T, app core.App, repoID string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatalf("find stacks collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "sops-test-stack")
	rec.Set("repository", repoID)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save stack: %v", err)
	}
	return rec
}

func TestSopsEnvVarsRouteNoSecretsFile(t *testing.T) {
	app, mux := setupSopsTestApp(t)
	t.Setenv("REPOS_WORKSPACE", t.TempDir())

	repo := createSopsTestRepo(t, app, "", "")
	stack := createSopsTestStack(t, app, repo.Id)

	req := httptest.NewRequest(http.MethodGet, "/api/custom/stacks/"+stack.Id+"/sops-env-vars", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out struct {
		Keys      []string `json:"keys"`
		Available bool     `json:"available"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Available || len(out.Keys) != 0 {
		t.Fatalf("expected no secrets file to report unavailable/empty, got %+v", out)
	}
}

// TestSopsEnvVarsRouteNeverLeaksValues is the core security assertion for
// this endpoint: it must return SOPS key names for the frontend to render
// disabled rows with, but the actual decrypted secret value must never
// appear anywhere in the response body.
func TestSopsEnvVarsRouteNeverLeaksValues(t *testing.T) {
	app, mux := setupSopsTestApp(t)
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)

	privateKey, publicKey, err := secrets.GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	encryptedKey, err := crypto.Encrypt([]byte(privateKey), []byte(testSecretBackendKey))
	if err != nil {
		t.Fatalf("crypto.Encrypt: %v", err)
	}

	repo := createSopsTestRepo(t, app, encryptedKey, publicKey)
	stack := createSopsTestStack(t, app, repo.Id)

	repoDir := filepath.Join(workspace, repo.Id)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo dir: %v", err)
	}
	const secretValue = "s3cr3t-value-must-not-leak"
	encrypted := encryptSopsFixture(t, publicKey, []byte("DB_PASS: "+secretValue+"\n"))
	if err := os.WriteFile(filepath.Join(repoDir, "secrets.yaml"), encrypted, 0o644); err != nil {
		t.Fatalf("write secrets.yaml: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/custom/stacks/"+stack.Id+"/sops-env-vars", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if strings.Contains(rec.Body.String(), secretValue) {
		t.Fatalf("response leaked secret value: %s", rec.Body.String())
	}

	var out struct {
		Keys      []string `json:"keys"`
		Available bool     `json:"available"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !out.Available || len(out.Keys) != 1 || out.Keys[0] != "DB_PASS" {
		t.Fatalf("expected available with key DB_PASS, got %+v", out)
	}
}

func TestSopsEncryptRouteRoundTrip(t *testing.T) {
	app, mux := setupSopsTestApp(t)

	privateKey, publicKey, err := secrets.GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	encryptedKey, err := crypto.Encrypt([]byte(privateKey), []byte(testSecretBackendKey))
	if err != nil {
		t.Fatalf("crypto.Encrypt: %v", err)
	}
	repo := createSopsTestRepo(t, app, encryptedKey, publicKey)

	body := strings.NewReader(`{"values":{"DB_PASS":"hunter2"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/repositories/"+repo.Id+"/sops-encrypt", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out struct {
		Content  string `json:"content"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Filename != "secrets.yaml" {
		t.Fatalf("expected filename secrets.yaml, got %q", out.Filename)
	}
	if strings.Contains(out.Content, "hunter2") {
		t.Fatal("response content is not encrypted — plaintext leaked")
	}

	values, err := secrets.DecryptSecretsFile(t.Context(), []byte(out.Content), privateKey)
	if err != nil {
		t.Fatalf("DecryptSecretsFile on route output: %v", err)
	}
	if values["DB_PASS"] != "hunter2" {
		t.Fatalf("round-trip mismatch: %#v", values)
	}
}

func TestSopsEncryptRouteNoPublicKey(t *testing.T) {
	app, mux := setupSopsTestApp(t)
	repo := createSopsTestRepo(t, app, "", "")

	body := strings.NewReader(`{"values":{"DB_PASS":"hunter2"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/repositories/"+repo.Id+"/sops-encrypt", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSopsEncryptRouteInvalidKeyName(t *testing.T) {
	app, mux := setupSopsTestApp(t)
	_, publicKey, err := secrets.GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	repo := createSopsTestRepo(t, app, "", publicKey)

	body := strings.NewReader(`{"values":{"not a valid key":"x"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/repositories/"+repo.Id+"/sops-encrypt", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSopsEncryptRouteInvalidBody(t *testing.T) {
	app, mux := setupSopsTestApp(t)
	_, publicKey, err := secrets.GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	repo := createSopsTestRepo(t, app, "", publicKey)

	req := httptest.NewRequest(http.MethodPost, "/api/custom/repositories/"+repo.Id+"/sops-encrypt", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSopsEncryptRouteRepositoryNotFound(t *testing.T) {
	_, mux := setupSopsTestApp(t)

	body := strings.NewReader(`{"values":{"DB_PASS":"hunter2"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/custom/repositories/does-not-exist/sops-encrypt", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
