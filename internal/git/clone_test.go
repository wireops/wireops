package git

import (
	"context"
	"errors"
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
