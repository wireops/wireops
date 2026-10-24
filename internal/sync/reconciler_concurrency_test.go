package sync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/wireops/wireops/internal/notify"
	"github.com/wireops/wireops/internal/protocol"
)

// requireCompose skips a test that needs `docker compose config` to render a
// revision. Only the CLI is exercised, never the daemon.
func requireCompose(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker CLI unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "compose", "version").Run(); err != nil {
		t.Skip("Compose unavailable")
	}
}

// blockingDispatcher holds every deploy inside Dispatch until release is
// closed, so a test can observe how many reconciles are in the deploy phase
// at the same moment. Everything else (the post-check's status query) is
// answered immediately so a reconcile can finish once released.
type blockingDispatcher struct {
	entered     chan string // stack IDs that reached the deploy dispatch
	release     chan struct{}
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
}

func newBlockingDispatcher(capacity int) *blockingDispatcher {
	return &blockingDispatcher{
		entered: make(chan string, capacity),
		release: make(chan struct{}),
	}
}

func (d *blockingDispatcher) IsConnected(string) bool { return true }

func (d *blockingDispatcher) Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error) {
	stackID := ""
	switch c := cmd.(type) {
	case protocol.DeployCommand:
		stackID = c.StackID
	case protocol.RedeployCommand:
		stackID = c.StackID
	default:
		// Post-check status queries and friends: answer at once, with no
		// payload, which postDeployCheck treats as "could not verify".
		return protocol.CommandResult{}, nil
	}

	now := d.inFlight.Add(1)
	for {
		peak := d.maxInFlight.Load()
		if now <= peak || d.maxInFlight.CompareAndSwap(peak, now) {
			break
		}
	}
	defer d.inFlight.Add(-1)

	select {
	case d.entered <- stackID:
	default:
	}

	select {
	case <-d.release:
	case <-ctx.Done():
		return protocol.CommandResult{}, ctx.Err()
	}
	return protocol.CommandResult{Output: "deployed"}, nil
}

