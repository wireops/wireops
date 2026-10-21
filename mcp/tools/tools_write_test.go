package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

// recordedRequest is one call the fake wireops server saw.
type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Body   map[string]any
}

// recorder is a fake wireops server for the write tools: it records every
// request and answers from a "METHOD /path" route table, defaulting to an
// empty JSON object so a tool that only cares about the status still works.
type recorder struct {
	mu       sync.Mutex
	requests []recordedRequest
	routes   map[string]string
	srv      *httptest.Server
}

func newRecorder(routes map[string]string) *recorder {
	rec := &recorder{routes: routes}
	rec.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		rec.mu.Lock()
		rec.requests = append(rec.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Body:   body,
		})
		response, ok := rec.routes[r.Method+" "+r.URL.Path]
		rec.mu.Unlock()
		if !ok {
			response = `{}`
		}
		_, _ = w.Write([]byte(response))
	}))
	return rec
}

func (r *recorder) close()                 { r.srv.Close() }
func (r *recorder) client() *client.Client { return client.New(r.srv.URL) }

func (r *recorder) all() []recordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedRequest(nil), r.requests...)
}

// writes returns every non-GET request, i.e. the ones that would have
// changed something on a real server.
func (r *recorder) writes() []recordedRequest {
	var out []recordedRequest
	for _, req := range r.all() {
		if req.Method != http.MethodGet {
			out = append(out, req)
		}
	}
	return out
}

// last returns the final request, failing the test if there was none.
func (r *recorder) last(t *testing.T) recordedRequest {
	t.Helper()
	all := r.all()
	if len(all) == 0 {
		t.Fatal("expected at least one request to the wireops server")
	}
	return all[len(all)-1]
}

func (r *recorder) expectLast(t *testing.T, method, path string) recordedRequest {
	t.Helper()
	got := r.last(t)
	if got.Method != method || got.Path != path {
		t.Fatalf("expected %s %s, got %s %s", method, path, got.Method, got.Path)
	}
	return got
}

// expectNoWrites asserts the tool refused before touching anything.
func (r *recorder) expectNoWrites(t *testing.T) {
	t.Helper()
	if writes := r.writes(); len(writes) != 0 {
		t.Fatalf("expected no write request, got %v", writes)
	}
}

// stackNamed serves the fields=name lookup requireConfirm does.
func stackNamed(name string) map[string]string {
	return map[string]string{"GET /api/collections/stacks/records/stack1": `{"name":"` + name + `"}`}
}

// stackStatus serves the fields=status lookup set_stack_paused does before
// resuming a stack.
func stackStatus(status string) map[string]string {
	return map[string]string{"GET /api/collections/stacks/records/stack1": `{"status":"` + status + `"}`}
}

func mergeRoutes(sets ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, set := range sets {
		for k, v := range set {
			out[k] = v
		}
	}
	return out
}

// servicesRoute serves the stack service list the container tools resolve
// against.
func servicesRoute() map[string]string {
	return map[string]string{
		"GET /api/custom/stacks/stack1/services": `[{"service_name":"web","container_id":"c-web"},{"service_name":"db","container_id":"c-db"}]`,
	}
}

// ---------------------------------------------------------------------------
// stacks
// ---------------------------------------------------------------------------

