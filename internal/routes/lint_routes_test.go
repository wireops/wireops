package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	_ "github.com/wireops/wireops/pb_migrations"

	"github.com/wireops/wireops/internal/lint"
	wiresync "github.com/wireops/wireops/internal/sync"
)

// setupLintTestApp wires the lint route behind an authenticated actor of the
// given role, so the tests exercise the same RBAC path a real request takes.
func setupLintTestApp(t *testing.T, role string) (core.App, http.Handler) {
	t.Helper()
	app := newSetupTestApp(t)

	var auth *core.Record
	if role != "" {
		col, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			t.Fatalf("find users collection: %v", err)
		}
		auth = core.NewRecord(col)
		auth.Set("email", role+"@example.com")
		auth.Set("password", "12345678")
		auth.Set("role", role)
		if err := app.Save(auth); err != nil {
			t.Fatalf("create %s user: %v", role, err)
		}
	}

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Auth:  auth,
			Event: router.Event{Response: w, Request: req},
		}, nil
	})

	rr := routeRegistrar{r: r, app: app, scheduler: wiresync.NewScheduler(app, nil)}
	rr.registerLintRoutes()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux
}

func postLint(t *testing.T, mux http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/custom/lint/compose", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func createLintRepo(t *testing.T, app core.App, name string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		t.Fatalf("find repositories collection: %v", err)
	}
	repo := core.NewRecord(col)
	repo.Set("name", name)
	repo.Set("git_url", "https://example.com/"+name+".git")
	repo.Set("branch", "main")
	if err := app.Save(repo); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	return repo
}

func createLintStack(t *testing.T, app core.App, name, repoID string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatalf("find stacks collection: %v", err)
	}
	stack := core.NewRecord(col)
	stack.Set("name", name)
	stack.Set("repository", repoID)
	stack.Set("status", "pending")
	if err := app.Save(stack); err != nil {
		t.Fatalf("create stack: %v", err)
	}
	return stack
}

