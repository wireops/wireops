package app

import (
	"testing"
)

func TestParseTags(t *testing.T) {
	got, err := parseTags(" edge, gpu ,,prod ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 tags, got %d (%v)", len(got), got)
	}
	if got[0] != "edge" || got[1] != "gpu" || got[2] != "prod" {
		t.Fatalf("unexpected tags: %v", got)
	}
}

func TestParseTagsEmptyInput(t *testing.T) {
	for _, raw := range []string{"", " ", ",", " , , "} {
		got, err := parseTags(raw)
		if err != nil {
			t.Fatalf("parseTags(%q): unexpected error: %v", raw, err)
		}
		if len(got) != 0 {
			t.Fatalf("parseTags(%q): expected no tags, got %v", raw, got)
		}
	}
}

func TestParseTagsRejectsInvalidEntries(t *testing.T) {
	for _, raw := range []string{"prod edge", "prod,ed ge", "prod,ed@ge", "prod,ed.ge", "prod,ed/ge"} {
		if _, err := parseTags(raw); err == nil {
			t.Fatalf("parseTags(%q): expected error, got none", raw)
		}
	}
}
