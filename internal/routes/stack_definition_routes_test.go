package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/hooks"
	"github.com/wireops/wireops/internal/logstream"
	"github.com/wireops/wireops/internal/rbac"
	wiresync "github.com/wireops/wireops/internal/sync"
)

func definitionRefreshRoutesMux(t *testing.T, app core.App, auth *core.Record, scheduler *wiresync.Scheduler) http.Handler {
	t.Helper()
	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{App: app, Auth: auth, Event: router.Event{Response: w, Request: req}}, nil
	})
	r.BindFunc(audit.CustomRouteMiddleware(app))
	rr := routeRegistrar{r: r, app: app, scheduler: scheduler}
	rr.registerStackDefinitionRoutes()
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return mux
}

func setupDefinitionRefreshTest(t *testing.T) (core.App, http.Handler, *wiresync.Scheduler) {
	t.Helper()
	t.Setenv("SECRET_KEY", testSecretBackendKey)
	t.Setenv("REPOS_WORKSPACE", t.TempDir())
	app := newSetupTestApp(t)
	auth := createTestUser(t, app, "definition-refresh@example.com", "Password1!", rbac.RoleOperator)
	scheduler := wiresync.NewScheduler(app, nil)
	hooks.Register(app, scheduler, nil, logstream.New())
	return app, definitionRefreshRoutesMux(t, app, auth, scheduler), scheduler
}

func decodeDefinitionRefresh(t *testing.T, rec *httptest.ResponseRecorder) definitionRefreshResponse {
	t.Helper()
	var out definitionRefreshResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return out
}

func TestResyncDefinitionRefreshesEmbeddedTimeoutWithoutDeploying(t *testing.T) {
	app, mux, scheduler := setupDefinitionRefreshTest(t)
	fixtureDir := t.TempDir()
	localGitFixture(t, fixtureDir, map[string]string{
		"docker-compose.yml": `x-wireops:
  version: wireops.v1
  name: api
  timeout: 3m
  compose:
    remove_orphans: false
    force_pull: true
  jobs:
    wait_running: true
  worker:
    tags: [prod, amd64]
  sync:
    interval: 45s
services:
  web:
    image: nginx
`,
	})
	repo := createMigrateTestRepo(t, app, "definition-embedded", fixtureDir)
	worker := createMigrateTestWorker(t, app)
	stack := createMigrateTestStack(t, app, repo.Id, map[string]any{
		"worker":                 worker.Id,
		"status":                 "paused",
		"config_source":          "compose_embedded",
		"deploy_timeout_seconds": 60,
		"remove_orphans":         true,
		"force_pull":             false,
		"sync_interval_seconds":  10,
		"wait_running_jobs":      "never",
		"worker_tags":            []string{"old"},
	})
	// Stack creation has a background registration hook. A first IsSyncing
	// probe can run before the goroutine has acquired its mutex, so give the
	// paused reconcile one brief chance to run before waiting it back to idle.
	time.Sleep(100 * time.Millisecond)
	waitUntilNotSyncing(t, scheduler, stack.Id)

	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stack.Id+"/resync-definition", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeDefinitionRefresh(t, rec)
	if out.Status != "updated" || out.Source != "compose_embedded" || out.DeployTriggered {
		t.Fatalf("unexpected refresh response: %+v", out)
	}
	if out.CommitSHA == "" {
		t.Fatal("expected refreshed commit SHA")
	}
	if got, ok := out.ChangedFields["deploy_timeout_seconds"]; !ok || got.From != float64(60) || got.To != float64(180) {
		t.Fatalf("expected timeout change 60 -> 180, got %#v", got)
	}

	updated, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if updated.GetInt("deploy_timeout_seconds") != 180 {
		t.Fatalf("expected timeout=180, got %d", updated.GetInt("deploy_timeout_seconds"))
	}
	if !updated.GetBool("force_pull") || updated.GetBool("remove_orphans") || updated.GetInt("sync_interval_seconds") != 45 || updated.GetString("wait_running_jobs") != "always" {
		t.Fatalf("expected definition-derived fields to refresh, got %+v", updated.FieldsData())
	}
	if got := updated.GetStringSlice("worker_tags"); len(got) != 2 || got[0] != "prod" || got[1] != "amd64" {
		t.Fatalf("expected refreshed worker tags, got %v", got)
	}
	if updated.GetString("status") != "paused" {
		t.Fatalf("definition refresh must not deploy or change status, got %q", updated.GetString("status"))
	}
}

