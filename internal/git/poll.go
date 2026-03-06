package git

import (
	"fmt"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

func RemoteHeadSHA(repo *gogit.Repository, branch string, auth transport.AuthMethod) (string, error) {
	remote, err := repo.Remote("origin")
	if err != nil {
		return "", fmt.Errorf("failed to get remote: %w", err)
	}

	refs, err := remote.List(&gogit.ListOptions{Auth: auth})
	if err != nil {
		return "", fmt.Errorf("failed to list remote refs: %w", err)
	}

	branchRef := plumbing.NewBranchReferenceName(branch)
	for _, ref := range refs {
		if ref.Name() == branchRef {
			return ref.Hash().String(), nil
		}
	}

	return "", fmt.Errorf("branch %q not found on remote", branch)
}

func HasChanged(remoteSHA, localSHA string) bool {
	return remoteSHA != localSHA
}

func LocalHeadSHA(repo *gogit.Repository) (string, error) {
	ref, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}
	return ref.Hash().String(), nil
}
