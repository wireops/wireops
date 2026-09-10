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

// TestRevealEnvVar covers the single-value reveal route as subtests sharing
// one app/repo/mux bootstrap each (admin, operator, unauthenticated) instead
// of one full PocketBase app spin-up per case — each bootstrap costs about a
// second, and this package's suite is already close to go test's 10-minute
// per-package timeout (see 27dcd087, "Consolidate new route tests to fix
// internal/routes CI timeout"). Every subtest still gets its own stack/row so
// state from one case can't leak into another.
func TestRevealEnvVar(t *testing.T) {
	app, operator := envVarTestApp(t)
	admin := createTestUser(t, app, "envvar-reveal-admin@example.com", "Password1!", rbac.RoleAdmin)
	repo := createEnvVarTestRepo(t, app, "reveal-repo")
	adminMux := envVarRoutesMux(t, app, admin)
	operatorMux := envVarRoutesMux(t, app, operator)
	anonMux := envVarRoutesMux(t, app, nil)

	t.Run("AdminGetsPlaintext", func(t *testing.T) {
		stack := createEnvVarTestStack(t, app, "reveal-stack-admin", repo.Id)
		const value = "s3cr3t\nprivate-key\\n\r\n"
		row := createEnvVarRow(t, app, stack.Id, "TOKEN", encryptForReveal(t, value), true, "internal")

		rec := doJSONRequest(t, adminMux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/"+row.Id+"/reveal", nil)
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
		if out.Value != value {
			t.Fatalf("expected decrypted value, got %q", out.Value)
		}
	})

	t.Run("NonAdminForbidden", func(t *testing.T) {
		stack := createEnvVarTestStack(t, app, "reveal-stack-nonadmin", repo.Id)
		row := createEnvVarRow(t, app, stack.Id, "TOKEN", encryptForReveal(t, "s3cr3t"), true, "internal")

		rec := doJSONRequest(t, operatorMux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/"+row.Id+"/reveal", nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for operator role, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("UnauthenticatedRejected", func(t *testing.T) {
		stack := createEnvVarTestStack(t, app, "reveal-stack-unauth", repo.Id)
		row := createEnvVarRow(t, app, stack.Id, "TOKEN", encryptForReveal(t, "s3cr3t"), true, "internal")

		rec := doJSONRequest(t, anonMux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/"+row.Id+"/reveal", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 when unauthenticated, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("NonInternalSecretRejected", func(t *testing.T) {
		stack := createEnvVarTestStack(t, app, "reveal-stack-plain", repo.Id)
		plainRow := createEnvVarRow(t, app, stack.Id, "PLAIN", "plain-value", false, "")
		externalRow := createEnvVarRow(t, app, stack.Id, "EXTERNAL", "vault/data/path#field", true, "vault")

		rec := doJSONRequest(t, adminMux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/"+plainRow.Id+"/reveal", nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for non-secret var, got %d: %s", rec.Code, rec.Body.String())
		}

		rec = doJSONRequest(t, adminMux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/"+externalRow.Id+"/reveal", nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for external-provider secret, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("UnknownCollectionRejected", func(t *testing.T) {
		rec := doJSONRequest(t, adminMux, http.MethodGet, "/api/custom/env-vars/users/some-id/reveal", nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for disallowed collection, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("MissingRecordNotFound", func(t *testing.T) {
		rec := doJSONRequest(t, adminMux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/does-not-exist/reveal", nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("RecordsAuditEvent", func(t *testing.T) {
		stack := createEnvVarTestStack(t, app, "reveal-stack-audit", repo.Id)
		row := createEnvVarRow(t, app, stack.Id, "TOKEN", encryptForReveal(t, "s3cr3t"), true, "internal")

		rec := doJSONRequest(t, adminMux, http.MethodGet, "/api/custom/env-vars/stack_env_vars/"+row.Id+"/reveal", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Scoped to this row's id — the shared app accumulates audit rows
		// from every other subtest, so filtering by action alone would
		// double-count.
		logs, err := app.FindAllRecords(auditLogsCollection, dbx.HashExp{"action": "env_vars.revealed", "resource_id": row.Id})
		if err != nil {
			t.Fatalf("find audit logs: %v", err)
		}
		if len(logs) != 1 {
			t.Fatalf("expected 1 audit row for env_vars.revealed, got %d", len(logs))
		}
	})
}

// TestRevealStackEnvVars covers the bulk reveal-all route as subtests sharing
// one app/mux bootstrap each — see TestRevealEnvVar's doc comment for why.
func TestRevealStackEnvVars(t *testing.T) {
	app, operator := envVarTestApp(t)
	admin := createTestUser(t, app, "envvar-reveal-all-admin@example.com", "Password1!", rbac.RoleAdmin)
	repo := createEnvVarTestRepo(t, app, "reveal-all-repo")
	adminMux := envVarRoutesMux(t, app, admin)
	operatorMux := envVarRoutesMux(t, app, operator)

	t.Run("DecryptsOnlyInternalSecrets", func(t *testing.T) {
		stack := createEnvVarTestStack(t, app, "reveal-all-stack", repo.Id)
		createEnvVarRow(t, app, stack.Id, "SECRET", encryptForReveal(t, "s3cr3t"), true, "internal")
		createEnvVarRow(t, app, stack.Id, "PLAIN", "plain-value", false, "")
		createEnvVarRow(t, app, stack.Id, "EXTERNAL", "vault/data/path#field", true, "vault")

		rec := doJSONRequest(t, adminMux, http.MethodGet, "/api/custom/stacks/"+stack.Id+"/env-vars/reveal-all", nil)
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
	})

	t.Run("SkipsCorruptCiphertext", func(t *testing.T) {
		stack := createEnvVarTestStack(t, app, "reveal-corrupt-stack", repo.Id)
		createEnvVarRow(t, app, stack.Id, "GOOD", encryptForReveal(t, "good-value"), true, "internal")
		createEnvVarRow(t, app, stack.Id, "CORRUPT", "not-valid-ciphertext", true, "internal")

		rec := doJSONRequest(t, adminMux, http.MethodGet, "/api/custom/stacks/"+stack.Id+"/env-vars/reveal-all", nil)
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
	})

	t.Run("NonAdminForbidden", func(t *testing.T) {
		stack := createEnvVarTestStack(t, app, "reveal-all-forbidden-stack", repo.Id)

		rec := doJSONRequest(t, operatorMux, http.MethodGet, "/api/custom/stacks/"+stack.Id+"/env-vars/reveal-all", nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for operator role, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("StackNotFound", func(t *testing.T) {
		rec := doJSONRequest(t, adminMux, http.MethodGet, "/api/custom/stacks/does-not-exist/env-vars/reveal-all", nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("RecordsAuditEvent", func(t *testing.T) {
		stack := createEnvVarTestStack(t, app, "reveal-all-audit-stack", repo.Id)
		row := createEnvVarRow(t, app, stack.Id, "SECRET", encryptForReveal(t, "s3cr3t"), true, "internal")

		rec := doJSONRequest(t, adminMux, http.MethodGet, "/api/custom/stacks/"+stack.Id+"/env-vars/reveal-all", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		logs, err := app.FindAllRecords(auditLogsCollection, dbx.HashExp{"action": "stack.env_vars.revealed_all", "resource_id": row.Id})
		if err != nil {
			t.Fatalf("find audit logs: %v", err)
		}
		if len(logs) != 1 {
			t.Fatalf("expected 1 audit row for stack.env_vars.revealed_all, got %d", len(logs))
		}
		if key, _ := auditMetadata(t, logs[0])["key"].(string); key != "SECRET" {
			t.Fatalf("expected audit row metadata to name the revealed key, got %+v", auditMetadata(t, logs[0]))
		}
	})

	// RecordsOnePerKeyAuditEvent guards the audit granularity: a bulk reveal
	// must name every key it disclosed, not just an aggregate count, and must
	// never emit an event for a key that wasn't actually decrypted (non-secret,
	// external-provider, or corrupt).
	t.Run("RecordsOnePerKeyAuditEvent", func(t *testing.T) {
		stack := createEnvVarTestStack(t, app, "reveal-all-multi-audit-stack", repo.Id)
		createEnvVarRow(t, app, stack.Id, "SECRET_A", encryptForReveal(t, "value-a"), true, "internal")
		createEnvVarRow(t, app, stack.Id, "SECRET_B", encryptForReveal(t, "value-b"), true, "internal")
		createEnvVarRow(t, app, stack.Id, "PLAIN", "plain-value", false, "")
		createEnvVarRow(t, app, stack.Id, "EXTERNAL", "vault/data/path#field", true, "vault")
		createEnvVarRow(t, app, stack.Id, "CORRUPT", "not-valid-ciphertext", true, "internal")

		rec := doJSONRequest(t, adminMux, http.MethodGet, "/api/custom/stacks/"+stack.Id+"/env-vars/reveal-all", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		rows, err := app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": stack.Id, "key": "SECRET_A"})
		if err != nil || len(rows) != 1 {
			t.Fatalf("find SECRET_A row: %v (rows=%d)", err, len(rows))
		}
		secretARowID := rows[0].Id
		rows, err = app.FindAllRecords("stack_env_vars", dbx.HashExp{"stack": stack.Id, "key": "SECRET_B"})
		if err != nil || len(rows) != 1 {
			t.Fatalf("find SECRET_B row: %v (rows=%d)", err, len(rows))
		}
		secretBRowID := rows[0].Id

		logs, err := app.FindAllRecords(auditLogsCollection, dbx.HashExp{
			"action":      "stack.env_vars.revealed_all",
			"resource_id": []any{secretARowID, secretBRowID},
		})
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
	})
}