func TestResyncDefinitionRefreshesStandaloneDerivedComposePath(t *testing.T) {
	app, mux, scheduler := setupDefinitionRefreshTest(t)
	fixtureDir := t.TempDir()
	localGitFixture(t, fixtureDir, map[string]string{
		"apps/api/wireops.yaml":       "version: wireops.v1\nname: api\ntimeout: 7m\n",
		"apps/api/docker-compose.yml": "services:\n  web:\n    image: nginx\n",
	})
	repo := createMigrateTestRepo(t, app, "definition-standalone", fixtureDir)
	worker := createMigrateTestWorker(t, app)
	stack := createMigrateTestStack(t, app, repo.Id, map[string]any{
		"worker":                 worker.Id,
		"status":                 "paused",
		"config_source":          "wireops_file",
		"wireops_file_path":      "apps/api/wireops.yaml",
		"deploy_timeout_seconds": 60,
		"compose_path":           ".",
		"compose_file":           "old.yml",
	})
	time.Sleep(100 * time.Millisecond)
	waitUntilNotSyncing(t, scheduler, stack.Id)

	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stack.Id+"/resync-definition", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeDefinitionRefresh(t, rec)
	if out.Status != "updated" {
		t.Fatalf("expected updated response, got %+v", out)
	}
	if _, ok := out.ChangedFields["compose_path"]; !ok {
		t.Fatalf("expected compose_path in diff, got %#v", out.ChangedFields)
	}

	updated, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if updated.GetInt("deploy_timeout_seconds") != 420 {
		t.Fatalf("expected timeout=420, got %d", updated.GetInt("deploy_timeout_seconds"))
	}
	if updated.GetString("compose_path") != filepath.Join("apps", "api") || updated.GetString("compose_file") != "docker-compose.yml" {
		t.Fatalf("expected resolved standalone compose path, got %q/%q", updated.GetString("compose_path"), updated.GetString("compose_file"))
	}
}

func TestResyncDefinitionRejectsManualAndBusyStacks(t *testing.T) {
	app, mux, scheduler := setupDefinitionRefreshTest(t)
	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/does-not-exist/resync-definition", map[string]any{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown stack, got %d: %s", rec.Code, rec.Body.String())
	}

	repo := createMigrateTestRepo(t, app, "definition-manual", "https://example.invalid/repo.git")
	worker := createMigrateTestWorker(t, app)
	manual := createMigrateTestStack(t, app, repo.Id, map[string]any{"config_source": "manual", "status": "paused", "worker": worker.Id})
	time.Sleep(100 * time.Millisecond)
	waitUntilNotSyncing(t, scheduler, manual.Id)

	rec = doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+manual.Id+"/resync-definition", map[string]any{})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for manual stack, got %d: %s", rec.Code, rec.Body.String())
	}

	release := scheduler.LockStackForTest(manual.Id)
	defer release()
	rec = doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+manual.Id+"/resync-definition", map[string]any{})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for busy stack, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResyncDefinitionLeavesTheStackUntouchedWhenGitDefinitionIsInvalid(t *testing.T) {
	app, mux, scheduler := setupDefinitionRefreshTest(t)
	fixtureDir := t.TempDir()
	localGitFixture(t, fixtureDir, map[string]string{
		"docker-compose.yml": "x-wireops:\n  version: wireops.v1\n  name: api\n  timeout: not-a-duration\nservices:\n  web:\n    image: nginx\n",
	})
	repo := createMigrateTestRepo(t, app, "definition-invalid", fixtureDir)
	worker := createMigrateTestWorker(t, app)
	stack := createMigrateTestStack(t, app, repo.Id, map[string]any{
		"worker":                 worker.Id,
		"status":                 "paused",
		"config_source":          "compose_embedded",
		"deploy_timeout_seconds": 60,
	})
	time.Sleep(100 * time.Millisecond)
	waitUntilNotSyncing(t, scheduler, stack.Id)

	rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stack.Id+"/resync-definition", map[string]any{})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid definition, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload stack: %v", err)
	}
	if updated.GetInt("deploy_timeout_seconds") != 60 {
		t.Fatalf("invalid definition must not change timeout, got %d", updated.GetInt("deploy_timeout_seconds"))
	}
}

