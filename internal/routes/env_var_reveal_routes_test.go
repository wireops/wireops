package routes

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/rbac"
)

// auditMetadata reads a record's metadata_json regardless of whether the test
// app stores it as a decoded map or a raw JSON string, mirroring the helper
// pattern used in internal/routes/setup_test.go.
func auditMetadata(t *testing.T, rec *core.Record) map[string]any {
	t.Helper()
	meta := audit.MetadataJSON(rec.Get("metadata_json"))
	if len(meta) == 0 {
		meta = audit.MetadataJSON(rec.GetString("metadata_json"))
	}
	return meta
}

const auditLogsCollection = "audit_logs"

func encryptForReveal(t *testing.T, plaintext string) string {
	t.Helper()
	secretKey := crypto.NormalizeSecretKey(testSecretBackendKey)
	ciphertext, err := crypto.Encrypt([]byte(plaintext), secretKey)
	if err != nil {
		t.Fatalf("crypto.Encrypt: %v", err)
	}
	return ciphertext
}

func TestRevealEnvVar_AdminGetsPlaintext(t *testing.T) {
	app, _ := envVarTestApp(t)
	admin := createTestUser(t, app, "envvar-reveal-admin@example.com", "Password1!", rbac.RoleAdmin)
	repo := createEnvVarTestRepo(t, app, "reveal-repo")
	stack := createEnvVarTestStack(t, app, "reveal-stack", repo.Id)
	row := createEnvVarRow(t, app, stack.Id, "TOKEN", encryptForReveal(t, "s3cr3t"), true, "internal")
	mux := envVarRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/"+row.Id+"/reveal", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control: no-store on a response carrying plaintext, got %q", got)
	}
	var out struct{ Value string }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Value != "s3cr3t" {
		t.Fatalf("expected decrypted value, got %q", out.Value)
	}
}

func TestRevealEnvVar_NonAdminForbidden(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createEnvVarTestRepo(t, app, "reveal-forbidden-repo")
	stack := createEnvVarTestStack(t, app, "reveal-forbidden-stack", repo.Id)
	row := createEnvVarRow(t, app, stack.Id, "TOKEN", encryptForReveal(t, "s3cr3t"), true, "internal")
	mux := envVarRoutesMux(t, app, operator)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/"+row.Id+"/reveal", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for operator role, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevealEnvVar_UnauthenticatedRejected(t *testing.T) {
	app, _ := envVarTestApp(t)
	repo := createEnvVarTestRepo(t, app, "reveal-unauth-repo")
	stack := createEnvVarTestStack(t, app, "reveal-unauth-stack", repo.Id)
	row := createEnvVarRow(t, app, stack.Id, "TOKEN", encryptForReveal(t, "s3cr3t"), true, "internal")
	mux := envVarRoutesMux(t, app, nil)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/"+row.Id+"/reveal", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when unauthenticated, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevealEnvVar_NonInternalSecretRejected(t *testing.T) {
	app, _ := envVarTestApp(t)
	admin := createTestUser(t, app, "envvar-reveal-nonsecret-admin@example.com", "Password1!", rbac.RoleAdmin)
	repo := createEnvVarTestRepo(t, app, "reveal-plain-repo")
	stack := createEnvVarTestStack(t, app, "reveal-plain-stack", repo.Id)
	plainRow := createEnvVarRow(t, app, stack.Id, "PLAIN", "plain-value", false, "")
	externalRow := createEnvVarRow(t, app, stack.Id, "EXTERNAL", "vault/data/path#field", true, "vault")
	mux := envVarRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/"+plainRow.Id+"/reveal", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-secret var, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSONRequest(t, mux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/"+externalRow.Id+"/reveal", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for external-provider secret, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevealEnvVar_UnknownCollectionRejected(t *testing.T) {
	app, _ := envVarTestApp(t)
	admin := createTestUser(t, app, "envvar-reveal-unknowncol-admin@example.com", "Password1!", rbac.RoleAdmin)
	mux := envVarRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/env-vars/users/some-id/reveal", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for disallowed collection, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevealEnvVar_MissingRecordNotFound(t *testing.T) {
	app, _ := envVarTestApp(t)
	admin := createTestUser(t, app, "envvar-reveal-missing-admin@example.com", "Password1!", rbac.RoleAdmin)
	mux := envVarRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/does-not-exist/reveal", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevealEnvVar_RecordsAuditEvent(t *testing.T) {
	app, _ := envVarTestApp(t)
	admin := createTestUser(t, app, "envvar-reveal-audit-admin@example.com", "Password1!", rbac.RoleAdmin)
	repo := createEnvVarTestRepo(t, app, "reveal-audit-repo")
	stack := createEnvVarTestStack(t, app, "reveal-audit-stack", repo.Id)
	row := createEnvVarRow(t, app, stack.Id, "TOKEN", encryptForReveal(t, "s3cr3t"), true, "internal")
	mux := envVarRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/"+row.Id+"/reveal", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	logs, err := app.FindAllRecords(auditLogsCollection, dbx.HashExp{"action": "env_vars.revealed"})
	if err != nil {
		t.Fatalf("find audit logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 audit row for env_vars.revealed, got %d", len(logs))
	}
}

func TestRevealStackEnvVars_DecryptsOnlyInternalSecrets(t *testing.T) {
	app, _ := envVarTestApp(t)
	admin := createTestUser(t, app, "envvar-reveal-all-admin@example.com", "Password1!", rbac.RoleAdmin)
	repo := createEnvVarTestRepo(t, app, "reveal-all-repo")
	stack := createEnvVarTestStack(t, app, "reveal-all-stack", repo.Id)
	createEnvVarRow(t, app, stack.Id, "SECRET", encryptForReveal(t, "s3cr3t"), true, "internal")
	createEnvVarRow(t, app, stack.Id, "PLAIN", "plain-value", false, "")
	createEnvVarRow(t, app, stack.Id, "EXTERNAL", "vault/data/path#field", true, "vault")
	mux := envVarRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/stacks/"+stack.Id+"/env-vars/reveal-all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control: no-store on a response carrying plaintext, got %q", got)
	}
	var out struct{ Values map[string]string }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Values) != 1 {
		t.Fatalf("expected only the internal secret in response, got %+v", out.Values)
	}
	if out.Values["SECRET"] != "s3cr3t" {
		t.Fatalf("expected decrypted SECRET value, got %q", out.Values["SECRET"])
	}
}

