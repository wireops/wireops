package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/rbac"
)

// composeCreateRoutesMux registers only POST /api/custom/stacks/from-compose
// against a real (non-hook-wired) test app, mirroring
// wireopsCreateRoutesMux in wireops_create_route_test.go.
func composeCreateRoutesMux(t *testing.T, app core.App, auth *core.Record) http.Handler {
	t.Helper()

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
			Auth:  auth,
		}, nil
	})

	rr := routeRegistrar{r: r, app: app}
	rr.registerCreateFromComposeRoute()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return mux
}

// setupComposeCreateTest wires a test app + operator + a repository backed by
// a real local git fixture containing a compose file with an embedded
// x-wireops block, plus an active worker — everything
// registerCreateFromComposeRoute needs to succeed end to end.
func setupComposeCreateTest(t *testing.T) (core.App, http.Handler, *core.Record, *core.Record) {
	t.Helper()
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)
	app := newSetupTestApp(t)
	operator := createTestUser(t, app, "compose-create-operator@example.com", "Password1!", rbac.RoleOperator)

	fixtureDir := t.TempDir()
	localGitFixture(t, fixtureDir, map[string]string{
		"docker-compose.yml": "x-wireops:\n  version: wireops.v1\n  name: api\nservices:\n  web:\n    image: nginx\n",
	})
	repo := createMigrateTestRepo(t, app, "compose-create-repo", fixtureDir)
	worker := createMigrateTestWorker(t, app)

	mux := composeCreateRoutesMux(t, app, operator)
	return app, mux, repo, worker
}

type createFromComposeResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

func TestCreateFromComposeDefaultsToPendingAndDeploysImmediately(t *testing.T) {
	app, mux, repo, worker := setupComposeCreateTest(t)

	body := map[string]any{
		"repository": repo.Id,
		"worker":     worker.Id,
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-compose", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out createFromComposeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Status != "pending" {
		t.Fatalf("expected response status=pending, got %q", out.Status)
	}
	if out.Name != "api" {
		t.Fatalf("expected name from x-wireops block, got %q", out.Name)
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
	if stack.GetString("config_source") != "compose_embedded" {
		t.Fatalf("expected config_source=compose_embedded, got %q", stack.GetString("config_source"))
	}
	if stack.GetString("compose_file") != "docker-compose.yml" {
		t.Fatalf("expected compose_file=docker-compose.yml, got %q", stack.GetString("compose_file"))
	}
	if stack.GetString("wireops_file_path") != "" {
		t.Fatalf("expected no wireops_file_path for a compose-embedded stack, got %q", stack.GetString("wireops_file_path"))
	}
}

func TestCreateFromComposePausedCreatesPausedStack(t *testing.T) {
	app, mux, repo, worker := setupComposeCreateTest(t)

	body := map[string]any{
		"repository": repo.Id,
		"worker":     worker.Id,
		"paused":     true,
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-compose", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var out createFromComposeResponse
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

func TestCreateFromComposeRequiresRepositoryAndWorker(t *testing.T) {
	_, mux, repo, worker := setupComposeCreateTest(t)

	cases := []map[string]any{
		{"worker": worker.Id},
		{"repository": repo.Id},
	}
	for _, body := range cases {
		rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-compose", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for body %+v, got %d: %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateFromComposeWorkerNotFound(t *testing.T) {
	_, mux, repo, _ := setupComposeCreateTest(t)

	body := map[string]any{
		"repository": repo.Id,
		"worker":     "does-not-exist",
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-compose", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing worker, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateFromComposeInvalidBody(t *testing.T) {
	_, mux, _, _ := setupComposeCreateTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/from-compose", strings.NewReader("{not-json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON body, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateFromComposeRejectsPathTraversal(t *testing.T) {
	_, mux, repo, worker := setupComposeCreateTest(t)

	cases := []map[string]any{
		{"repository": repo.Id, "worker": worker.Id, "compose_path": "../escape"},
		{"repository": repo.Id, "worker": worker.Id, "compose_file": "../escape.yml"},
	}
	for _, body := range cases {
		rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-compose", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for body %+v, got %d: %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateFromComposeFileNotFound(t *testing.T) {
	_, mux, repo, worker := setupComposeCreateTest(t)

	body := map[string]any{
		"repository":   repo.Id,
		"worker":       worker.Id,
		"compose_file": "does-not-exist.yml",
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-compose", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing compose file, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateFromComposeMalformedXWireopsBlock(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)
	app := newSetupTestApp(t)
	operator := createTestUser(t, app, "compose-create-malformed@example.com", "Password1!", rbac.RoleOperator)

	fixtureDir := t.TempDir()
	localGitFixture(t, fixtureDir, map[string]string{
		"docker-compose.yml": "x-wireops: [this, is, not, a, map]\nservices:\n  web:\n    image: nginx\n",
	})
	repo := createMigrateTestRepo(t, app, "compose-create-malformed-repo", fixtureDir)
	worker := createMigrateTestWorker(t, app)

	mux := composeCreateRoutesMux(t, app, operator)

	body := map[string]any{
		"repository": repo.Id,
		"worker":     worker.Id,
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-compose", body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for malformed x-wireops block, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateFromComposeRepositoryNotFound(t *testing.T) {
	_, mux, _, worker := setupComposeCreateTest(t)

	body := map[string]any{
		"repository": "does-not-exist",
		"worker":     worker.Id,
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-compose", body)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected a non-200 response for a repository that fails to sync, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateFromComposeMissingXWireopsBlock(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)
	app := newSetupTestApp(t)
	operator := createTestUser(t, app, "compose-create-noblock@example.com", "Password1!", rbac.RoleOperator)

	fixtureDir := t.TempDir()
	localGitFixture(t, fixtureDir, map[string]string{
		"docker-compose.yml": "services:\n  web:\n    image: nginx\n",
	})
	repo := createMigrateTestRepo(t, app, "compose-create-noblock-repo", fixtureDir)
	worker := createMigrateTestWorker(t, app)

	mux := composeCreateRoutesMux(t, app, operator)

	body := map[string]any{
		"repository": repo.Id,
		"worker":     worker.Id,
	}
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/from-compose", body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for compose file without x-wireops, got %d: %s", rec.Code, rec.Body.String())
	}
}