// TestResyncDefinitionPreview groups the preview scenarios behind one app
// bootstrap: each PocketBase test app costs about a second and this package
// already runs close to the per-package timeout in CI.
func TestResyncDefinitionPreview(t *testing.T) {
	app, mux, scheduler := setupDefinitionRefreshTest(t)

	const divergentCompose = `x-wireops:
  version: wireops.v1
  name: api
  timeout: 3m
  compose:
    remove_orphans: false
    force_pull: true
  worker:
    tags: [prod, amd64]
  sync:
    interval: 45s
services:
  web:
    image: nginx
`

	// One worker for both subtests: createMigrateTestWorker uses a fixed
	// fingerprint, which is unique-constrained.
	worker := createMigrateTestWorker(t, app)

	newPausedStack := func(t *testing.T, name string, fields map[string]any) string {
		t.Helper()
		fixtureDir := t.TempDir()
		localGitFixture(t, fixtureDir, map[string]string{"docker-compose.yml": divergentCompose})
		repo := createMigrateTestRepo(t, app, name, fixtureDir)
		base := map[string]any{
			"worker":        worker.Id,
			"status":        "paused",
			"config_source": "compose_embedded",
		}
		for k, v := range fields {
			base[k] = v
		}
		stack := createMigrateTestStack(t, app, repo.Id, base)
		time.Sleep(100 * time.Millisecond)
		waitUntilNotSyncing(t, scheduler, stack.Id)
		return stack.Id
	}

	t.Run("ReportsTheDiffWithoutSavingAnything", func(t *testing.T) {
		stackID := newPausedStack(t, "definition-preview-diff", map[string]any{
			"deploy_timeout_seconds": 60,
			"remove_orphans":         true,
			"force_pull":             false,
			"sync_interval_seconds":  10,
			"worker_tags":            []string{"old"},
		})

		rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stackID+"/resync-definition?preview=true", map[string]any{})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		out := decodeDefinitionRefresh(t, rec)
		if !out.Preview || out.Status != "would-update" || out.DeployTriggered {
			t.Fatalf("unexpected preview response: %+v", out)
		}
		if got, ok := out.ChangedFields["deploy_timeout_seconds"]; !ok || got.From != float64(60) || got.To != float64(180) {
			t.Fatalf("expected previewed timeout change 60 -> 180, got %#v", got)
		}
		if _, ok := out.ChangedFields["worker_tags"]; !ok {
			t.Fatal("expected worker_tags in the preview diff: applying it relocates the stack")
		}

		// The point of the flag: nothing was persisted.
		unchanged, err := app.FindRecordById("stacks", stackID)
		if err != nil {
			t.Fatalf("reload stack: %v", err)
		}
		if got := unchanged.GetInt("deploy_timeout_seconds"); got != 60 {
			t.Fatalf("preview must not save: deploy_timeout_seconds = %d, want 60", got)
		}
		if got := unchanged.GetStringSlice("worker_tags"); len(got) != 1 || got[0] != "old" {
			t.Fatalf("preview must not save: worker_tags = %v, want [old]", got)
		}
		if got := unchanged.GetInt("sync_interval_seconds"); got != 10 {
			t.Fatalf("preview must not save: sync_interval_seconds = %d, want 10", got)
		}
	})

	t.Run("ReportsUnchangedWhenTheDefinitionAlreadyMatches", func(t *testing.T) {
		stackID := newPausedStack(t, "definition-preview-match", map[string]any{
			"deploy_timeout_seconds": 180,
			"remove_orphans":         false,
			"force_pull":             true,
			"sync_interval_seconds":  45,
			"wait_running_jobs":      "never",
			"worker_tags":            []string{"prod", "amd64"},
		})

		rec := doJSONRequest(t, mux, http.MethodPost, "/api/custom/stacks/"+stackID+"/resync-definition?preview=true", map[string]any{})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		out := decodeDefinitionRefresh(t, rec)
		if !out.Preview || out.Status != "unchanged" {
			t.Fatalf("expected an unchanged preview, got %+v", out)
		}
		if len(out.ChangedFields) != 0 {
			t.Fatalf("expected no changed fields, got %#v", out.ChangedFields)
		}
	})
}
