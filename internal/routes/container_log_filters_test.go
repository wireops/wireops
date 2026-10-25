package routes

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestNormalizeContainerLogFilters(t *testing.T) {
	reference := time.Date(2026, 10, 24, 12, 0, 0, 123456789, time.UTC)

	t.Run("normalizes relative and RFC3339 values from one reference", func(t *testing.T) {
		since, until, err := normalizeContainerLogFilters("30m", "2026-10-24T11:45:00Z", "ERROR", reference)
		if err != nil {
			t.Fatalf("normalizeContainerLogFilters: %v", err)
		}
		if since != "1792841400.123456789" || until != "1792842300" {
			t.Fatalf("got since=%s until=%s", since, until)
		}
	})

	t.Run("preserves fractional RFC3339 and Unix timestamps", func(t *testing.T) {
		since, until, err := normalizeContainerLogFilters("2026-10-24T11:30:00.25Z", "1792842300.125", "", reference)
		if err != nil {
			t.Fatalf("normalizeContainerLogFilters: %v", err)
		}
		if since != "1792841400.25" || until != "1792842300.125" {
			t.Fatalf("got since=%s until=%s", since, until)
		}
	})

	t.Run("rewrites pre-epoch instants as RFC3339 for Docker's client-side parser", func(t *testing.T) {
		// A negative seconds.nanos string is rejected before the request is
		// sent: Docker reads the "-" as RFC3339-like and never reaches its own
		// Unix-timestamp branch.
		cases := map[string]string{
			"-1.5":                   "1969-12-31T23:59:58.5Z",
			"-1":                     "1969-12-31T23:59:59Z",
			"1969-12-31T23:59:58.5Z": "1969-12-31T23:59:58.5Z",
		}
		for raw, want := range cases {
			got, _, err := normalizeLogTime(raw, reference)
			if err != nil {
				t.Fatalf("normalizeLogTime(%q): %v", raw, err)
			}
			if got != want {
				t.Fatalf("normalizeLogTime(%q) = %q, want %q", raw, got, want)
			}
		}
	})

	t.Run("still emits numeric timestamps at or after the epoch", func(t *testing.T) {
		// "0" is deliberately absent: ParseDuration accepts it, so the
		// relative-duration branch claims it and rejects it as non-positive.
		for raw, want := range map[string]string{
			"0.5":        "0.5",
			"1.500":      "1.5",
			"1792842300": "1792842300",
		} {
			got, _, err := normalizeLogTime(raw, reference)
			if err != nil {
				t.Fatalf("normalizeLogTime(%q): %v", raw, err)
			}
			if got != want {
				t.Fatalf("normalizeLogTime(%q) = %q, want %q", raw, got, want)
			}
		}
	})

	// strconv.ParseInt accepts a sign inside either component, so these parsed
	// as valid before the digit check. "-1.-5" was the worst of them: it
	// resolved to a different instant here than the daemon reads from the same
	// string.
	for _, raw := range []string{"2026-10-24T12:00:00", "0s", "bad", "1.+5", "+1.5", "-1.-5", "1.-5", "1.5e2"} {
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
	large := "old-" + strings.Repeat("x", 6<<10) + strings.Repeat("é", 33<<10) + "newest!"
	got, truncated := capLogOutput(large)
	if !truncated || len(got) > 64<<10 {
		t.Fatalf("capLogOutput length=%d truncated=%t", len(got), truncated)
	}
	if !strings.HasSuffix(got, "newest!") || !utf8.ValidString(got) {
		t.Fatalf("capLogOutput did not preserve a valid newest tail: %q", got[len(got)-16:])
	}
}
