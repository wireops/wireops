package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/secrets"
	"github.com/wireops/wireops/internal/testutil"
)

// newRollbackGuardrailTestApp creates the minimal "stacks" collection needed
// to exercise resolveComposeFile's existence check in isolation, mirroring
// what RollbackStack relies on before it ever loads SOPS secrets or dispatches
// a deploy.
func newRollbackGuardrailTestApp(t *testing.T) (*tests.TestApp, *core.Record) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	stacks.Fields.Add(&core.TextField{Name: "compose_file"})
	stacks.Fields.Add(&core.SelectField{Name: "status", Values: []string{"active", "syncing", "paused", "error", "pending"}})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("failed to create stacks collection: %v", err)
	}

	stack := core.NewRecord(stacks)
	stack.Set("name", "stack")
	stack.Set("status", "active")
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to create stack: %v", err)
	}
	return app, stack
}

// TestResolveComposeFileMissingAtCommitBlocksRollback guards against
// rollback-to-a-commit-that-never-had-a-compose-file: RollbackStack calls
// resolveComposeFile right after the hard git reset, before any SOPS
// decryption or worker dispatch happens, so a missing compose file at that
// revision must hard-fail rollback rather than silently deploying stale
// state.
func TestResolveComposeFileMissingAtCommitBlocksRollback(t *testing.T) {
	app, stack := newRollbackGuardrailTestApp(t)
	r := &Reconciler{app: app}

	// workDir simulates the repo tree *after* `git reset --hard <commit>` —
	// empty here, i.e. the commit being rolled back to never had a compose file.
	workDir := t.TempDir()

	_, err := r.resolveComposeFile(stack, workDir, stack.Id, "manual", "deadbeef")
	if err == nil {
		t.Fatal("expected error when compose file is missing at the target commit, got nil")
	}

	reloaded, findErr := app.FindRecordById("stacks", stack.Id)
	if findErr != nil {
		t.Fatalf("failed to reload stack: %v", findErr)
	}
	if reloaded.GetString("status") != "error" {
		t.Errorf("expected stack status=error after blocked rollback, got %q", reloaded.GetString("status"))
	}
}

// TestResolveComposeFileFoundAtCommitAllowsRollback is the inverse: when the
// compose file does exist at workDir, resolution succeeds and the stack is
// left untouched.
func TestResolveComposeFileFoundAtCommitAllowsRollback(t *testing.T) {
	app, stack := newRollbackGuardrailTestApp(t)
	r := &Reconciler{app: app}

	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	composeFile, err := r.resolveComposeFile(stack, workDir, stack.Id, "manual", "deadbeef")
	if err != nil {
		t.Fatalf("expected resolveComposeFile to succeed, got: %v", err)
	}
	if composeFile != "docker-compose.yml" {
		t.Errorf("expected docker-compose.yml, got %q", composeFile)
	}
}

// TestRollbackReconcilesSopsSecretsForTargetCommitVersion drives the same
// git-reset + loadSopsEnv sequence RollbackStack performs, across two real
// commits each carrying their own secrets.yaml encrypted for the repo's age
// key. It proves that rolling back to an older commit decrypts *that
// commit's* SOPS secrets, not whatever is newest — the same version being
// rolled back to must be reconciled together with its secrets.
func TestRollbackReconcilesSopsSecretsForTargetCommitVersion(t *testing.T) {
	t.Setenv("SECRET_KEY", sopsTestSecretKey)

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	privateKey, publicKey, err := secrets.GenerateAgeKeypair()
	if err != nil {
		t.Fatalf("GenerateAgeKeypair: %v", err)
	}
	encryptedKey, err := crypto.Encrypt([]byte(privateKey), []byte(sopsTestSecretKey))
	if err != nil {
		t.Fatalf("crypto.Encrypt: %v", err)
	}
	repo := newSopsTestRepo(t, app, encryptedKey, publicKey)

	repoDir := t.TempDir()
	gitRepo, err := gogit.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := gitRepo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	commitSecrets := func(value string) string {
		encrypted := testutil.EncryptForAge(t, publicKey, []byte("DB_PASS: "+value+"\n"))
		if err := os.WriteFile(filepath.Join(repoDir, "secrets.yaml"), encrypted, 0o644); err != nil {
			t.Fatalf("write secrets.yaml: %v", err)
		}
		if _, err := wt.Add("secrets.yaml"); err != nil {
			t.Fatalf("git add: %v", err)
		}
		hash, err := wt.Commit("secrets v"+value, &gogit.CommitOptions{
			Author: &object.Signature{Name: "test", Email: "test@example.com"},
		})
		if err != nil {
			t.Fatalf("git commit: %v", err)
		}
		return hash.String()
	}

	commitA := commitSecrets("v1-secret")
	commitB := commitSecrets("v2-secret")

	r := &Reconciler{app: app}
	ctx := context.Background()

	// Roll back to commit A: hard-reset the worktree then decrypt secrets
	// from that exact checkout, exactly as RollbackStack does.
	if err := wt.Reset(&gogit.ResetOptions{Commit: mustParseHash(commitA), Mode: gogit.HardReset}); err != nil {
		t.Fatalf("git reset to commitA: %v", err)
	}
	valuesAtA, err := r.loadSopsEnv(ctx, repo, repoDir)
	if err != nil {
		t.Fatalf("loadSopsEnv at commitA: %v", err)
	}
	if valuesAtA["DB_PASS"] != "v1-secret" {
		t.Errorf("expected DB_PASS=v1-secret when rolled back to commitA, got %#v", valuesAtA)
	}

	// Sanity check: without the reset, HEAD (commit B) has a different secret,
	// proving the decrypted value really does track the checked-out commit.
	if err := wt.Reset(&gogit.ResetOptions{Commit: mustParseHash(commitB), Mode: gogit.HardReset}); err != nil {
		t.Fatalf("git reset to commitB: %v", err)
	}
	valuesAtB, err := r.loadSopsEnv(ctx, repo, repoDir)
	if err != nil {
		t.Fatalf("loadSopsEnv at commitB: %v", err)
	}
	if valuesAtB["DB_PASS"] != "v2-secret" {
		t.Errorf("expected DB_PASS=v2-secret at commitB, got %#v", valuesAtB)
	}
}
