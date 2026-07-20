package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/protocol"
	wiresync "github.com/wireops/wireops/internal/sync"
)

// fakeWorkerDispatcher is a minimal sync.WorkerDispatcher stub so render-overrides
// routes (which require rr.workerOnline) can be exercised without a real worker
// WebSocket connection.
type fakeWorkerDispatcher struct {
	connected bool
}

func (f *fakeWorkerDispatcher) Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error) {
	return protocol.CommandResult{}, nil
}

func (f *fakeWorkerDispatcher) IsConnected(workerID string) bool {
	return f.connected
}

// setupRenderOverridesTestApp wires the render-overrides routes plus the same
// audit middleware cmd/serve.go binds globally, and treats every request as an
// authenticated admin so rbac.Require(rbac.CapOperateStacks) passes.
func setupRenderOverridesTestApp(t *testing.T, connected bool) (core.App, http.Handler, *core.Record) {
	t.Helper()
	app := newSetupTestApp(t)
	admin := createTestUser(t, app, "admin-overrides@example.com", "password123", "admin")
	dispatcher := &fakeWorkerDispatcher{connected: connected}

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:  app,
			Auth: admin,
			Event: router.Event{
				Response: w,
				Request:  req,
			},
		}, nil
	})

	r.BindFunc(audit.CustomRouteMiddleware(app))

	rr := routeRegistrar{
		r:         r,
		app:       app,
		scheduler: wiresync.NewScheduler(app, dispatcher),
		workerSvc: dispatcher,
	}
	rr.registerStackInspectionRoutes()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux, admin
}

func createOverridesTestRepoAndWorker(t *testing.T, app core.App) (*core.Record, *core.Record) {
	t.Helper()
	repoCol, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		t.Fatalf("find repositories collection: %v", err)
	}
	repo := core.NewRecord(repoCol)
	repo.Set("name", "overrides-repo")
	repo.Set("git_url", "https://example.com/overrides-repo.git")
	repo.Set("branch", "main")
	if err := app.Save(repo); err != nil {
		t.Fatalf("create test repository: %v", err)
	}

	workerCol, err := app.FindCollectionByNameOrId("workers")
	if err != nil {
		t.Fatalf("find workers collection: %v", err)
	}
	worker := core.NewRecord(workerCol)
	worker.Id = "" // let PocketBase generate a fresh id, used below for a unique fingerprint
	worker.Set("hostname", "overrides-worker")
	worker.Set("status", "ACTIVE")
	worker.Set("policy_inherit", true)
	worker.Set("fingerprint", "overrides-worker-fp-"+core.GenerateDefaultRandomId())
	if err := app.Save(worker); err != nil {
		t.Fatalf("create test worker: %v", err)
	}
	return repo, worker
}

// createOverridesTestStack creates a local-source stack (source_type=local) backed
// by workDir/docker-compose.yml, so stackWorkDir resolves without needing a cloned
// git repository on disk.
func createOverridesTestStack(t *testing.T, app core.App, repoID, workerID, workDir string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatalf("find stacks collection: %v", err)
	}
	stack := core.NewRecord(col)
	stack.Set("name", "overrides-stack")
	stack.Set("repository", repoID)
	stack.Set("worker", workerID)
	stack.Set("source_type", "local")
	stack.Set("import_path", filepath.Join(workDir, "docker-compose.yml"))
	if err := app.Save(stack); err != nil {
		t.Fatalf("create test stack: %v", err)
	}
	return stack
}

func writeOverridesComposeFile(t *testing.T, workDir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workDir, "docker-compose.yml"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write compose file: %v", err)
	}
}

func setGlobalAllowRenderOverrides(t *testing.T, app core.App, allow bool) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("worker_policies")
	if err != nil {
		t.Fatalf("find worker_policies collection: %v", err)
	}
	policy := core.NewRecord(col)
	policy.Set("enabled", true)
	policy.Set("allow_render_overrides", allow)
	if err := app.Save(policy); err != nil {
		t.Fatalf("save global worker policy: %v", err)
	}
}

