package routes

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/rbac"
)

// wireopsCreateRoutesMux registers only POST /api/custom/stacks/from-wireops
// against a real (non-hook-wired) test app, exercising the real
// CloneOrFetchContext path via localGitFixture (defined in
// stack_migrate_routes_test.go) rather than mocking it out.
func wireopsCreateRoutesMux(t *testing.T, app core.App, auth *core.Record) http.Handler {
	t.Helper()

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
			Auth:  auth,
		}, nil
	})

	rr := routeRegistrar{r: r, app: app}
	rr.registerCreateFromWireopsRoute()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return mux
}

// setupWireopsCreateTest wires a test app + operator + a repository backed by
// a real local git fixture containing wireops.yaml and docker-compose.yml,
// plus an active worker — everything registerCreateFromWireopsRoute needs to
// succeed end to end.
func setupWireopsCreateTest(t *testing.T) (core.App, http.Handler, *core.Record, *core.Record) {
	t.Helper()
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)
	app := newSetupTestApp(t)
	operator := createTestUser(t, app, "wireops-create-operator@example.com", "Password1!", rbac.RoleOperator)

	fixtureDir := t.TempDir()
	localGitFixture(t, fixtureDir, map[string]string{
		"wireops.yaml":       "version: wireops.v1\nname: api\n",
		"docker-compose.yml": "services:\n  web:\n    image: nginx\n",
	})
	repo := createMigrateTestRepo(t, app, "wireops-create-repo", fixtureDir)
	worker := createMigrateTestWorker(t, app)

	mux := wireopsCreateRoutesMux(t, app, operator)
	return app, mux, repo, worker
}

type createFromWireopsResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

func TestCreateFromWireops_DefaultsToPendingAndDeploysImmediately(t *testing.T) {
	app, mux, repo, worker := setupWireopsCreateTest(t)

	body := map[string]any{
		"repository":   repo.Id,
		"worker":       worker.Id,
		"wireops_file": "wireops.yaml",
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-wireops", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out createFromWireopsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Status != "pending" {
		t.Fatalf("expected response status=pending, got %q", out.Status)
	}
	if out.Name != "api" {
		t.Fatalf("expected name from wireops.yaml, got %q", out.Name)
	}

	stack, err := app.FindRecordById("stacks", out.ID)
	if err != nil {
		t.Fatalf("find created stack: %v", err)
	}
	if stack.GetString("status") != "pending" {
		t.Fatalf("expected stored status=pending, got %q", stack.GetString("status"))
	}
	if !stack.GetBool("auto_sync") {
		t.Fatal("expected auto_sync=true")
	}
}

func TestCreateFromWireops_PausedCreatesPausedStack(t *testing.T) {
	app, mux, repo, worker := setupWireopsCreateTest(t)

	// Mirrors what the create-stack wizard sends when it has pending env vars
	// to save: the stack must not auto-deploy until those vars are persisted.
	body := map[string]any{
		"repository":   repo.Id,
		"worker":       worker.Id,
		"wireops_file": "wireops.yaml",
		"paused":       true,
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-wireops", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out createFromWireopsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Status != "paused" {
		t.Fatalf("expected response status=paused, got %q", out.Status)
	}

	stack, err := app.FindRecordById("stacks", out.ID)
	if err != nil {
		t.Fatalf("find created stack: %v", err)
	}
	if stack.GetString("status") != "paused" {
		t.Fatalf("expected stored status=paused, got %q", stack.GetString("status"))
	}
}

func TestCreateFromWireops_RequiresRepositoryWorkerAndFile(t *testing.T) {
	_, mux, repo, worker := setupWireopsCreateTest(t)

	cases := []map[string]any{
		{"worker": worker.Id, "wireops_file": "wireops.yaml"},
		{"repository": repo.Id, "wireops_file": "wireops.yaml"},
		{"repository": repo.Id, "worker": worker.Id},
	}
	for _, body := range cases {
		rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-wireops", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for body %+v, got %d: %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateFromWireops_WorkerNotFound(t *testing.T) {
	_, mux, repo, _ := setupWireopsCreateTest(t)

	body := map[string]any{
		"repository":   repo.Id,
		"worker":       "does-not-exist",
		"wireops_file": "wireops.yaml",
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-wireops", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing worker, got %d: %s", rec.Code, rec.Body.String())
	}
}