func TestDeployStackPostsSync(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := deployStack(rec.client())(ctxWithKey(), nil, models.DeployStackInput{StackID: "stack1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec.expectLast(t, http.MethodPost, "/api/custom/stacks/stack1/sync")
}

func TestDeployStackMissingAPIKey(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if _, _, err := deployStack(rec.client())(t.Context(), nil, models.DeployStackInput{StackID: "stack1"}); err == nil {
		t.Fatal("expected error when no API key on context")
	}
}

func TestForceRedeployStackDefaultsPauseAfterToTrue(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := forceRedeployStack(rec.client())(ctxWithKey(), nil, models.ForceRedeployStackInput{
		StackID:            "stack1",
		RecreateContainers: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPost, "/api/custom/stacks/stack1/force-redeploy")
	if got.Body["pause_after_redeploy"] != true {
		t.Fatalf("expected pause_after_redeploy to default to true, got %v", got.Body["pause_after_redeploy"])
	}
	if got.Body["recreate_containers"] != true {
		t.Fatalf("expected recreate_containers to be forwarded, got %v", got.Body["recreate_containers"])
	}
}

func TestForceRedeployStackHonorsExplicitPauseAfterFalse(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	pauseAfter := false
	_, _, err := forceRedeployStack(rec.client())(ctxWithKey(), nil, models.ForceRedeployStackInput{
		StackID:    "stack1",
		PauseAfter: &pauseAfter,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := rec.last(t); got.Body["pause_after_redeploy"] != false {
		t.Fatalf("expected pause_after_redeploy false, got %v", got.Body["pause_after_redeploy"])
	}
}

func TestForceRedeployStackRequiresConfirmForVolumeRecreation(t *testing.T) {
	rec := newRecorder(stackNamed("billing"))
	defer rec.close()

	_, _, err := forceRedeployStack(rec.client())(ctxWithKey(), nil, models.ForceRedeployStackInput{
		StackID:         "stack1",
		RecreateVolumes: true,
		Confirm:         "billling",
	})
	if err == nil {
		t.Fatal("expected confirm mismatch to fail")
	}
	if !strings.Contains(err.Error(), "billing") {
		t.Fatalf("expected the error to name the expected value, got %v", err)
	}
	rec.expectNoWrites(t)
}

func TestForceRedeployStackProceedsWithMatchingConfirm(t *testing.T) {
	rec := newRecorder(stackNamed("billing"))
	defer rec.close()

	_, _, err := forceRedeployStack(rec.client())(ctxWithKey(), nil, models.ForceRedeployStackInput{
		StackID:         "stack1",
		RecreateVolumes: true,
		Confirm:         "billing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec.expectLast(t, http.MethodPost, "/api/custom/stacks/stack1/force-redeploy")
}

func TestRollbackStackRequiresCommitSHA(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if _, _, err := rollbackStack(rec.client())(ctxWithKey(), nil, models.RollbackStackInput{StackID: "stack1"}); err == nil {
		t.Fatal("expected error without commit_sha")
	}
	rec.expectNoWrites(t)
}

func TestRollbackStackPostsCommitSHA(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := rollbackStack(rec.client())(ctxWithKey(), nil, models.RollbackStackInput{StackID: "stack1", CommitSHA: "abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPost, "/api/custom/stacks/stack1/rollback")
	if got.Body["commit_sha"] != "abc123" {
		t.Fatalf("expected commit_sha forwarded, got %v", got.Body["commit_sha"])
	}
}

func TestSetStackPausedPatchesStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		paused  bool
		current string
		want    string
	}{
		{"pausing an active stack", true, "active", "paused"},
		{"pausing a failing stack", true, "error", "paused"},
		{"resuming a paused stack", false, "paused", "active"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecorder(stackStatus(tc.current))
			defer rec.close()

			_, _, err := setStackPaused(rec.client())(ctxWithKey(), nil, models.SetStackPausedInput{StackID: "stack1", Paused: tc.paused})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := rec.expectLast(t, http.MethodPatch, "/api/collections/stacks/records/stack1")
			if got.Body["status"] != tc.want {
				t.Fatalf("expected status %q, got %v", tc.want, got.Body["status"])
			}
		})
	}
}

func TestSetStackPausedRefusesToResumeAStackThatIsNotPaused(t *testing.T) {
	// Writing "active" here would replace a real status with a healthy-looking
	// one that nothing recomputes until the next reconcile.
	for _, current := range []string{"error", "degraded", "syncing", "active"} {
		t.Run(current, func(t *testing.T) {
			rec := newRecorder(stackStatus(current))
			defer rec.close()

			_, _, err := setStackPaused(rec.client())(ctxWithKey(), nil, models.SetStackPausedInput{StackID: "stack1"})
			if err == nil {
				t.Fatalf("expected resuming a %q stack to be refused", current)
			}
			if !strings.Contains(err.Error(), current) {
				t.Fatalf("expected the error to name the current status, got %v", err)
			}
			rec.expectNoWrites(t)
		})
	}
}

func TestSetStackRenderOverridesRejectsEmptySet(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if _, _, err := setStackRenderOverrides(rec.client())(ctxWithKey(), nil, models.SetStackRenderOverridesInput{StackID: "stack1"}); err == nil {
		t.Fatal("expected error for an empty override set")
	}
	rec.expectNoWrites(t)
}

func TestSetStackRenderOverridesPutsOverrides(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := setStackRenderOverrides(rec.client())(ctxWithKey(), nil, models.SetStackRenderOverridesInput{
		StackID:   "stack1",
		Overrides: map[string]models.ServiceOverrideInput{"web": {Image: "nginx:1.29"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPut, "/api/custom/stacks/stack1/render-overrides")
	overrides, ok := got.Body["overrides"].(map[string]any)
	if !ok {
		t.Fatalf("expected an overrides object, got %v", got.Body)
	}
	web, ok := overrides["web"].(map[string]any)
	if !ok || web["image"] != "nginx:1.29" {
		t.Fatalf("expected web.image forwarded, got %v", overrides)
	}
}

func TestClearStackRenderOverridesDeletes(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := clearStackRenderOverrides(rec.client())(ctxWithKey(), nil, models.ClearStackRenderOverridesInput{StackID: "stack1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec.expectLast(t, http.MethodDelete, "/api/custom/stacks/stack1/render-overrides")
}

func TestRestartContainerResolvesServiceName(t *testing.T) {
	rec := newRecorder(servicesRoute())
	defer rec.close()

	_, _, err := restartContainer(rec.client())(ctxWithKey(), nil, models.ContainerActionInput{StackID: "stack1", ServiceName: "db"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPost, "/api/custom/stacks/stack1/container/restart")
	if got.Body["container_id"] != "c-db" {
		t.Fatalf("expected the resolved container id, got %v", got.Body["container_id"])
	}
}

func TestRestartContainerRequiresATarget(t *testing.T) {
	rec := newRecorder(servicesRoute())
	defer rec.close()

	if _, _, err := restartContainer(rec.client())(ctxWithKey(), nil, models.ContainerActionInput{StackID: "stack1"}); err == nil {
		t.Fatal("expected error without container_id or service_name")
	}
	rec.expectNoWrites(t)
}

func TestStopContainerConfirmsWithServiceName(t *testing.T) {
	rec := newRecorder(servicesRoute())
	defer rec.close()

	_, _, err := stopContainer(rec.client())(ctxWithKey(), nil, models.StopContainerInput{
		StackID:     "stack1",
		ContainerID: "c-web",
		Confirm:     "db",
	})
	if err == nil {
		t.Fatal("expected confirm mismatch to fail")
	}
	if !strings.Contains(err.Error(), "web") {
		t.Fatalf("expected the error to name the container's own service, got %v", err)
	}
	rec.expectNoWrites(t)

	_, _, err = stopContainer(rec.client())(ctxWithKey(), nil, models.StopContainerInput{
		StackID:     "stack1",
		ContainerID: "c-web",
		Confirm:     "web",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPost, "/api/custom/stacks/stack1/container/stop")
	if got.Body["container_id"] != "c-web" {
		t.Fatalf("expected container id forwarded, got %v", got.Body["container_id"])
	}
}

func TestStopContainerRefusesAnUnnamedService(t *testing.T) {
	rec := newRecorder(map[string]string{
		"GET /api/custom/stacks/stack1/services": `[{"service_name":"","container_id":"c-orphan"}]`,
	})
	defer rec.close()

	// Resolved by container id, this service has no compose name to confirm
	// against. An omitted confirm must not match that empty string and turn
	// the guard off.
	_, _, err := stopContainer(rec.client())(ctxWithKey(), nil, models.StopContainerInput{
		StackID:     "stack1",
		ContainerID: "c-orphan",
	})
	if err == nil {
		t.Fatal("expected the stop to be refused with no name to confirm against")
	}
	rec.expectNoWrites(t)
}

func TestTransferStackRequiresConfirm(t *testing.T) {
	rec := newRecorder(stackNamed("billing"))
	defer rec.close()

	_, _, err := transferStack(rec.client())(ctxWithKey(), nil, models.TransferStackInput{
		StackID:        "stack1",
		TargetWorkerID: "worker2",
	})
	if err == nil {
		t.Fatal("expected missing confirm to fail")
	}
	rec.expectNoWrites(t)
}

func TestTransferStackPostsTargetWorker(t *testing.T) {
	rec := newRecorder(stackNamed("billing"))
	defer rec.close()

	_, _, err := transferStack(rec.client())(ctxWithKey(), nil, models.TransferStackInput{
		StackID:        "stack1",
		TargetWorkerID: "worker2",
		Confirm:        "billing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPost, "/api/custom/stacks/stack1/transfer")
	if got.Body["target_worker_id"] != "worker2" {
		t.Fatalf("expected target_worker_id forwarded, got %v", got.Body["target_worker_id"])
	}
}

func TestDeleteStackRefusesOnConfirmMismatch(t *testing.T) {
	rec := newRecorder(stackNamed("billing"))
	defer rec.close()

	_, _, err := deleteStack(rec.client())(ctxWithKey(), nil, models.DeleteStackInput{StackID: "stack1", Confirm: "nope"})
	if err == nil {
		t.Fatal("expected confirm mismatch to fail")
	}
	rec.expectNoWrites(t)
}

func TestDeleteStackSendsForceQuery(t *testing.T) {
	rec := newRecorder(stackNamed("billing"))
	defer rec.close()

	_, _, err := deleteStack(rec.client())(ctxWithKey(), nil, models.DeleteStackInput{StackID: "stack1", Force: true, Confirm: "billing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodDelete, "/api/custom/stacks/stack1")
	if got.Query != "force=true" {
		t.Fatalf("expected force=true query, got %q", got.Query)
	}
}

func TestSetStackEnvVarsRejectsUnknownMode(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := setStackEnvVars(rec.client())(ctxWithKey(), nil, models.SetStackEnvVarsInput{StackID: "stack1", Mode: "merge"})
	if err == nil {
		t.Fatal("expected an unknown mode to fail")
	}
	rec.expectNoWrites(t)
}

func TestSetStackEnvVarsAppendNeedsNoConfirm(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := setStackEnvVars(rec.client())(ctxWithKey(), nil, models.SetStackEnvVarsInput{
		StackID: "stack1",
		Mode:    "append",
		Vars:    []models.BulkEnvVarInput{{Key: "PORT", Value: "8080"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPost, "/api/custom/stacks/stack1/env-vars/bulk")
	if got.Body["mode"] != "append" {
		t.Fatalf("expected mode forwarded, got %v", got.Body["mode"])
	}
}

func TestSetStackEnvVarsReplaceRequiresConfirm(t *testing.T) {
	rec := newRecorder(stackNamed("billing"))
	defer rec.close()

	_, _, err := setStackEnvVars(rec.client())(ctxWithKey(), nil, models.SetStackEnvVarsInput{
		StackID: "stack1",
		Mode:    "replace",
		Vars:    []models.BulkEnvVarInput{{Key: "PORT", Value: "8080"}},
	})
	if err == nil {
		t.Fatal("expected replace without confirm to fail")
	}
	rec.expectNoWrites(t)
}

func TestCopyStackEnvVarsRequiresSourceStack(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if _, _, err := copyStackEnvVars(rec.client())(ctxWithKey(), nil, models.CopyStackEnvVarsInput{StackID: "stack1"}); err == nil {
		t.Fatal("expected error without source_stack")
	}
	rec.expectNoWrites(t)
}

func TestImportStackRequiresNameWorkerAndPath(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if _, _, err := importStack(rec.client())(ctxWithKey(), nil, models.ImportStackInput{Name: "legacy"}); err == nil {
		t.Fatal("expected error without worker_id and import_path")
	}
	rec.expectNoWrites(t)
}

func TestImportStackPostsBody(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := importStack(rec.client())(ctxWithKey(), nil, models.ImportStackInput{
		Name:       "legacy",
		WorkerID:   "worker1",
		ImportPath: "/srv/legacy/docker-compose.yml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPost, "/api/custom/stacks/import")
	if got.Body["import_path"] != "/srv/legacy/docker-compose.yml" {
		t.Fatalf("expected import_path forwarded, got %v", got.Body["import_path"])
	}
}

// ---------------------------------------------------------------------------
// repositories
// ---------------------------------------------------------------------------

func TestSyncRepositoryPosts(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := syncRepository(rec.client())(ctxWithKey(), nil, models.SyncRepositoryInput{RepositoryID: "repo1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec.expectLast(t, http.MethodPost, "/api/custom/repositories/repo1/sync")
}

func TestCreateStackFromComposePostsBody(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := createStackFromCompose(rec.client())(ctxWithKey(), nil, models.CreateStackFromComposeInput{
		Repository:  "repo1",
		Worker:      "worker1",
		ComposePath: "apps/web",
		Paused:      true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPost, "/api/custom/stacks/from-compose")
	if got.Body["compose_path"] != "apps/web" || got.Body["paused"] != true {
		t.Fatalf("unexpected body: %v", got.Body)
	}
}

func TestCreateStackFromWireopsRequiresWireopsFile(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := createStackFromWireops(rec.client())(ctxWithKey(), nil, models.CreateStackFromWireopsInput{
		Repository: "repo1",
		Worker:     "worker1",
	})
	if err == nil {
		t.Fatal("expected error without wireops_file")
	}
	rec.expectNoWrites(t)
}

func TestLintComposeRequiresRepository(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if _, _, err := lintCompose(rec.client())(ctxWithKey(), nil, models.LintComposeInput{}); err == nil {
		t.Fatal("expected error without repository")
	}
	rec.expectNoWrites(t)
}

func TestPreviewStackMigrationDoesNotConfirmServerSide(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := previewStackMigration(rec.client())(ctxWithKey(), nil, models.PreviewStackMigrationInput{
		StackID:    "stack1",
		Repository: "repo2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPost, "/api/custom/stacks/stack1/migrate/preview")
	if got.Body["confirm"] != false {
		t.Fatalf("expected the preview to send confirm=false, got %v", got.Body["confirm"])
	}
}

func TestMigrateStackSetsServerConfirmOnlyAfterNameMatches(t *testing.T) {
	rec := newRecorder(stackNamed("billing"))
	defer rec.close()

	_, _, err := migrateStack(rec.client())(ctxWithKey(), nil, models.MigrateStackInput{
		StackID:    "stack1",
		Repository: "repo2",
		Confirm:    "wrong",
	})
	if err == nil {
		t.Fatal("expected confirm mismatch to fail")
	}
	rec.expectNoWrites(t)

	_, _, err = migrateStack(rec.client())(ctxWithKey(), nil, models.MigrateStackInput{
		StackID:    "stack1",
		Repository: "repo2",
		Confirm:    "billing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPost, "/api/custom/stacks/stack1/migrate")
	if got.Body["confirm"] != true {
		t.Fatalf("expected the server's confirm gate to be set, got %v", got.Body["confirm"])
	}
}

func TestRepoDefinitionSendsFileQuery(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := repoDefinition(rec.client(), "job-definition")(ctxWithKey(), nil, models.RepoFileDefinitionInput{
		RepositoryID: "repo1",
		Path:         "jobs/backup.yaml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodGet, "/api/custom/repositories/repo1/job-definition")
	if got.Query != "file=jobs%2Fbackup.yaml" {
		t.Fatalf("unexpected query: %q", got.Query)
	}
}

func TestRepoDefinitionRequiresPath(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := repoDefinition(rec.client(), "compose-definition")(ctxWithKey(), nil, models.RepoFileDefinitionInput{RepositoryID: "repo1"})
	if err == nil {
		t.Fatal("expected error without path")
	}
}

func TestCreateRepositoryDefaultsBranchToMain(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := createRepository(rec.client())(ctxWithKey(), nil, models.CreateRepositoryInput{
		Name:   "infra",
		GitURL: "https://example.com/infra.git",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPost, "/api/collections/repositories/records")
	if got.Body["branch"] != "main" {
		t.Fatalf("expected branch to default to main, got %v", got.Body["branch"])
	}
}

func TestUpdateRepositoryOnlySendsGivenFields(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := updateRepository(rec.client())(ctxWithKey(), nil, models.UpdateRepositoryInput{
		RepositoryID: "repo1",
		Branch:       "develop",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPatch, "/api/collections/repositories/records/repo1")
	if len(got.Body) != 1 || got.Body["branch"] != "develop" {
		t.Fatalf("expected only branch in the payload, got %v", got.Body)
	}
}

func TestUpdateRepositoryRejectsEmptyUpdate(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if _, _, err := updateRepository(rec.client())(ctxWithKey(), nil, models.UpdateRepositoryInput{RepositoryID: "repo1"}); err == nil {
		t.Fatal("expected error when nothing was passed to update")
	}
	rec.expectNoWrites(t)
}

func TestDeleteRepositoryConfirmsAgainstName(t *testing.T) {
	routes := map[string]string{"GET /api/collections/repositories/records/repo1": `{"name":"infra"}`}
	rec := newRecorder(routes)
	defer rec.close()

	_, _, err := deleteRepository(rec.client())(ctxWithKey(), nil, models.DeleteRepositoryInput{RepositoryID: "repo1", Confirm: "infa"})
	if err == nil {
		t.Fatal("expected confirm mismatch to fail")
	}
	rec.expectNoWrites(t)

	_, _, err = deleteRepository(rec.client())(ctxWithKey(), nil, models.DeleteRepositoryInput{RepositoryID: "repo1", Confirm: "infra"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec.expectLast(t, http.MethodDelete, "/api/collections/repositories/records/repo1")
}

// ---------------------------------------------------------------------------
// jobs
// ---------------------------------------------------------------------------

func TestRunJobPosts(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if _, _, err := runJob(rec.client())(ctxWithKey(), nil, models.RunJobInput{JobID: "job1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec.expectLast(t, http.MethodPost, "/api/custom/jobs/job1/run")
}

func TestCancelJobRunPosts(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if _, _, err := cancelJobRun(rec.client())(ctxWithKey(), nil, models.JobRunInput{RunID: "run1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec.expectLast(t, http.MethodPost, "/api/custom/job-runs/run1/cancel")
}

func TestDeleteJobRunRequiresLiteralConfirm(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if _, _, err := deleteJobRun(rec.client())(ctxWithKey(), nil, models.DeleteJobRunInput{RunID: "run1"}); err == nil {
		t.Fatal("expected missing confirm to fail")
	}
	rec.expectNoWrites(t)

	if _, _, err := deleteJobRun(rec.client())(ctxWithKey(), nil, models.DeleteJobRunInput{RunID: "run1", Confirm: "delete"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec.expectLast(t, http.MethodDelete, "/api/custom/job-runs/run1")
}

func TestSetJobEnvVarsReplaceConfirmsAgainstJobName(t *testing.T) {
	routes := map[string]string{"GET /api/collections/scheduled_jobs/records/job1": `{"name":"nightly-backup"}`}
	rec := newRecorder(routes)
	defer rec.close()

	_, _, err := setJobEnvVars(rec.client())(ctxWithKey(), nil, models.SetJobEnvVarsInput{
		JobID: "job1",
		Mode:  "replace",
		Vars:  []models.BulkEnvVarInput{{Key: "TARGET", Value: "s3"}},
	})
	if err == nil || !strings.Contains(err.Error(), "nightly-backup") {
		t.Fatalf("expected the error to name the job, got %v", err)
	}
	rec.expectNoWrites(t)

	_, _, err = setJobEnvVars(rec.client())(ctxWithKey(), nil, models.SetJobEnvVarsInput{
		JobID:   "job1",
		Mode:    "replace",
		Vars:    []models.BulkEnvVarInput{{Key: "TARGET", Value: "s3"}},
		Confirm: "nightly-backup",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec.expectLast(t, http.MethodPost, "/api/custom/jobs/job1/env-vars/bulk")
}

func TestCreateScheduledJobRequiresFileAndRepository(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if _, _, err := createScheduledJob(rec.client())(ctxWithKey(), nil, models.CreateScheduledJobInput{Name: "nightly"}); err == nil {
		t.Fatal("expected error without repository and job_file")
	}
	rec.expectNoWrites(t)
}

func TestCreateScheduledJobDefaultsToDisabled(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	_, _, err := createScheduledJob(rec.client())(ctxWithKey(), nil, models.CreateScheduledJobInput{
		Name:       "nightly",
		Repository: "repo1",
		JobFile:    "jobs/backup.yaml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPost, "/api/collections/scheduled_jobs/records")
	if got.Body["enabled"] != false {
		t.Fatalf("expected enabled to default to false, got %v", got.Body["enabled"])
	}
}

func TestUpdateScheduledJobSendsEnabledFalseWhenExplicit(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	enabled := false
	_, _, err := updateScheduledJob(rec.client())(ctxWithKey(), nil, models.UpdateScheduledJobInput{JobID: "job1", Enabled: &enabled})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.expectLast(t, http.MethodPatch, "/api/collections/scheduled_jobs/records/job1")
	if len(got.Body) != 1 || got.Body["enabled"] != false {
		t.Fatalf("expected only enabled=false in the payload, got %v", got.Body)
	}
}

func TestDeleteScheduledJobConfirmsAgainstName(t *testing.T) {
	routes := map[string]string{"GET /api/collections/scheduled_jobs/records/job1": `{"name":"nightly-backup"}`}
	rec := newRecorder(routes)
	defer rec.close()

	if _, _, err := deleteScheduledJob(rec.client())(ctxWithKey(), nil, models.DeleteScheduledJobInput{JobID: "job1", Confirm: "nightly"}); err == nil {
		t.Fatal("expected confirm mismatch to fail")
	}
	rec.expectNoWrites(t)
}

// ---------------------------------------------------------------------------
// workers
// ---------------------------------------------------------------------------

func TestRevokeWorkerConfirmsAgainstHostname(t *testing.T) {
	routes := map[string]string{"GET /api/collections/workers/records/worker1": `{"hostname":"edge-01"}`}
	rec := newRecorder(routes)
	defer rec.close()

	_, _, err := revokeWorker(rec.client())(ctxWithKey(), nil, models.RevokeWorkerInput{WorkerID: "worker1", Confirm: "edge-1"})
	if err == nil || !strings.Contains(err.Error(), "edge-01") {
		t.Fatalf("expected the error to name the hostname, got %v", err)
	}
	rec.expectNoWrites(t)

	_, _, err = revokeWorker(rec.client())(ctxWithKey(), nil, models.RevokeWorkerInput{WorkerID: "worker1", Confirm: "edge-01"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec.expectLast(t, http.MethodPost, "/api/custom/workers/worker1/revoke")
}

func TestRevokeWorkerMissingAPIKey(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if _, _, err := revokeWorker(rec.client())(t.Context(), nil, models.RevokeWorkerInput{WorkerID: "worker1"}); err == nil {
		t.Fatal("expected error when no API key on context")
	}
}

// ---------------------------------------------------------------------------
// confirm helper
// ---------------------------------------------------------------------------

func TestRequireConfirmSurfacesLookupFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	err := requireConfirm(ctxWithKey(), client.New(srv.URL), "wireops_sk_test", confirmStacks, "ghost", "whatever")
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected a lookup error naming the record, got %v", err)
	}
}

func TestMatchConfirmRejectsAnEmptyExpectedValue(t *testing.T) {
	// Without this, an omitted confirm ("") would equal the expected value and
	// every caller that resolves its target by something other than a name
	// would silently lose the guard.
	for _, got := range []string{"", "anything"} {
		if err := matchConfirm("", got); err == nil {
			t.Fatalf("expected confirm %q against an empty expected value to fail", got)
		}
	}
}

func TestRequireConfirmRejectsUnknownCollection(t *testing.T) {
	rec := newRecorder(nil)
	defer rec.close()

	if err := requireConfirm(ctxWithKey(), rec.client(), "wireops_sk_test", "integrations", "x", "x"); err == nil {
		t.Fatal("expected an unregistered collection to fail")
	}
}