func TestLintComposeRequiresAuthentication(t *testing.T) {
	_, mux := setupLintTestApp(t, "")

	rec := postLint(t, mux, map[string]any{"repository": "whatever"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an unauthenticated request", rec.Code)
	}
}

func TestLintComposeRejectsViewerRole(t *testing.T) {
	_, mux := setupLintTestApp(t, "viewer")

	rec := postLint(t, mux, map[string]any{"repository": "whatever"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a viewer (lint needs manage_repositories)", rec.Code)
	}
}

func TestLintComposeRejectsPathTraversal(t *testing.T) {
	app, mux := setupLintTestApp(t, "operator")
	repo := createLintRepo(t, app, "repo-a")

	cases := []struct {
		name string
		body map[string]any
	}{
		{"traversal in path", map[string]any{"repository": repo.Id, "compose_path": "../../etc"}},
		{"absolute path", map[string]any{"repository": repo.Id, "compose_path": "/etc"}},
		{"traversal in file", map[string]any{"repository": repo.Id, "compose_file": "../../etc/x.yml"}},
		{"separator in file", map[string]any{"repository": repo.Id, "compose_file": "sub/x.yml"}},
		{"absolute file", map[string]any{"repository": repo.Id, "compose_file": "/etc/x.yml"}},
		{"non-yaml file", map[string]any{"repository": repo.Id, "compose_file": "passwd"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postLint(t, mux, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 — traversal must be rejected before any git or docker work: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestLintComposeRequiresRepository(t *testing.T) {
	_, mux := setupLintTestApp(t, "operator")

	rec := postLint(t, mux, map[string]any{"compose_file": "docker-compose.yml"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when repository is missing", rec.Code)
	}
}

// TestLintComposeRejectsStackFromAnotherRepository is the authorization check
// that keeps the undefined-variable rule from being used as an oracle: without
// it, a caller could lint their own compose file against an unrelated stack's
// resolved environment and learn which variable names that stack defines (and
// make the server resolve its secrets as a side effect).
func TestLintComposeRejectsStackFromAnotherRepository(t *testing.T) {
	app, mux := setupLintTestApp(t, "operator")
	repoA := createLintRepo(t, app, "repo-a")
	repoB := createLintRepo(t, app, "repo-b")
	victimStack := createLintStack(t, app, "victim", repoB.Id)

	rec := postLint(t, mux, map[string]any{
		"repository": repoA.Id,
		"stack":      victimStack.Id,
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when the stack belongs to a different repository: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "does not belong") {
		t.Errorf("body = %s, want it to explain the repository mismatch", rec.Body.String())
	}
}

func TestLintComposeRejectsUnknownStack(t *testing.T) {
	app, mux := setupLintTestApp(t, "operator")
	repo := createLintRepo(t, app, "repo-a")

	rec := postLint(t, mux, map[string]any{"repository": repo.Id, "stack": "does-not-exist"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unknown stack", rec.Code)
	}
}

// TestLintComposeRejectsUnknownWorker guards against a silent downgrade:
// policy.Load falls back to the global policy for a worker it cannot find, so
// accepting an unknown id would report "no policy violations" for a policy
// that was never actually consulted.
func TestLintComposeRejectsUnknownWorker(t *testing.T) {
	app, mux := setupLintTestApp(t, "operator")
	repo := createLintRepo(t, app, "repo-a")

	rec := postLint(t, mux, map[string]any{"repository": repo.Id, "worker": "does-not-exist"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown worker: %s", rec.Code, rec.Body.String())
	}
}

func TestLintComposeRejectsUnknownRepository(t *testing.T) {
	_, mux := setupLintTestApp(t, "operator")

	rec := postLint(t, mux, map[string]any{"repository": "does-not-exist"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unknown repository", rec.Code)
	}
}

func TestRedactWorkspacePaths(t *testing.T) {
	t.Setenv("DATA_DIR", "/srv/wireops-data")

	cases := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "not found error",
			msg:  "compose file not found in /srv/wireops-data/repos/abc123/services",
			want: "compose file not found in <repos>/abc123/services",
		},
		{
			name: "cli stderr with a path",
			msg:  "docker compose config failed: exit status 1\nstderr: open /srv/wireops-data/repos/abc123/x.yml: no such file",
			want: "docker compose config failed: exit status 1\nstderr: open <repos>/abc123/x.yml: no such file",
		},
		{
			name: "yaml diagnostic is preserved verbatim",
			msg:  "yaml: line 4, column 12: mapping values are not allowed in this context",
			want: "yaml: line 4, column 12: mapping values are not allowed in this context",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactWorkspacePaths(tc.msg); got != tc.want {
				t.Errorf("redactWorkspacePaths()\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// TestRedactFindingsStripsWorkspacePaths pins the disclosure boundary for the
// lint report. `docker compose config` rewrites relative bind-mount sources
// into absolute server paths, so a finding's text can carry the repo workspace
// layout even though the author never wrote it.
func TestRedactFindingsStripsWorkspacePaths(t *testing.T) {
	t.Setenv("DATA_DIR", "/srv/wireops-data")

	findings := []lint.Finding{
		{
			Rule:    "policy/volumes",
			Path:    "services.web.volumes",
			Message: `volume policy violation: volume mount path "/srv/wireops-data/repos/abc123/config" is not in the worker's allowed volume list`,
			Hint:    "blocked by this worker's deploy policy",
		},
		{
			Rule:    "compose/relative-bind-mount",
			Path:    "services.db.volumes",
			Message: `service "db" bind-mounts "./data", a path inside the repository`,
			Hint:    "the worker deploys from a rendered revision directory under /srv/wireops-data",
		},
	}

	redactFindings(findings)

	for _, f := range findings {
		if strings.Contains(f.Message, "/srv/wireops-data") {
			t.Errorf("message still leaks the workspace path: %q", f.Message)
		}
		if strings.Contains(f.Hint, "/srv/wireops-data") {
			t.Errorf("hint still leaks the workspace path: %q", f.Hint)
		}
	}

	// The diagnostic itself must survive — redaction replaces the server root,
	// it does not blank the finding.
	if !strings.Contains(findings[0].Message, "abc123/config") {
		t.Errorf("message lost the offending path entirely: %q", findings[0].Message)
	}
	if !strings.Contains(findings[1].Message, "./data") {
		t.Errorf("author-written path was altered: %q", findings[1].Message)
	}
	// Path is a dotted config location, not a filesystem path, so it is left
	// alone by design.
	if findings[0].Path != "services.web.volumes" {
		t.Errorf("Path = %q, want the dotted config location untouched", findings[0].Path)
	}
}
