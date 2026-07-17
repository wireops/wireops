package sync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// newSopsForceRedeployTestApp mirrors newForceRedeployNilNotifierTestApp but
// adds sops_age_key to repositories and uses a valid compose_path, so
// ForceRedeployStack runs past render setup and reaches loadSopsEnv instead
// of failing earlier on an invalid compose_path.
func newSopsForceRedeployTestApp(t *testing.T) (*tests.TestApp, *core.Record, string) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	repos := core.NewBaseCollection("repositories")
	repos.Fields.Add(&core.TextField{Name: "name"})
	repos.Fields.Add(&core.TextField{Name: "last_commit_sha"})
	repos.Fields.Add(&core.TextField{Name: "sops_age_key"})
	if err := app.Save(repos); err != nil {
		t.Fatalf("failed to create repositories collection: %v", err)
	}

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name", Required: true})
	stacks.Fields.Add(&core.RelationField{Name: "repository", CollectionId: repos.Id, Required: true, MaxSelect: 1})
	stacks.Fields.Add(&core.TextField{Name: "compose_path"})
	stacks.Fields.Add(&core.SelectField{Name: "status", Values: []string{"active", "syncing", "paused", "error", "pending"}})
	stacks.Fields.Add(&core.NumberField{Name: "deployed_version"})
	stacks.Fields.Add(&core.TextField{Name: "deployed_commit"})
	stacks.Fields.Add(&core.TextField{Name: "deployed_checksum"})
	stacks.Fields.Add(&core.DateField{Name: "deployed_at"})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("failed to create stacks collection: %v", err)
	}

	syncLogs := core.NewBaseCollection("sync_logs")
	syncLogs.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, Required: true, MaxSelect: 1})
	syncLogs.Fields.Add(&core.SelectField{Name: "trigger", Values: []string{"cron", "webhook", "manual", "redeploy", "rollback", "transfer", "queue"}})
	syncLogs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"running", "success", "error", "queued", "noop"}})
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

	repo := core.NewRecord(repos)
	repo.Set("name", "repo")
	repo.Set("last_commit_sha", "abc123")
	// sops_age_key deliberately left unset — a secrets.yaml with no
	// configured age key is loadSopsEnv's decrypt-failure path.
	if err := app.Save(repo); err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}

	stack := core.NewRecord(stacks)
	stack.Set("name", "stack")
	stack.Set("repository", repo.Id)
	stack.Set("compose_path", ".")
	stack.Set("status", "active")
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to create stack: %v", err)
	}

	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)
	repoDir := filepath.Join(workspace, repo.Id)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("failed to create repo workdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "secrets.yaml"), []byte("DB_PASS: ENC[...]\n"), 0o644); err != nil {
		t.Fatalf("failed to write secrets.yaml: %v", err)
	}

	return app, stack, repoDir
}

// TestForceRedeployStackSopsDecryptFailureBlocksDeploy proves a
// secrets.yaml present without a configured repository age key fails
// loadSopsEnv and blocks ForceRedeployStack before any worker dispatch,
// leaving the stack and sync log in an error state — mirroring the
// commit-aware SOPS coverage in rollback_sops_test.go, applied to the
// redeploy flow.
func TestForceRedeployStackSopsDecryptFailureBlocksDeploy(t *testing.T) {
	app, stack, _ := newSopsForceRedeployTestApp(t)
	r := &Reconciler{app: app}

	err := r.ForceRedeployStack(context.Background(), stack.Id, false, false, false)
	if err == nil {
		t.Fatal("ForceRedeployStack succeeded, want SOPS decrypt failure")
	}
	if !strings.Contains(err.Error(), "failed to decrypt SOPS secrets file") {
		t.Fatalf("ForceRedeployStack error = %q, want SOPS decrypt failure", err)
	}
	if !strings.Contains(err.Error(), "no SOPS age key configured") {
		t.Fatalf("ForceRedeployStack error = %q, want missing age key detail", err)
	}

	logs, err := app.FindAllRecords("sync_logs", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		t.Fatalf("failed to query sync logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("sync logs = %d, want 1", len(logs))
	}
	if got := logs[0].GetString("status"); got != "error" {
		t.Fatalf("sync log status = %q, want error", got)
	}

	reloaded, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("failed to reload stack: %v", err)
	}
	if got := reloaded.GetString("status"); got != "error" {
		t.Fatalf("stack status = %q, want error", got)
	}
}

