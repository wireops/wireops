package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func TestConnectionPublicRepository(t *testing.T) {
	repoURL := createLocalTestRepository(t)

	err := TestConnection(repoURL, nil)
	if err != nil {
		t.Errorf("expected successful connection to local repository, got error: %v", err)
	}
}

func TestConnectionInvalidURL(t *testing.T) {
	invalidURL := filepath.Join(t.TempDir(), "missing-repo.git")

	err := TestConnection(invalidURL, nil)
	if err == nil {
		t.Error("expected error for invalid repository URL, got nil")
	}
}

func TestConnectionMalformedURL(t *testing.T) {
	malformedURL := "not-a-valid-url"

	err := TestConnection(malformedURL, nil)
	if err == nil {
		t.Error("expected error for malformed URL, got nil")
	}
}

func TestConnectionPrivateRepoWithoutAuth(t *testing.T) {
	invalidURL := filepath.Join(t.TempDir(), "private-repo.git")

	err := TestConnection(invalidURL, nil)
	if err == nil {
		t.Error("expected error for inaccessible repository without auth, got nil")
	}
}

func TestConnectionWithBasicAuth(t *testing.T) {
	var auth transport.AuthMethod = nil
	repoURL := createLocalTestRepository(t)

	err := TestConnection(repoURL, auth)
	if err != nil {
		t.Errorf("expected successful connection with nil auth, got error: %v", err)
	}
}

func createLocalTestRepository(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("failed to init local repository: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# local test repo\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("failed to get worktree: %v", err)
	}
	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("failed to add test file: %v", err)
	}
	if _, err := wt.Commit("initial commit", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "wireops test",
			Email: "test@wireops.local",
			When:  time.Now(),
		},
	}); err != nil {
		t.Fatalf("failed to commit test file: %v", err)
	}

	return dir
}
