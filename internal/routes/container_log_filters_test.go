package routes

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeContainerLogFilters(t *testing.T) {
	reference := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

	t.Run("normalizes relative and RFC3339 values from one reference", func(t *testing.T) {
		since, until, err := normalizeContainerLogFilters("30m", "2026-10-24T11:45:00Z", "ERROR", reference)
		if err != nil {
			t.Fatalf("normalizeContainerLogFilters: %v", err)
		}
		if since != "1792841400" || until != "1792842300" {
			t.Fatalf("got since=%s until=%s", since, until)
		}
	})

	for _, raw := range []string{"2026-10-24T12:00:00", "0s", "bad"} {
		t.Run("rejects invalid time "+raw, func(t *testing.T) {
			_, _, err := normalizeContainerLogFilters(raw, "", "", reference)
			if err == nil {
				t.Fatalf("expected %q to fail", raw)
			}
		})
	}

	if _, _, err := normalizeContainerLogFilters("2026-10-24T12:00:00Z", "2026-10-24T12:00:00Z", "", reference); err == nil || !strings.Contains(err.Error(), "before") {
		t.Fatalf("expected invalid interval error, got %v", err)
	}
	if _, _, err := normalizeContainerLogFilters("", "", "(", reference); err == nil || !strings.Contains(err.Error(), "pattern") {
		t.Fatalf("expected invalid pattern error, got %v", err)
	}
}

func TestFilterAndCapLogOutput(t *testing.T) {
	if got := filterLogOutput("INFO one\nERROR two\n", "ERROR"); got != "ERROR two" {
		t.Fatalf("filtered logs = %q", got)
	}
	large := strings.Repeat("x", 70<<10)
	got, truncated := capLogOutput(large)
	if !truncated || len(got) != 64<<10 {
		t.Fatalf("capLogOutput length=%d truncated=%t", len(got), truncated)
	}
}
