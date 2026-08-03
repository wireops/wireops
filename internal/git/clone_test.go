package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCloneOrFetchContextHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := CloneOrFetchContext(ctx, "repo-1", "https://example.com/repo.git", "main", nil, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestCloneOrFetchContextRejectsInvalidRepoIDs(t *testing.T) {
	// Each of these must be rejected before any filesystem work happens —
	// notably "" and ".", which clean to the workspace root itself.
	for _, repoID := range []string{"", ".", "..", "../escape", "sub/dir", "/abs/path", "a/../b", "trailing/", "./repo"} {
		t.Run("repoID="+repoID, func(t *testing.T) {
			_, err := CloneOrFetchContext(context.Background(), repoID, "https://example.com/repo.git", "main", nil, t.TempDir())
			if err == nil {
				t.Fatalf("expected error for repo ID %q, got nil", repoID)
			}
			if !strings.Contains(err.Error(), "invalid repository") {
				t.Fatalf("expected invalid repository error for %q, got: %v", repoID, err)
			}
		})
	}
}
