package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func CloneOrFetch(repoID, gitURL, branch string, auth transport.AuthMethod, workspace string) (*gogit.Repository, error) {
	cleaned := filepath.Clean(repoID)
	if filepath.IsAbs(cleaned) || strings.Contains(repoID, "..") || strings.Contains(repoID, string(os.PathSeparator)) {
		return nil, fmt.Errorf("invalid repository ID: %s", repoID)
	}

	repoDir := filepath.Join(workspace, cleaned)
	if rel, err := filepath.Rel(workspace, repoDir); err != nil || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("invalid repository path traversal: %s", repoID)
	}

	if _, err := os.Stat(filepath.Join(repoDir, ".git")); os.IsNotExist(err) {
		return cloneRepo(repoDir, gitURL, branch, auth)
	}

	return fetchRepo(repoDir, branch, auth)
}

func cloneRepo(dir, gitURL, branch string, auth transport.AuthMethod) (*gogit.Repository, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create repo dir: %w", err)
	}

	opts := &gogit.CloneOptions{
		URL:           gitURL,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		SingleBranch:  true,
		Depth:         0,
		Auth:          auth,
	}

	repo, err := gogit.PlainClone(dir, false, opts)
	if err != nil {
		return nil, fmt.Errorf("git clone failed: %w", err)
	}

	return repo, nil
}

func fetchRepo(dir, branch string, auth transport.AuthMethod) (*gogit.Repository, error) {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open repo: %w", err)
	}

	fetchOpts := &gogit.FetchOptions{
		Auth:  auth,
		Force: true,
	}

	err = repo.Fetch(fetchOpts)
	if err != nil && err != gogit.NoErrAlreadyUpToDate {
		return nil, fmt.Errorf("git fetch failed: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	resetOpts := &gogit.ResetOptions{
		Commit: plumbing.ZeroHash,
		Mode:   gogit.HardReset,
	}

	remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote ref: %w", err)
	}
	resetOpts.Commit = remoteRef.Hash()

	if err := wt.Reset(resetOpts); err != nil {
		return nil, fmt.Errorf("git reset failed: %w", err)
	}

	return repo, nil
}
