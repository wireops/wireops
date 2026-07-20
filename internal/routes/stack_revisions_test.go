package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/audit"
	wiresync "github.com/wireops/wireops/internal/sync"
)

// setupStackRevisionsTestApp wires only registerStackRevisionRoute, treating
// every request as an authenticated admin so rbac.Require(rbac.CapViewStacks)
// passes, and isolates rendered revision files under a temp STACKS_STORAGE_PATH.
func setupStackRevisionsTestApp(t *testing.T) (core.App, http.Handler) {
	t.Helper()
	t.Setenv("STACKS_STORAGE_PATH", t.TempDir())

	app := newSetupTestApp(t)
	admin := createTestUser(t, app, "admin-revisions@example.com", "password123", "admin")

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:  app,
			Auth: admin,
			Event: router.Event{
				Response: w,
				Request:  req,
			},
		}, nil
	})
	r.BindFunc(audit.CustomRouteMiddleware(app))

	rr := routeRegistrar{r: r, app: app}
	rr.registerStackRevisionRoute()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux
}

func writeStackRevisionFile(t *testing.T, app core.App, stackID string, version int, content string) {
	t.Helper()
	renderer := wiresync.NewRenderer(app)
	path := renderer.GetRevisionFilePath(stackID, version)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir revision dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write revision file: %v", err)
	}
}

func TestStackRevisionRouteReturnsContent(t *testing.T) {
	app, mux := setupStackRevisionsTestApp(t)
	repo, worker := createOverridesTestRepoAndWorker(t, app)
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, t.TempDir())
	writeStackRevisionFile(t, app, stack.Id, 34, "name: my_stack\nservices:\n  web:\n    image: nginx:1.28\n")

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/custom/stacks/%s/revisions/34", stack.Id), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "nginx:1.28") || !strings.Contains(rec.Body.String(), `"filename":"v34.yml"`) {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}

func TestStackRevisionRouteMissingRevisionReturns404(t *testing.T) {
	app, mux := setupStackRevisionsTestApp(t)
	repo, worker := createOverridesTestRepoAndWorker(t, app)
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/custom/stacks/%s/revisions/99", stack.Id), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStackRevisionRouteInvalidVersionReturns400(t *testing.T) {
	app, mux := setupStackRevisionsTestApp(t)
	repo, worker := createOverridesTestRepoAndWorker(t, app)
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/custom/stacks/%s/revisions/not-a-number", stack.Id), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStackRevisionRouteUnknownStackReturns404(t *testing.T) {
	_, mux := setupStackRevisionsTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/custom/stacks/nonexistent/revisions/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
