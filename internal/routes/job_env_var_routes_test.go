package routes

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/jobscheduler"
	"github.com/wireops/wireops/internal/rbac"
)

// jobEnvVarRoutesMux wires the job env-vars bulk route through the real
// RegisterJobRoutes (rather than registering it standalone), so the test
// also exercises the actual production route-registration path — a nil
// WorkerDispatcher is fine since none of these tests trigger a job run.
func jobEnvVarRoutesMux(t *testing.T, app core.App, auth *core.Record) http.Handler {
	t.Helper()

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
			Auth:  auth,
		}, nil
	})

	sched := jobscheduler.NewScheduler(app, nil, t.TempDir())
	RegisterJobRoutes(r, app, sched)

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return mux
}

// createJobEnvVarRow saves a job_env_vars row directly (bypassing the
// route/HTTP layer, and bypassing the encryption hook — internal/hooks isn't
// registered in this test app, matching createEnvVarRow's stack_env_vars
// counterpart). For secret=true+internal rows callers should pass an
// already-encrypted value (crypto.Encrypt).
func createJobEnvVarRow(t *testing.T, app core.App, jobID, key, value string, secret bool, provider string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("job_env_vars")
	if err != nil {
		t.Fatalf("find job_env_vars collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("job", jobID)
	rec.Set("key", key)
	rec.Set("value", value)
	rec.Set("secret", secret)
	rec.Set("secret_provider", provider)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save job_env_var %s: %v", key, err)
	}
	return rec
}

func TestBulkUpsertJobEnvVarsReplaceMode(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createTestRepository(t, app, "job-bulk-replace-repo")
	job := createTestScheduledJob(t, app, repo.Id, "job-bulk-replace", "active")
	createJobEnvVarRow(t, app, job.Id, "KEEP", "keep-value", false, "")
	createJobEnvVarRow(t, app, job.Id, "DROP", "drop-value", false, "")
	mux := jobEnvVarRoutesMux(t, app, operator)

	body := map[string]any{
		"mode": "replace",
		"vars": []map[string]any{
			{"key": "KEEP", "value": "keep-value-changed", "secret": false, "secret_provider": ""},
			{"key": "NEW", "value": "new-value", "secret": false, "secret_provider": ""},
		},
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/jobs/"+job.Id+"/env-vars/bulk", body)
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

	rows, err := app.FindAllRecords("job_env_vars", dbx.HashExp{"job": job.Id})
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

func TestBulkUpsertJobEnvVarsPreservesUnchangedSecret(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createTestRepository(t, app, "job-bulk-secret-repo")
	job := createTestScheduledJob(t, app, repo.Id, "job-bulk-secret", "active")

	secretKey := crypto.NormalizeSecretKey(testSecretBackendKey)
	ciphertext, err := crypto.Encrypt([]byte("s3cr3t"), secretKey)
	if err != nil {
		t.Fatalf("crypto.Encrypt: %v", err)
	}
	createJobEnvVarRow(t, app, job.Id, "TOKEN", ciphertext, true, "internal")
	mux := jobEnvVarRoutesMux(t, app, operator)

	body := map[string]any{
		"mode": "replace",
		"vars": []map[string]any{
			{"key": "TOKEN", "value": "", "secret": true, "secret_provider": "internal"},
		},
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/jobs/"+job.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rows, err := app.FindAllRecords("job_env_vars", dbx.HashExp{"job": job.Id})
	if err != nil {
		t.Fatalf("find rows: %v", err)
	}
	if len(rows) != 1 || rows[0].GetString("value") != ciphertext {
		t.Fatalf("expected ciphertext unchanged, got rows=%+v", rows)
	}
}

func TestBulkUpsertJobEnvVarsRejectsDuplicateKeysInPayload(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createTestRepository(t, app, "job-bulk-dup-repo")
	job := createTestScheduledJob(t, app, repo.Id, "job-bulk-dup", "active")
	mux := jobEnvVarRoutesMux(t, app, operator)

	body := map[string]any{
		"mode": "replace",
		"vars": []map[string]any{
			{"key": "FOO", "value": "1"},
			{"key": "FOO", "value": "2"},
		},
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/jobs/"+job.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBulkUpsertJobEnvVarsRejectsInvalidKey(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createTestRepository(t, app, "job-bulk-invalid-key-repo")
	job := createTestScheduledJob(t, app, repo.Id, "job-bulk-invalid-key", "active")
	mux := jobEnvVarRoutesMux(t, app, operator)

	body := map[string]any{
		"mode": "replace",
		"vars": []map[string]any{
			{"key": "1-not-an-identifier", "value": "1"},
		},
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/jobs/"+job.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBulkUpsertJobEnvVarsDowngradeToNonSecretClearsValue(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createTestRepository(t, app, "job-bulk-downgrade-repo")
	job := createTestScheduledJob(t, app, repo.Id, "job-bulk-downgrade", "active")

	secretKey := crypto.NormalizeSecretKey(testSecretBackendKey)
	ciphertext, err := crypto.Encrypt([]byte("s3cr3t"), secretKey)
	if err != nil {
		t.Fatalf("crypto.Encrypt: %v", err)
	}
	createJobEnvVarRow(t, app, job.Id, "TOKEN", ciphertext, true, "internal")
	mux := jobEnvVarRoutesMux(t, app, operator)

	body := map[string]any{
		"mode": "replace",
		"vars": []map[string]any{
			{"key": "TOKEN", "value": "", "secret": false, "secret_provider": ""},
		},
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/jobs/"+job.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rows, err := app.FindAllRecords("job_env_vars", dbx.HashExp{"job": job.Id})
	if err != nil {
		t.Fatalf("find rows: %v", err)
	}
	if len(rows) != 1 || rows[0].GetBool("secret") || rows[0].GetString("value") != "" {
		t.Fatalf("expected TOKEN downgraded and cleared, got rows=%+v", rows)
	}
}

func TestBulkUpsertJobEnvVarsRejectsEmptyKey(t *testing.T) {
	app, operator := envVarTestApp(t)
	repo := createTestRepository(t, app, "job-bulk-empty-repo")
	job := createTestScheduledJob(t, app, repo.Id, "job-bulk-empty", "active")
	mux := jobEnvVarRoutesMux(t, app, operator)

	body := map[string]any{
		"mode": "replace",
		"vars": []map[string]any{
			{"key": "  ", "value": "1"},
		},
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/jobs/"+job.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBulkUpsertJobEnvVarsJobNotFound(t *testing.T) {
	app, operator := envVarTestApp(t)
	mux := jobEnvVarRoutesMux(t, app, operator)

	body := map[string]any{"mode": "replace", "vars": []map[string]any{}}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/jobs/does-not-exist/env-vars/bulk", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBulkUpsertJobEnvVarsRequiresManageJobsCapability(t *testing.T) {
	app, _ := envVarTestApp(t)
	viewer := createTestUser(t, app, "job-envvar-viewer@example.com", "Password1!", rbac.RoleViewer)
	repo := createTestRepository(t, app, "job-bulk-viewer-repo")
	job := createTestScheduledJob(t, app, repo.Id, "job-bulk-viewer", "active")
	mux := jobEnvVarRoutesMux(t, app, viewer)

	body := map[string]any{"mode": "replace", "vars": []map[string]any{}}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/jobs/"+job.Id+"/env-vars/bulk", body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer role, got %d: %s", rec.Code, rec.Body.String())
	}
}
