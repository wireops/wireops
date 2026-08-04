package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// newRepoFetchTestApp creates the minimal "repositories" collection needed to
// exercise FetchRepo/ensureRepoFetched in isolation.
func newRepoFetchTestApp(t *testing.T) (*tests.TestApp, *core.Collection) {
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
	repos.Fields.Add(&core.SelectField{Name: "status", Values: []string{"connected", "error"}})
	if err := app.Save(repos); err != nil {
		t.Fatalf("failed to create repositories collection: %v", err)
	}
	return app, repos
}

// localGitRepo initializes a real git repo directly at dir (mirroring
// rollback_sops_test.go's pattern) and returns the HEAD commit SHA, so tests
// can simulate a repo the repo-ticker has "already fetched" without touching
// the network.
func localGitRepo(t *testing.T, dir string) string {
	t.Helper()
	gitRepo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := gitRepo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	if _, err := wt.Add("docker-compose.yml"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	hash, err := wt.Commit("initial", &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com"},
	})
	if err != nil {
		t.Fatalf("git commit: %v", err)
	}
	return hash.String()
}

func TestRepoMutexReturnsSameInstanceForSameID(t *testing.T) {
	r := &Reconciler{}
	a := r.repoMutex("repo-1")
	b := r.repoMutex("repo-1")
	if a != b {
		t.Fatal("repoMutex returned different instances for the same repo ID")
	}
	c := r.repoMutex("repo-2")
	if a == c {
		t.Fatal("repoMutex returned the same instance for different repo IDs")
	}
}

func TestFetchRepoCronSkipsWhenAlreadyLocked(t *testing.T) {
	r := &Reconciler{}
	repoID := "busy-repo"

	// Simulate a fetch already in flight (e.g. the repo ticker mid-clone).
	mu := r.repoMutex(repoID)
	mu.Lock()
	defer mu.Unlock()

	changed, sha, err := r.FetchRepo(context.Background(), repoID, "cron")
	if err != nil {
		t.Fatalf("FetchRepo returned error, want nil (should skip quietly): %v", err)
	}
	if changed {
		t.Fatal("FetchRepo reported changed=true while skipping a locked repo")
	}
	if sha != "" {
		t.Fatalf("FetchRepo returned sha=%q, want empty when skipping", sha)
	}
}

func TestFetchRepoReturnsErrorWhenRepositoryNotFound(t *testing.T) {
	app, _ := newRepoFetchTestApp(t)
	r := &Reconciler{app: app}

	_, _, err := r.FetchRepo(context.Background(), "does-not-exist", "manual")
	if err == nil {
		t.Fatal("FetchRepo succeeded, want error for missing repository record")
	}
}

func TestEnsureRepoFetchedCronSkipsFetchWhenAlreadyFresh(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)

	app, repos := newRepoFetchTestApp(t)
	repo := core.NewRecord(repos)
	repo.Set("name", "repo")
	// Deliberately invalid: if ensureRepoFetched actually attempted a fetch
	// here, it would try to clone/fetch this URL and fail.
	repo.Set("git_url", "not-a-real-remote")
	if err := app.Save(repo); err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}

	repoDir := filepath.Join(workspace, repo.Id)
	commitSHA := localGitRepo(t, repoDir)

	repo.Set("last_commit_sha", commitSHA)
	repo.Set("last_fetched_at", time.Now().UTC().Format(time.RFC3339))
	repo.Set("status", "connected")
	if err := app.Save(repo); err != nil {
		t.Fatalf("failed to update repository: %v", err)
	}

	r := &Reconciler{app: app}
	sha, gitRepo, err := r.ensureRepoFetched(context.Background(), repo, "cron")
	if err != nil {
		t.Fatalf("ensureRepoFetched returned error, want nil (should read cached state): %v", err)
	}
	if sha != commitSHA {
		t.Fatalf("ensureRepoFetched sha = %q, want %q (repo's already-fetched commit)", sha, commitSHA)
	}
	if gitRepo == nil {
		t.Fatal("ensureRepoFetched returned nil *gogit.Repository")
	}
}

func TestEnsureRepoFetchedManualTriggerForcesFetch(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)

	app, repos := newRepoFetchTestApp(t)
	repo := core.NewRecord(repos)
	repo.Set("name", "repo")
	repo.Set("git_url", "not-a-real-remote")
	if err := app.Save(repo); err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}

	repoDir := filepath.Join(workspace, repo.Id)
	commitSHA := localGitRepo(t, repoDir)
	repo.Set("last_commit_sha", commitSHA)
	repo.Set("last_fetched_at", time.Now().UTC().Format(time.RFC3339))
	if err := app.Save(repo); err != nil {
		t.Fatalf("failed to update repository: %v", err)
	}

	r := &Reconciler{app: app}
	// Non-cron triggers must always force a fresh fetch, even when the repo
	// looks already up to date - a bad git_url here must surface as an error
	// rather than silently falling back to the cached commit.
	_, _, err := r.ensureRepoFetched(context.Background(), repo, "manual")
	if err == nil {
		t.Fatal("ensureRepoFetched succeeded, want error: manual trigger should force a fetch against the (invalid) git_url")
	}
}

func TestEnsureRepoFetchedCronBootstrapsWhenNeverFetched(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)

	app, repos := newRepoFetchTestApp(t)
	repo := core.NewRecord(repos)
	repo.Set("name", "repo")
	repo.Set("git_url", "not-a-real-remote")
	// last_fetched_at left empty and no repoDir on disk: this repo has never
	// been fetched by its own ticker yet.
	if err := app.Save(repo); err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}

	r := &Reconciler{app: app}
	_, _, err := r.ensureRepoFetched(context.Background(), repo, "cron")
	if err == nil {
		t.Fatal("ensureRepoFetched succeeded, want error: a never-fetched repo must attempt a bootstrap fetch instead of stalling forever")
	}
}
