package app

import (
	"testing"
)

func TestParseTags(t *testing.T) {
	got := parseTags(" edge, gpu ,,prod ")
	if len(got) != 3 {
		t.Fatalf("expected 3 tags, got %d (%v)", len(got), got)
	}
	if got[0] != "edge" || got[1] != "gpu" || got[2] != "prod" {
		t.Fatalf("unexpected tags: %v", got)
	}
}