// newConcurrencyTestApp builds two stacks in one repository that resolve to
// the *same* working directory (both have an empty compose_path, so
// stackWorkDir returns workspace/<repoID> for both) and assigns them to the
// same worker. That is exactly the shape the old workDir mutex serialized.
func newConcurrencyTestApp(t *testing.T) (*tests.TestApp, *Reconciler, []string, string) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	repos := core.NewBaseCollection("repositories")
	repos.Fields.Add(&core.TextField{Name: "name"})
	repos.Fields.Add(&core.TextField{Name: "git_url"})
	repos.Fields.Add(&core.TextField{Name: "branch"})
	repos.Fields.Add(&core.TextField{Name: "last_commit_sha"})
	repos.Fields.Add(&core.DateField{Name: "last_fetched_at"})
	repos.Fields.Add(&core.TextField{Name: "sops_age_key"})
	repos.Fields.Add(&core.SelectField{Name: "status", Values: []string{"connected", "error"}})
	if err := app.Save(repos); err != nil {
		t.Fatalf("failed to create repositories collection: %v", err)
	}

	// The renderer refuses to render without a worker policy to check
	// against, so the (empty, disabled) policy collection has to exist.
	policies := core.NewBaseCollection("worker_policies")
	policies.Fields.Add(&core.BoolField{Name: "enabled"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_volumes"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_networks"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_images"})
	policies.Fields.Add(&core.BoolField{Name: "prevent_latest_images"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_volumes"})
	policies.Fields.Add(&core.BoolField{Name: "block_privileged"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_network"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_pid"})
	policies.Fields.Add(&core.BoolField{Name: "block_host_ipc"})
	policies.Fields.Add(&core.BoolField{Name: "block_docker_socket"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_cap_add"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_devices"})
	policies.Fields.Add(&core.JSONField{Name: "allowed_security_opt"})
	policies.Fields.Add(&core.BoolField{Name: "allow_render_overrides"})
	if err := app.Save(policies); err != nil {
		t.Fatalf("failed to create worker_policies collection: %v", err)
	}

	workers := core.NewBaseCollection("workers")
	workers.Fields.Add(&core.TextField{Name: "hostname"})
	workers.Fields.Add(&core.TextField{Name: "fingerprint"})
	workers.Fields.Add(&core.JSONField{Name: "capabilities"})
	workers.Fields.Add(&core.BoolField{Name: "policy_inherit"})
	workers.Fields.Add(&core.JSONField{Name: "policy_flags"})
	workers.Fields.Add(&core.JSONField{Name: "policy_volumes"})
	workers.Fields.Add(&core.JSONField{Name: "policy_networks"})
	workers.Fields.Add(&core.JSONField{Name: "policy_images"})
	workers.Fields.Add(&core.JSONField{Name: "policy_cap_add"})
	workers.Fields.Add(&core.JSONField{Name: "policy_devices"})
	workers.Fields.Add(&core.JSONField{Name: "policy_security_opt"})
	if err := app.Save(workers); err != nil {
		t.Fatalf("failed to create workers collection: %v", err)
	}

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name", Required: true})
	stacks.Fields.Add(&core.RelationField{Name: "repository", CollectionId: repos.Id, MaxSelect: 1})
	stacks.Fields.Add(&core.RelationField{Name: "worker", CollectionId: workers.Id, MaxSelect: 1})
	stacks.Fields.Add(&core.TextField{Name: "compose_path"})
	stacks.Fields.Add(&core.TextField{Name: "compose_file"})
	stacks.Fields.Add(&core.TextField{Name: "compose_project_name"})
	stacks.Fields.Add(&core.SelectField{Name: "status", Values: []string{"active", "syncing", "paused", "error", "pending"}})
	stacks.Fields.Add(&core.NumberField{Name: "current_version"})
	stacks.Fields.Add(&core.NumberField{Name: "deployed_version"})
	stacks.Fields.Add(&core.TextField{Name: "deployed_commit"})
	stacks.Fields.Add(&core.TextField{Name: "deployed_checksum"})
	stacks.Fields.Add(&core.DateField{Name: "deployed_at"})
	stacks.Fields.Add(&core.TextField{Name: "last_synced_at"})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("failed to create stacks collection: %v", err)
	}

	revisions := core.NewBaseCollection("stack_revisions")
	revisions.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, MaxSelect: 1})
	revisions.Fields.Add(&core.NumberField{Name: "version"})
	revisions.Fields.Add(&core.TextField{Name: "commit_sha"})
	revisions.Fields.Add(&core.TextField{Name: "checksum"})
	revisions.Fields.Add(&core.TextField{Name: "compose_path"})
	if err := app.Save(revisions); err != nil {
		t.Fatalf("failed to create stack_revisions collection: %v", err)
	}

	syncLogs := core.NewBaseCollection("sync_logs")
	syncLogs.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, Required: true, MaxSelect: 1})
	syncLogs.Fields.Add(&core.SelectField{Name: "trigger", Values: []string{"cron", "webhook", "manual", "redeploy", "rollback", "transfer", "queue"}})
	syncLogs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"running", "success", "error", "queued", "noop", "degraded"}})
	syncLogs.Fields.Add(&core.TextField{Name: "commit_sha"})
	syncLogs.Fields.Add(&core.TextField{Name: "commit_message"})
	syncLogs.Fields.Add(&core.TextField{Name: "output"})
	syncLogs.Fields.Add(&core.NumberField{Name: "duration_ms"})
	if err := app.Save(syncLogs); err != nil {
		t.Fatalf("failed to create sync_logs collection: %v", err)
	}

	phases := core.NewBaseCollection("sync_log_phases")
	phases.Fields.Add(&core.RelationField{Name: "sync_log", CollectionId: syncLogs.Id, Required: true, MaxSelect: 1})
	phases.Fields.Add(&core.SelectField{Name: "phase", Required: true, MaxSelect: 1, Values: deployPhases})
	phases.Fields.Add(&core.SelectField{Name: "status", Required: true, MaxSelect: 1, Values: []string{"running", "success", "error", "skipped"}})
	phases.Fields.Add(&core.DateField{Name: "started_at", Required: true})
	phases.Fields.Add(&core.NumberField{Name: "duration_ms"})
	phases.Fields.Add(&core.TextField{Name: "detail"})
	phases.Fields.Add(&core.NumberField{Name: "seq"})
	if err := app.Save(phases); err != nil {
		t.Fatalf("failed to create sync_log_phases collection: %v", err)
	}

	worker := core.NewRecord(workers)
	worker.Set("hostname", "worker-1")
	worker.Set("fingerprint", "fp-1")
	if err := app.Save(worker); err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	repo := core.NewRecord(repos)
	repo.Set("name", "repo")
	repo.Set("git_url", "https://example.com/repo.git")
	repo.Set("branch", "main")
	repo.Set("status", "connected")
	repo.Set("last_fetched_at", time.Now().UTC().Format(time.RFC3339))
	if err := app.Save(repo); err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}

	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)
	t.Setenv("STACKS_STORAGE_PATH", t.TempDir())
	repoDir := filepath.Join(workspace, repo.Id)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("failed to create repo workdir: %v", err)
	}
	sha := localGitRepo(t, repoDir)
	repo.Set("last_commit_sha", sha)
	if err := app.Save(repo); err != nil {
		t.Fatalf("failed to persist repo SHA: %v", err)
	}
	// Both stacks render from this one file, in the one directory they share.
	if err := os.WriteFile(filepath.Join(repoDir, "docker-compose.yml"), []byte("services:\n  web:\n    image: nginx:alpine\n"), 0o644); err != nil {
		t.Fatalf("failed to write compose file: %v", err)
	}

	stackIDs := make([]string, 0, 2)
	for _, name := range []string{"alpha", "beta"} {
		stack := core.NewRecord(stacks)
		stack.Set("name", name)
		stack.Set("repository", repo.Id)
		stack.Set("worker", worker.Id)
		stack.Set("compose_path", "")
		stack.Set("compose_file", "docker-compose.yml")
		stack.Set("status", "active")
		if err := app.Save(stack); err != nil {
			t.Fatalf("failed to create stack %s: %v", name, err)
		}
		stackIDs = append(stackIDs, stack.Id)
	}

	r := &Reconciler{app: app, notifier: notify.New(app), renderer: NewRenderer(app)}
	return app, r, stackIDs, repo.Id
}