func TestRevealStackEnvVars_SkipsCorruptCiphertext(t *testing.T) {
	app, _ := envVarTestApp(t)
	admin := createTestUser(t, app, "envvar-reveal-corrupt-admin@example.com", "Password1!", rbac.RoleAdmin)
	repo := createEnvVarTestRepo(t, app, "reveal-corrupt-repo")
	stack := createEnvVarTestStack(t, app, "reveal-corrupt-stack", repo.Id)
	createEnvVarRow(t, app, stack.Id, "GOOD", encryptForReveal(t, "good-value"), true, "internal")
	createEnvVarRow(t, app, stack.Id, "CORRUPT", "not-valid-ciphertext", true, "internal")
	mux := envVarRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/stacks/"+stack.Id+"/env-vars/reveal-all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct{ Values map[string]string }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Values) != 1 || out.Values["GOOD"] != "good-value" {
		t.Fatalf("expected only GOOD to decrypt, got %+v", out.Values)
	}
}

func TestRevealStackEnvVars_NonAdminForbidden(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createEnvVarTestRepo(t, app, "reveal-all-forbidden-repo")
	stack := createEnvVarTestStack(t, app, "reveal-all-forbidden-stack", repo.Id)
	mux := envVarRoutesMux(t, app, operator)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/stacks/"+stack.Id+"/env-vars/reveal-all", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for operator role, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevealStackEnvVars_StackNotFound(t *testing.T) {
	app, _ := envVarTestApp(t)
	admin := createTestUser(t, app, "envvar-reveal-all-missing-admin@example.com", "Password1!", rbac.RoleAdmin)
	mux := envVarRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/stacks/does-not-exist/env-vars/reveal-all", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRevealStackEnvVars_RecordsAuditEvent(t *testing.T) {
	app, _ := envVarTestApp(t)
	admin := createTestUser(t, app, "envvar-reveal-all-audit-admin@example.com", "Password1!", rbac.RoleAdmin)
	repo := createEnvVarTestRepo(t, app, "reveal-all-audit-repo")
	stack := createEnvVarTestStack(t, app, "reveal-all-audit-stack", repo.Id)
	createEnvVarRow(t, app, stack.Id, "SECRET", encryptForReveal(t, "s3cr3t"), true, "internal")
	mux := envVarRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/stacks/"+stack.Id+"/env-vars/reveal-all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	logs, err := app.FindAllRecords(auditLogsCollection, dbx.HashExp{"action": "stack.env_vars.revealed_all"})
	if err != nil {
		t.Fatalf("find audit logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 audit row for stack.env_vars.revealed_all, got %d", len(logs))
	}
	if key, _ := auditMetadata(t, logs[0])["key"].(string); key != "SECRET" {
		t.Fatalf("expected audit row metadata to name the revealed key, got %+v", auditMetadata(t, logs[0]))
	}
}

// TestRevealStackEnvVars_RecordsOnePerKeyAuditEvent guards the audit
// granularity: a bulk reveal must name every key it disclosed, not just an
// aggregate count, and must never emit an event for a key that wasn't
// actually decrypted (non-secret, external-provider, or corrupt).
func TestRevealStackEnvVars_RecordsOnePerKeyAuditEvent(t *testing.T) {
	app, _ := envVarTestApp(t)
	admin := createTestUser(t, app, "envvar-reveal-all-multi-audit-admin@example.com", "Password1!", rbac.RoleAdmin)
	repo := createEnvVarTestRepo(t, app, "reveal-all-multi-audit-repo")
	stack := createEnvVarTestStack(t, app, "reveal-all-multi-audit-stack", repo.Id)
	createEnvVarRow(t, app, stack.Id, "SECRET_A", encryptForReveal(t, "value-a"), true, "internal")
	createEnvVarRow(t, app, stack.Id, "SECRET_B", encryptForReveal(t, "value-b"), true, "internal")
	createEnvVarRow(t, app, stack.Id, "PLAIN", "plain-value", false, "")
	createEnvVarRow(t, app, stack.Id, "EXTERNAL", "vault/data/path#field", true, "vault")
	createEnvVarRow(t, app, stack.Id, "CORRUPT", "not-valid-ciphertext", true, "internal")
	mux := envVarRoutesMux(t, app, admin)

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/stacks/"+stack.Id+"/env-vars/reveal-all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	logs, err := app.FindAllRecords(auditLogsCollection, dbx.HashExp{"action": "stack.env_vars.revealed_all"})
	if err != nil {
		t.Fatalf("find audit logs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 1 audit row per successfully decrypted key (2), got %d: %+v", len(logs), logs)
	}
	keys := map[string]bool{}
	for _, log := range logs {
		key, _ := auditMetadata(t, log)["key"].(string)
		keys[key] = true
	}
	if !keys["SECRET_A"] || !keys["SECRET_B"] {
		t.Fatalf("expected audit rows naming SECRET_A and SECRET_B, got keys=%+v", keys)
	}
}
