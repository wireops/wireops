package routes

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/secrets"
	"github.com/wireops/wireops/internal/testutil"
)

func envVarRoutesMux(t *testing.T, app core.App, auth *core.Record) http.Handler {
	t.Helper()

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
			Auth:  auth,
		}, nil
	})

	rr := routeRegistrar{r: r, app: app}
	rr.registerEnvVarRoutes()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return mux
}

func envVarTestApp(t *testing.T) (core.App, *core.Record) {
	t.Helper()
	t.Setenv("SECRET_KEY", testSecretBackendKey)
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	operator := createTestUser(t, app, "envvar-operator@example.com", "Password1!", rbac.RoleOperator)
	return app, operator
}

func createEnvVarTestStack(t *testing.T, app core.App, name, repoID string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatalf("find stacks collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("repository", repoID)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save stack: %v", err)
	}
	return rec
}

func createEnvVarTestRepo(t *testing.T, app core.App, name string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		t.Fatalf("find repositories collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("git_url", "https://example.com/"+name+".git")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save repo: %v", err)
	}
	return rec
}

// createEnvVarRow saves a stack_env_vars row directly (bypassing the
// route/HTTP layer, and bypassing the encryption hook — internal/hooks isn't
// registered in this test app, matching every other internal/routes test).
// For secret=true+internal rows callers should pass an already-encrypted
// value (crypto.Encrypt) so the route's own decrypt calls have real
// ciphertext to work with, exactly as they would in production once the hook
// has run.
func createEnvVarRow(t *testing.T, app core.App, stackID, key, value string, secret bool, provider string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("stack_env_vars")
	if err != nil {
		t.Fatalf("find stack_env_vars collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("stack", stackID)
	rec.Set("key", key)
	rec.Set("value", value)
	rec.Set("secret", secret)
	rec.Set("secret_provider", provider)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save stack_env_var %s: %v", key, err)
	}
	return rec
}

func TestBulkUpsertEnvVars_ReplaceMode(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createEnvVarTestRepo(t, app, "bulk-replace-repo")
	stack := createEnvVarTestStack(t, app, "bulk-replace-stack", repo.Id)
	createEnvVarRow(t, app, stack.Id, "KEEP", "keep-value", false, "")
	createEnvVarRow(t, app, stack.Id, "DROP", "drop-value", false, "")
	mux := envVarRoutesMux(t, app, operator)

	body := map[string]any{
		"mode": "replace",
		"vars": []map[string]any{
			{"key": "KEEP", "value": "keep-value-changed", "secret": false, "secret_provider": ""},
			{"key": "NEW", "value": "new-value", "secret": false, "secret_provider": ""},
		},
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stack.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct{ Created, Updated, Deleted int }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Created != 1 || out.Updated != 1 || out.Deleted != 1 {
		t.Fatalf("expected created=1 updated=1 deleted=1, got %+v", out)
	}

	rows, err := app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		t.Fatalf("find rows: %v", err)
	}
	byKey := map[string]*core.Record{}
	for _, r := range rows {
		byKey[r.GetString("key")] = r
	}
	if len(byKey) != 2 {
		t.Fatalf("expected 2 rows after replace, got %d: %+v", len(byKey), byKey)
	}
	if byKey["DROP"] != nil {
		t.Fatal("expected DROP row to be deleted")
	}
	if byKey["KEEP"].GetString("value") != "keep-value-changed" {
		t.Fatalf("expected KEEP value updated, got %q", byKey["KEEP"].GetString("value"))
	}
	if byKey["NEW"] == nil {
		t.Fatal("expected NEW row to be created")
	}
}

func TestBulkUpsertEnvVars_PreservesUnchangedSecret(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createEnvVarTestRepo(t, app, "bulk-secret-repo")
	stack := createEnvVarTestStack(t, app, "bulk-secret-stack", repo.Id)

	secretKey := crypto.NormalizeSecretKey(testSecretBackendKey)
	ciphertext, err := crypto.Encrypt([]byte("s3cr3t"), secretKey)
	if err != nil {
		t.Fatalf("crypto.Encrypt: %v", err)
	}
	createEnvVarRow(t, app, stack.Id, "TOKEN", ciphertext, true, "internal")
	mux := envVarRoutesMux(t, app, operator)

	body := map[string]any{
		"mode": "replace",
		"vars": []map[string]any{
			{"key": "TOKEN", "value": "", "secret": true, "secret_provider": "internal"},
		},
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stack.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rows, err := app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		t.Fatalf("find rows: %v", err)
	}
	if len(rows) != 1 || rows[0].GetString("value") != ciphertext {
		t.Fatalf("expected ciphertext unchanged, got rows=%+v", rows)
	}
}

func TestBulkUpsertEnvVars_RejectsDuplicateKeysInPayload(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createEnvVarTestRepo(t, app, "bulk-dup-repo")
	stack := createEnvVarTestStack(t, app, "bulk-dup-stack", repo.Id)
	mux := envVarRoutesMux(t, app, operator)

	body := map[string]any{
		"mode": "replace",
		"vars": []map[string]any{
			{"key": "FOO", "value": "1"},
			{"key": "FOO", "value": "2"},
		},
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stack.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBulkUpsertEnvVars_RejectsInvalidKey(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createEnvVarTestRepo(t, app, "bulk-invalid-key-repo")
	stack := createEnvVarTestStack(t, app, "bulk-invalid-key-stack", repo.Id)
	mux := envVarRoutesMux(t, app, operator)

	body := map[string]any{
		"mode": "replace",
		"vars": []map[string]any{
			{"key": "1-not-an-identifier", "value": "1"},
		},
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stack.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBulkUpsertEnvVars_DowngradeToNonSecretClearsValue(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createEnvVarTestRepo(t, app, "bulk-downgrade-repo")
	stack := createEnvVarTestStack(t, app, "bulk-downgrade-stack", repo.Id)

	secretKey := crypto.NormalizeSecretKey(testSecretBackendKey)
	ciphertext, err := crypto.Encrypt([]byte("s3cr3t"), secretKey)
	if err != nil {
		t.Fatalf("crypto.Encrypt: %v", err)
	}
	createEnvVarRow(t, app, stack.Id, "TOKEN", ciphertext, true, "internal")
	mux := envVarRoutesMux(t, app, operator)

	// Downgrading an internal secret to non-secret with a blank value must
	// clear the stored ciphertext, not preserve it under secret=false where
	// the UI would render it as plaintext.
	body := map[string]any{
		"mode": "replace",
		"vars": []map[string]any{
			{"key": "TOKEN", "value": "", "secret": false, "secret_provider": ""},
		},
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stack.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rows, err := app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		t.Fatalf("find rows: %v", err)
	}
	if len(rows) != 1 || rows[0].GetBool("secret") || rows[0].GetString("value") != "" {
		t.Fatalf("expected TOKEN downgraded and cleared, got rows=%+v", rows)
	}
}

func TestBulkUpsertEnvVars_RejectsEmptyKey(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createEnvVarTestRepo(t, app, "bulk-empty-repo")
	stack := createEnvVarTestStack(t, app, "bulk-empty-stack", repo.Id)
	mux := envVarRoutesMux(t, app, operator)

	body := map[string]any{
		"mode": "replace",
		"vars": []map[string]any{
			{"key": "  ", "value": "1"},
		},
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stack.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBulkUpsertEnvVars_StackNotFound(t *testing.T) {
	app, operator := envVarTestApp(t)
	mux := envVarRoutesMux(t, app, operator)

	body := map[string]any{"mode": "replace", "vars": []map[string]any{}}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/does-not-exist/env-vars/bulk", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBulkUpsertEnvVars_RequiresOperatorCapability(t *testing.T) {
	app, _ := envVarTestApp(t)
	viewer := createTestUser(t, app, "envvar-viewer@example.com", "Password1!", rbac.RoleViewer)
	repo := createEnvVarTestRepo(t, app, "bulk-viewer-repo")
	stack := createEnvVarTestStack(t, app, "bulk-viewer-stack", repo.Id)
	mux := envVarRoutesMux(t, app, viewer)

	body := map[string]any{"mode": "replace", "vars": []map[string]any{}}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stack.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer role, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCopyEnvVars_DecryptsAndReencrypts(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createEnvVarTestRepo(t, app, "copy-secret-repo")
	source := createEnvVarTestStack(t, app, "copy-secret-source", repo.Id)
	target := createEnvVarTestStack(t, app, "copy-secret-target", repo.Id)

	secretKey := crypto.NormalizeSecretKey(testSecretBackendKey)
	ciphertext, err := crypto.Encrypt([]byte("s3cr3t-copy-me"), secretKey)
	if err != nil {
		t.Fatalf("crypto.Encrypt: %v", err)
	}
	createEnvVarRow(t, app, source.Id, "TOKEN", ciphertext, true, "internal")
	mux := envVarRoutesMux(t, app, operator)

	body := map[string]any{"source_stack": source.Id, "keys": []string{"TOKEN"}, "overwrite": true}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+target.Id+"/env-vars/copy-from", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if strings.Contains(rec.Body.String(), "s3cr3t-copy-me") {
		t.Fatalf("response leaked secret plaintext: %s", rec.Body.String())
	}

	targetRows, err := app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": target.Id})
	if err != nil {
		t.Fatalf("find target rows: %v", err)
	}
	if len(targetRows) != 1 {
		t.Fatalf("expected 1 target row, got %d", len(targetRows))
	}
	targetCiphertext := targetRows[0].GetString("value")
	if targetCiphertext == ciphertext {
		t.Fatal("expected target ciphertext to differ from source (re-encrypted, not copied blob)")
	}
	plaintext, err := crypto.Decrypt(targetCiphertext, secretKey)
	if err != nil {
		t.Fatalf("decrypt target ciphertext: %v", err)
	}
	if string(plaintext) != "s3cr3t-copy-me" {
		t.Fatalf("expected decrypted plaintext to match source, got %q", plaintext)
	}
}

func TestCopyEnvVars_SkipsWhenOverwriteFalse(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createEnvVarTestRepo(t, app, "copy-overwrite-repo")
	source := createEnvVarTestStack(t, app, "copy-overwrite-source", repo.Id)
	target := createEnvVarTestStack(t, app, "copy-overwrite-target", repo.Id)
	createEnvVarRow(t, app, source.Id, "FOO", "from-source", false, "")
	createEnvVarRow(t, app, target.Id, "FOO", "already-here", false, "")
	mux := envVarRoutesMux(t, app, operator)

	body := map[string]any{"source_stack": source.Id, "keys": []string{"FOO"}, "overwrite": false}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+target.Id+"/env-vars/copy-from", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Copied  int
		Skipped []string
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Copied != 0 || len(out.Skipped) != 1 || out.Skipped[0] != "FOO" {
		t.Fatalf("expected copied=0 skipped=[FOO], got %+v", out)
	}

	targetRows, err := app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": target.Id})
	if err != nil {
		t.Fatalf("find target rows: %v", err)
	}
	if len(targetRows) != 1 || targetRows[0].GetString("value") != "already-here" {
		t.Fatalf("expected target value untouched, got %+v", targetRows)
	}
}

func TestCopyEnvVars_SourceOrTargetNotFound_404(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createEnvVarTestRepo(t, app, "copy-404-repo")
	stack := createEnvVarTestStack(t, app, "copy-404-stack", repo.Id)
	mux := envVarRoutesMux(t, app, operator)

	body := map[string]any{"source_stack": "does-not-exist", "keys": []string{"FOO"}, "overwrite": true}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stack.Id+"/env-vars/copy-from", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing source, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/does-not-exist/env-vars/copy-from", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing target, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCopyEnvVars_BlocksCrossRepositoryWhenSourceHasSops(t *testing.T) {
	app, operator := envVarTestApp(t)
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)

	sourceRepo := createEnvVarTestRepo(t, app, "copy-sops-source-repo")
	targetRepo := createEnvVarTestRepo(t, app, "copy-sops-target-repo")
	source := createEnvVarTestStack(t, app, "copy-sops-source-stack", sourceRepo.Id)
	target := createEnvVarTestStack(t, app, "copy-sops-target-stack", targetRepo.Id)
	createEnvVarRow(t, app, source.Id, "FOO", "plain-value", false, "")

	repoDir := filepath.Join(workspace, sourceRepo.Id)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo dir: %v", err)
	}
	_, publicKey, err := secrets.GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("generate age keypair: %v", err)
	}
	encrypted := testutil.EncryptForAge(t, publicKey, []byte("DB_PASS: hunter2\n"))
	if err := os.WriteFile(filepath.Join(repoDir, "secrets.yaml"), encrypted, 0o644); err != nil {
		t.Fatalf("write secrets.yaml: %v", err)
	}

	mux := envVarRoutesMux(t, app, operator)
	body := map[string]any{"source_stack": source.Id, "keys": []string{"FOO"}, "overwrite": true}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+target.Id+"/env-vars/copy-from", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	targetRows, err := app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": target.Id})
	if err != nil {
		t.Fatalf("find target rows: %v", err)
	}
	if len(targetRows) != 0 {
		t.Fatalf("expected no rows created on blocked copy, got %+v", targetRows)
	}
}

func TestCopyEnvVars_AllowsCrossRepositoryWithoutSops(t *testing.T) {
	app, operator := envVarTestApp(t)
	t.Setenv("REPOS_WORKSPACE", t.TempDir())

	sourceRepo := createEnvVarTestRepo(t, app, "copy-nosops-source-repo")
	targetRepo := createEnvVarTestRepo(t, app, "copy-nosops-target-repo")
	source := createEnvVarTestStack(t, app, "copy-nosops-source-stack", sourceRepo.Id)
	target := createEnvVarTestStack(t, app, "copy-nosops-target-stack", targetRepo.Id)
	createEnvVarRow(t, app, source.Id, "FOO", "plain-value", false, "")

	mux := envVarRoutesMux(t, app, operator)
	body := map[string]any{"source_stack": source.Id, "keys": []string{"FOO"}, "overwrite": true}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+target.Id+"/env-vars/copy-from", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when source has no SOPS file, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCopyEnvVars_AllowsSameRepositorySops(t *testing.T) {
	app, operator := envVarTestApp(t)
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)

	repo := createEnvVarTestRepo(t, app, "copy-samesops-repo")
	source := createEnvVarTestStack(t, app, "copy-samesops-source-stack", repo.Id)
	target := createEnvVarTestStack(t, app, "copy-samesops-target-stack", repo.Id)
	createEnvVarRow(t, app, source.Id, "FOO", "plain-value", false, "")

	repoDir := filepath.Join(workspace, repo.Id)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo dir: %v", err)
	}
	_, publicKey, err := secrets.GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("generate age keypair: %v", err)
	}
	encrypted := testutil.EncryptForAge(t, publicKey, []byte("DB_PASS: hunter2\n"))
	if err := os.WriteFile(filepath.Join(repoDir, "secrets.yaml"), encrypted, 0o644); err != nil {
		t.Fatalf("write secrets.yaml: %v", err)
	}

	mux := envVarRoutesMux(t, app, operator)
	body := map[string]any{"source_stack": source.Id, "keys": []string{"FOO"}, "overwrite": true}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+target.Id+"/env-vars/copy-from", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for same-repository copy despite SOPS file, got %d: %s", rec.Code, rec.Body.String())
	}
}