// TestStacksSharingWorkDirDeployConcurrently is the regression test for the
// removed workDir mutex: two stacks whose compose files live in the same
// checkout directory must be able to sit in the deploy phase at once.
func TestStacksSharingWorkDirDeployConcurrently(t *testing.T) {
	requireCompose(t)
	app, r, stackIDs, _ := newConcurrencyTestApp(t)
	_ = app

	dispatcher := newBlockingDispatcher(len(stackIDs))
	r.dispatcher = dispatcher
	postCheckInterval = time.Millisecond
	t.Cleanup(func() { postCheckInterval = 3 * time.Second })

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	done := make(chan error, len(stackIDs))
	for _, stackID := range stackIDs {
		go func() { done <- r.ReconcileStack(ctx, stackID, "cron", 0) }()
	}

	for i := range stackIDs {
		select {
		case <-dispatcher.entered:
		case <-time.After(60 * time.Second):
			t.Fatalf("only %d of %d stacks reached the deploy phase: they are still serialized", i, len(stackIDs))
		}
	}
	if got := dispatcher.maxInFlight.Load(); got != int32(len(stackIDs)) {
		t.Fatalf("peak concurrent deploys = %d, want %d", got, len(stackIDs))
	}

	close(dispatcher.release)
	for range stackIDs {
		if err := <-done; err != nil {
			t.Fatalf("ReconcileStack failed: %v", err)
		}
	}
}