// newSopsTransferTestApp builds the fixtures TransferStack needs to reach
// its SOPS overlay step: a stack with a rendered compose revision on disk,
// assigned to a source worker, and a distinct target worker to transfer to.
func newSopsTransferTestApp(t *testing.T) (*tests.TestApp, *Reconciler, *core.Record, *core.Record) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	repos := core.NewBaseCollection("repositories")
	repos.Fields.Add(&core.TextField{Name: "name"})
	repos.Fields.Add(&core.TextField{Name: "sops_age_key"})
	if err := app.Save(repos); err != nil {
		t.Fatalf("failed to create repositories collection: %v", err)
	}

	workers := core.NewBaseCollection("workers")
	workers.Fields.Add(&core.TextField{Name: "hostname"})
	if err := app.Save(workers); err != nil {
		t.Fatalf("failed to create workers collection: %v", err)
	}

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name", Required: true})
	stacks.Fields.Add(&core.RelationField{Name: "repository", CollectionId: repos.Id, MaxSelect: 1})
	stacks.Fields.Add(&core.RelationField{Name: "worker", CollectionId: workers.Id, MaxSelect: 1})
	stacks.Fields.Add(&core.TextField{Name: "compose_path"})
	stacks.Fields.Add(&core.NumberField{Name: "current_version"})
	stacks.Fields.Add(&core.SelectField{Name: "status", Values: []string{"active", "syncing", "paused", "error", "pending"}})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("failed to create stacks collection: %v", err)
	}

	syncLogs := core.NewBaseCollection("sync_logs")
	syncLogs.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, Required: true, MaxSelect: 1})
	syncLogs.Fields.Add(&core.SelectField{Name: "trigger", Values: []string{"cron", "webhook", "manual", "redeploy", "rollback", "transfer", "queue"}})
	syncLogs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"running", "success", "error", "queued", "noop"}})
	syncLogs.Fields.Add(&core.TextField{Name: "commit_sha"})
	syncLogs.Fields.Add(&core.TextField{Name: "commit_message"})
	syncLogs.Fields.Add(&core.TextField{Name: "output"})
	syncLogs.Fields.Add(&core.NumberField{Name: "duration_ms"})
	if err := app.Save(syncLogs); err != nil {
		t.Fatalf("failed to create sync_logs collection: %v", err)
	}

	repo := core.NewRecord(repos)
	repo.Set("name", "repo")
	// sops_age_key left unset on purpose — the decrypt-failure path.
	if err := app.Save(repo); err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}

	sourceWorker := core.NewRecord(workers)
	sourceWorker.Set("hostname", "source-host")
	if err := app.Save(sourceWorker); err != nil {
		t.Fatalf("failed to create source worker: %v", err)
	}
	targetWorker := core.NewRecord(workers)
	targetWorker.Set("hostname", "target-host")
	if err := app.Save(targetWorker); err != nil {
		t.Fatalf("failed to create target worker: %v", err)
	}

	stack := core.NewRecord(stacks)
	stack.Set("name", "stack")
	stack.Set("repository", repo.Id)
	stack.Set("worker", sourceWorker.Id)
	stack.Set("compose_path", ".")
	stack.Set("current_version", 1)
	stack.Set("status", "active")
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to create stack: %v", err)
	}

	storage := t.TempDir()
	t.Setenv("STACKS_STORAGE_PATH", storage)
	r := &Reconciler{app: app, renderer: NewRenderer(app)}
	revisionPath := r.renderer.GetRevisionFilePath(stack.Id, 1)
	if err := os.MkdirAll(filepath.Dir(revisionPath), 0o755); err != nil {
		t.Fatalf("failed to create revision dir: %v", err)
	}
	if err := os.WriteFile(revisionPath, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("failed to write rendered compose: %v", err)
	}

	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)
	repoDir := filepath.Join(workspace, repo.Id)
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("failed to create repo workdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "secrets.yaml"), []byte("DB_PASS: ENC[...]\n"), 0o644); err != nil {
		t.Fatalf("failed to write secrets.yaml: %v", err)
	}

	return app, r, stack, targetWorker
}

// TestTransferStackSopsDecryptFailureBlocksTransfer mirrors the
// ForceRedeployStack case above for TransferStack: a secrets.yaml present
// without a configured repository age key must block the transfer before
// any worker dispatch or stack/status mutation.
func TestTransferStackSopsDecryptFailureBlocksTransfer(t *testing.T) {
	app, r, stack, targetWorker := newSopsTransferTestApp(t)

	err := r.TransferStack(context.Background(), stack.Id, targetWorker.Id)
	if err == nil {
		t.Fatal("TransferStack succeeded, want SOPS decrypt failure")
	}
	if !strings.Contains(err.Error(), "failed to decrypt SOPS secrets file") {
		t.Fatalf("TransferStack error = %q, want SOPS decrypt failure", err)
	}
	if !strings.Contains(err.Error(), "no SOPS age key configured") {
		t.Fatalf("TransferStack error = %q, want missing age key detail", err)
	}

	logs, err := app.FindAllRecords("sync_logs", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		t.Fatalf("failed to query sync logs: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("sync logs = %d, want 0 (SOPS check runs before sync log creation)", len(logs))
	}

	reloaded, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("failed to reload stack: %v", err)
	}
	if got := reloaded.GetString("status"); got != "active" {
		t.Fatalf("stack status = %q, want unchanged active (SOPS check runs before status mutation)", got)
	}
}

// TestHashEnvVarsIsKeyedBySecretKey proves the checksum-folded env digest is
// an HMAC keyed by SECRET_KEY rather than a bare hash — the same env vars
// must produce different digests under different keys (so persisted
// checksums can't be brute-forced against a guessed/known secret value),
// while staying deterministic for a fixed key and still changing whenever
// the env vars themselves change (redeploy detection for env-only changes).
func TestHashEnvVarsIsKeyedBySecretKey(t *testing.T) {
	envVars := []string{"DB_PASS=hunter2", "API_KEY=abc123"}

	t.Setenv("SECRET_KEY", "12345678901234567890123456789012")
	digestA1 := hashEnvVars(envVars)
	digestA2 := hashEnvVars(envVars)
	if digestA1 != digestA2 {
		t.Fatalf("hashEnvVars is not deterministic for a fixed key: %q != %q", digestA1, digestA2)
	}

	t.Setenv("SECRET_KEY", "abcdefghijklmnopqrstuvwxyz012345")
	digestB := hashEnvVars(envVars)
	if digestA1 == digestB {
		t.Fatal("hashEnvVars produced the same digest under a different SECRET_KEY — not actually keyed")
	}

	changedEnvVars := []string{"DB_PASS=different", "API_KEY=abc123"}
	digestC := hashEnvVars(changedEnvVars)
	if digestB == digestC {
		t.Fatal("hashEnvVars did not change when an env var value changed — would break env-only redeploy detection")
	}
}