func doRenderOverridesRequest(t *testing.T, mux http.Handler, method, stackID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = strings.NewReader(string(b))
	} else {
		reqBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, "/api/custom/stacks/"+stackID+"/render-overrides", reqBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestRenderOverridesGetEmptyWhenNoneSet(t *testing.T) {
	app, mux, _ := setupRenderOverridesTestApp(t, true)
	repo, worker := createOverridesTestRepoAndWorker(t, app)
	workDir := t.TempDir()
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, workDir)

	rec := doRenderOverridesRequest(t, mux, http.MethodGet, stack.Id, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	overrides, _ := resp["overrides"].(map[string]interface{})
	if len(overrides) != 0 {
		t.Fatalf("expected empty overrides, got %#v", resp["overrides"])
	}
}

func TestRenderOverridesGetUnknownStack(t *testing.T) {
	_, mux, _ := setupRenderOverridesTestApp(t, true)

	rec := doRenderOverridesRequest(t, mux, http.MethodGet, "nonexistent", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRenderOverridesPutRejectedWhenWorkerPolicyDisallows(t *testing.T) {
	app, mux, _ := setupRenderOverridesTestApp(t, true)
	repo, worker := createOverridesTestRepoAndWorker(t, app)
	workDir := t.TempDir()
	writeOverridesComposeFile(t, workDir, "name: overrides_stack\nservices:\n  web:\n    image: nginx:alpine\n")
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, workDir)
	// AllowRenderOverrides is off by default (no worker_policies record at all).

	rec := doRenderOverridesRequest(t, mux, http.MethodPut, stack.Id, map[string]interface{}{
		"overrides": map[string]interface{}{"web": map[string]interface{}{"image": "nginx:test"}},
	})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	reloaded, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if got := wiresync.LoadRenderOverrides(reloaded); len(got) != 0 {
		t.Fatalf("expected no overrides persisted after a rejected PUT, got %#v", got)
	}
}

func TestRenderOverridesPutRejectedWhenWorkerOffline(t *testing.T) {
	app, mux, _ := setupRenderOverridesTestApp(t, false) // worker never connected
	repo, worker := createOverridesTestRepoAndWorker(t, app)
	setGlobalAllowRenderOverrides(t, app, true)
	workDir := t.TempDir()
	writeOverridesComposeFile(t, workDir, "name: overrides_stack\nservices:\n  web:\n    image: nginx:alpine\n")
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, workDir)

	rec := doRenderOverridesRequest(t, mux, http.MethodPut, stack.Id, map[string]interface{}{
		"overrides": map[string]interface{}{"web": map[string]interface{}{"image": "nginx:test"}},
	})

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRenderOverridesPutRejectsUnknownService(t *testing.T) {
	app, mux, _ := setupRenderOverridesTestApp(t, true)
	repo, worker := createOverridesTestRepoAndWorker(t, app)
	setGlobalAllowRenderOverrides(t, app, true)
	workDir := t.TempDir()
	writeOverridesComposeFile(t, workDir, "name: overrides_stack\nservices:\n  web:\n    image: nginx:alpine\n")
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, workDir)

	rec := doRenderOverridesRequest(t, mux, http.MethodPut, stack.Id, map[string]interface{}{
		"overrides": map[string]interface{}{"does-not-exist": map[string]interface{}{"image": "nginx:test"}},
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "does-not-exist") {
		t.Fatalf("expected error to name the unknown service, got: %s", rec.Body.String())
	}

	reloaded, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if got := wiresync.LoadRenderOverrides(reloaded); len(got) != 0 {
		t.Fatalf("expected no overrides persisted after a rejected PUT, got %#v", got)
	}
}

func TestRenderOverridesPutRejectsPolicyViolatingOverride(t *testing.T) {
	app, mux, _ := setupRenderOverridesTestApp(t, true)
	repo, worker := createOverridesTestRepoAndWorker(t, app)
	workDir := t.TempDir()
	writeOverridesComposeFile(t, workDir, "name: overrides_stack\nservices:\n  web:\n    image: nginx:alpine\n")
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, workDir)

	col, err := app.FindCollectionByNameOrId("worker_policies")
	if err != nil {
		t.Fatalf("find worker_policies collection: %v", err)
	}
	policy := core.NewRecord(col)
	policy.Set("enabled", true)
	policy.Set("allow_render_overrides", true)
	policy.Set("prevent_latest_images", true)
	if err := app.Save(policy); err != nil {
		t.Fatalf("save global worker policy: %v", err)
	}

	rec := doRenderOverridesRequest(t, mux, http.MethodPut, stack.Id, map[string]interface{}{
		"overrides": map[string]interface{}{"web": map[string]interface{}{"image": "nginx:latest"}},
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	reloaded, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if got := wiresync.LoadRenderOverrides(reloaded); len(got) != 0 {
		t.Fatalf("expected no overrides persisted after a policy-rejected PUT, got %#v", got)
	}
}

func TestRenderOverridesPutRejectsEmptyBody(t *testing.T) {
	app, mux, _ := setupRenderOverridesTestApp(t, true)
	repo, worker := createOverridesTestRepoAndWorker(t, app)
	setGlobalAllowRenderOverrides(t, app, true)
	workDir := t.TempDir()
	writeOverridesComposeFile(t, workDir, "name: overrides_stack\nservices:\n  web:\n    image: nginx:alpine\n")
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, workDir)

	rec := doRenderOverridesRequest(t, mux, http.MethodPut, stack.Id, map[string]interface{}{
		"overrides": map[string]interface{}{},
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRenderOverridesPutPersistsAndAudits(t *testing.T) {
	app, mux, _ := setupRenderOverridesTestApp(t, true)
	repo, worker := createOverridesTestRepoAndWorker(t, app)
	setGlobalAllowRenderOverrides(t, app, true)
	workDir := t.TempDir()
	writeOverridesComposeFile(t, workDir, "name: overrides_stack\nservices:\n  web:\n    image: nginx:alpine\n")
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, workDir)

	rec := doRenderOverridesRequest(t, mux, http.MethodPut, stack.Id, map[string]interface{}{
		"overrides": map[string]interface{}{"web": map[string]interface{}{"image": "nginx:test"}},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	reloaded, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	got := wiresync.LoadRenderOverrides(reloaded)
	if len(got) != 1 || got["web"].Image != "nginx:test" {
		t.Fatalf("expected persisted override for web=nginx:test, got %#v", got)
	}

	logs, err := app.FindAllRecords("audit_logs", dbx.HashExp{
		"action":      "stack.render_overrides.set",
		"resource_id": stack.Id,
	})
	if err != nil {
		t.Fatalf("query audit logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 render_overrides.set audit log, got %d", len(logs))
	}
}

func TestRenderOverridesDeleteClearsAndAudits(t *testing.T) {
	app, mux, _ := setupRenderOverridesTestApp(t, true)
	repo, worker := createOverridesTestRepoAndWorker(t, app)
	setGlobalAllowRenderOverrides(t, app, true)
	workDir := t.TempDir()
	writeOverridesComposeFile(t, workDir, "name: overrides_stack\nservices:\n  web:\n    image: nginx:alpine\n")
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, workDir)

	stack.Set("render_overrides", map[string]wiresync.ServiceOverride{"web": {Image: "nginx:test"}})
	if err := app.Save(stack); err != nil {
		t.Fatalf("seed overrides: %v", err)
	}

	rec := doRenderOverridesRequest(t, mux, http.MethodDelete, stack.Id, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	reloaded, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if got := wiresync.LoadRenderOverrides(reloaded); len(got) != 0 {
		t.Fatalf("expected overrides cleared, got %#v", got)
	}

	logs, err := app.FindAllRecords("audit_logs", dbx.HashExp{
		"action":      "stack.render_overrides.clear",
		"resource_id": stack.Id,
	})
	if err != nil {
		t.Fatalf("query audit logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 render_overrides.clear audit log, got %d", len(logs))
	}
}

func TestRenderOverridesDeleteRejectedWhenWorkerOffline(t *testing.T) {
	app, mux, _ := setupRenderOverridesTestApp(t, false)
	repo, worker := createOverridesTestRepoAndWorker(t, app)
	workDir := t.TempDir()
	writeOverridesComposeFile(t, workDir, "name: overrides_stack\nservices:\n  web:\n    image: nginx:alpine\n")
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, workDir)

	rec := doRenderOverridesRequest(t, mux, http.MethodDelete, stack.Id, nil)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}
