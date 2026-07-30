package lint_test

import (
	"testing"

	"github.com/wireops/wireops/internal/lint"
)

// numberedSource is the fixture the line assertions below are written
// against. Line numbers are 1-based, so `services:` is line 1.
//
//	 1  services:
//	 2    web:
//	 3      image: nginx:latest
//	 4      ports:
//	 5        - "8080:80"
//	 6        - "9090:90"
//	 7      environment:
//	 8        POSTGRES_PASSWORD: hunter2
//	 9    my.api:
//	10      image: api:latest
//	11      privileged: true
const numberedSource = `services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
      - "9090:90"
    environment:
      POSTGRES_PASSWORD: hunter2
  my.api:
    image: api:latest
    privileged: true
`

// lineFor resolves a single path against a source and returns its line.
func lineFor(t *testing.T, source string, path string) int {
	t.Helper()
	resolved := lint.ResolveLines([]byte(source), []lint.Finding{{Path: path}})
	if len(resolved) != 1 {
		t.Fatalf("ResolveLines returned %d findings, want 1", len(resolved))
	}
	return resolved[0].Line
}

func TestResolveLinesLocatesPaths(t *testing.T) {
	cases := []struct {
		name string
		path string
		want int
	}{
		{"top level key", "services", 1},
		{"service block", "services.web", 2},
		{"scalar under a service", "services.web.image", 3},
		{"sequence key", "services.web.ports", 4},
		{"first sequence element", "services.web.ports[0]", 5},
		{"second sequence element", "services.web.ports[1]", 6},
		{"nested map key", "services.web.environment.POSTGRES_PASSWORD", 8},
		{"service name containing a dot", "services.my.api", 9},
		{"key under a dotted service name", "services.my.api.privileged", 11},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lineFor(t, numberedSource, tc.path); got != tc.want {
				t.Errorf("line for %q = %d, want %d", tc.path, got, tc.want)
			}
		})
	}
}

// TestResolveLinesFallsBackToTheDeepestExistingPrefix covers the "missing key"
// findings, which are the common case: the path names a key that is absent
// precisely because its absence is the problem.
func TestResolveLinesFallsBackToTheDeepestExistingPrefix(t *testing.T) {
	cases := []struct {
		name string
		path string
		want int
	}{
		{"absent leaf falls back to the service", "services.web.restart", 2},
		{"absent nested block falls back to the service", "services.web.deploy.resources.limits", 2},
		{"out-of-range index falls back to the key", "services.web.ports[9]", 4},
		{"unknown service resolves to the services key", "services.nope.image", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lineFor(t, numberedSource, tc.path); got != tc.want {
				t.Errorf("line for %q = %d, want %d", tc.path, got, tc.want)
			}
		})
	}
}

func TestResolveLinesHandlesUnresolvableInput(t *testing.T) {
	cases := []struct {
		name   string
		source string
		path   string
	}{
		{"unparseable yaml", "{not: valid: yaml:", "services.web.image"},
		{"empty source", "", "services.web.image"},
		{"path matches nothing", numberedSource, "networks.backend"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lineFor(t, tc.source, tc.path); got != 0 {
				t.Errorf("line = %d, want 0 when the path cannot be located", got)
			}
		})
	}
}

func TestResolveLinesLeavesPathlessFindingsAlone(t *testing.T) {
	resolved := lint.ResolveLines([]byte(numberedSource), []lint.Finding{
		{Message: "no path", Path: ""},
		{Message: "has path", Path: "services.web.image"},
	})

	if resolved[0].Line != 0 {
		t.Errorf("pathless finding got line %d, want 0", resolved[0].Line)
	}
	if resolved[1].Line != 3 {
		t.Errorf("path finding got line %d, want 3", resolved[1].Line)
	}
}

// TestResolveLinesDoesNotMutateTheInput guards the copy in ResolveLines: the
// caller's slice is also the one recorded on the deploy timeline.
func TestResolveLinesDoesNotMutateTheInput(t *testing.T) {
	original := []lint.Finding{{Path: "services.web.image"}}
	resolved := lint.ResolveLines([]byte(numberedSource), original)

	if original[0].Line != 0 {
		t.Errorf("input finding was mutated to line %d, want it left at 0", original[0].Line)
	}
	if resolved[0].Line == 0 {
		t.Error("returned finding has no line, want it resolved")
	}
}

// TestResolveLinesOnRealFindings runs the actual rule output through the
// resolver, which is what the API does — a path format the rules emit but the
// resolver cannot parse would show up here rather than in the hand-written
// paths above.
func TestResolveLinesOnRealFindings(t *testing.T) {
	cfg := mustConfig(t, `{
		"services": {
			"web": {
				"image": "nginx:latest",
				"ports": [{"target": 80, "published": "8080"}],
				"environment": {"POSTGRES_PASSWORD": "hunter2"}
			}
		}
	}`)

	report := lint.Run(cfg, lint.Context{})
	resolved := lint.ResolveLines([]byte(numberedSource), report.Findings)

	if len(resolved) == 0 {
		t.Fatal("expected findings to resolve lines for")
	}
	for _, f := range resolved {
		if f.Path == "" {
			continue
		}
		if f.Line == 0 {
			t.Errorf("finding %s (path %q) resolved to no line", f.Rule, f.Path)
		}
	}
}