// TestReconcileReleasesRepoReadLockBeforeDispatch proves the repository read
// lock is scoped to the checkout reads, not to the whole reconcile: a fetch
// (which needs the write lock) must not have to wait for a deploy to finish.
func TestReconcileReleasesRepoReadLockBeforeDispatch(t *testing.T) {
	requireCompose(t)
	_, r, stackIDs, repoID := newConcurrencyTestApp(t)

	dispatcher := newBlockingDispatcher(1)
	r.dispatcher = dispatcher
	postCheckInterval = time.Millisecond
	t.Cleanup(func() { postCheckInterval = 3 * time.Second })

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.ReconcileStack(ctx, stackIDs[0], "cron", 0) }()

	select {
	case <-dispatcher.entered:
	case <-time.After(60 * time.Second):
		t.Fatal("reconcile never reached the deploy phase")
	}

	if !r.repoMutex(repoID).TryLock() {
		t.Fatal("repository write lock is still held during dispatch: the read lock outlives the checkout reads")
	}
	r.repoMutex(repoID).Unlock()

	close(dispatcher.release)
	if err := <-done; err != nil {
		t.Fatalf("ReconcileStack failed: %v", err)
	}
}

// TestReconcileUnchangedComposeSkipReleasesRepoLockOnce covers the early
// release colliding with the deferred one on the "compose unchanged" path:
// sync.OnceFunc must make the second call a no-op rather than panicking on
// an unlocked RWMutex.
func TestReconcileUnchangedComposeSkipReleasesRepoLockOnce(t *testing.T) {
	requireCompose(t)
	_, r, stackIDs, repoID := newConcurrencyTestApp(t)

	dispatcher := newBlockingDispatcher(1)
	close(dispatcher.release) // never block: both runs go straight through
	r.dispatcher = dispatcher
	postCheckInterval = time.Millisecond
	t.Cleanup(func() { postCheckInterval = 3 * time.Second })

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := r.ReconcileStack(ctx, stackIDs[0], "cron", 0); err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}
	// Second run renders identical content -> the unchanged-compose skip.
	// Still a cron trigger: a manual one would force a real git fetch
	// against the fixture's fake remote.
	if err := r.ReconcileStack(ctx, stackIDs[0], "cron", 0); err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}

	if !r.repoMutex(repoID).TryLock() {
		t.Fatal("repository lock left held after the unchanged-compose skip")
	}
	r.repoMutex(repoID).Unlock()
}

// TestLockDeployProjectKeysOnWorkerAndProject documents what replaced the
// workDir mutex: a lock on the Compose project a deploy touches, which is
// the previous project while an identity migration tears it down.
func TestLockDeployProjectKeysOnWorkerAndProject(t *testing.T) {
	r := &Reconciler{}

	if a, b := r.projectMutex("w1", "proj"), r.projectMutex("w1", "proj"); a != b {
		t.Fatal("projectMutex returned different instances for the same worker/project")
	}
	if a, b := r.projectMutex("w1", "proj"), r.projectMutex("w2", "proj"); a == b {
		t.Fatal("projectMutex shared one instance across different workers")
	}

	release := r.lockDeployProject("w1", &RenderResult{ProjectName: "new", PreviousProjectName: "old"})
	if r.projectMutex("w1", "old").TryLock() {
		t.Fatal("a migrating deploy must hold the previous project's lock")
	}
	if !r.projectMutex("w1", "new").TryLock() {
		t.Fatal("the target project should be free while the previous one is held")
	}
	r.projectMutex("w1", "new").Unlock()
	release()

	if !r.projectMutex("w1", "old").TryLock() {
		t.Fatal("release did not unlock the previous project")
	}
	r.projectMutex("w1", "old").Unlock()

	// No render result: nothing to lock, and the release must still be safe.
	r.lockDeployProject("w1", nil)()
}

// deployPhasesContainsRender guards the fixture above: the phase select
// values come from the production list, so a renamed phase fails loudly here
// instead of silently breaking the sync_log_phases writes in these tests.
func TestConcurrencyFixtureUsesRealPhaseList(t *testing.T) {
	if !strings.Contains(strings.Join(deployPhases, ","), "render") {
		t.Fatalf("deployPhases = %v, want it to include the render phase", deployPhases)
	}
}
