package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/audit"
)

func setupStackDependencyGraphTestApp(t *testing.T) (core.App, http.Handler) {
	t.Helper()
	t.Setenv("STACKS_STORAGE_PATH", t.TempDir())

	app := newSetupTestApp(t)
	admin := createTestUser(t, app, "admin-depgraph@example.com", "password123", "admin")

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
	rr.registerStackDependencyGraphRoute()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux
}

func TestStackDependencyGraphRoute(t *testing.T) {
	app, mux := setupStackDependencyGraphTestApp(t)

	repo, worker := createOverridesTestRepoAndWorker(t, app)
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, t.TempDir())
	stack.Set("current_version", 1)
	if err := app.Save(stack); err != nil {
		t.Fatalf("set current_version: %v", err)
	}

	composeYAML := `
services:
  web:
    image: nginx:1.28
    depends_on:
      api:
        condition: service_healthy
    networks:
      - proxy
  api:
    image: my/api:1.0
    volumes:
      - data:/var/lib/api
networks:
  proxy:
    external: true
volumes:
  data: {}
`
	writeStackRevisionFile(t, app, stack.Id, 1, composeYAML)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/custom/stacks/%s/dependency-graph", stack.Id), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	for _, want := range []string{
		`"id":"service:web"`,
		`"image":"nginx:1.28"`,
		`"id":"service:api"`,
		`"image":"my/api:1.0"`,
		`"id":"network:proxy"`,
		`"external":true`,
		`"id":"volume:data"`,
		`"type":"depends_on"`,
		`"label":"service_healthy"`,
		`"type":"network"`,
		`"type":"volume"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got: %s", want, body)
		}
	}
}

func TestStackDependencyGraphRouteUnknownStack(t *testing.T) {
	_, mux := setupStackDependencyGraphTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/custom/stacks/nonexistent/dependency-graph", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStackDependencyGraphRouteInvalidCompose(t *testing.T) {
	app, mux := setupStackDependencyGraphTestApp(t)

	repo, worker := createOverridesTestRepoAndWorker(t, app)
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, t.TempDir())
	stack.Set("current_version", 1)
	if err := app.Save(stack); err != nil {
		t.Fatalf("set current_version: %v", err)
	}

	writeStackRevisionFile(t, app, stack.Id, 1, "not: [valid: compose")

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/custom/stacks/%s/dependency-graph", stack.Id), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("expected error body, got: %s", rec.Body.String())
	}
}

func TestStackDependencyGraphRouteMissingCompose(t *testing.T) {
	app, mux := setupStackDependencyGraphTestApp(t)

	repo, worker := createOverridesTestRepoAndWorker(t, app)
	// No revision written and no worker connected, so the route falls through
	// to the local-source resolution path and fails to reach the file.
	stack := createOverridesTestStack(t, app, repo.Id, worker.Id, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/custom/stacks/%s/dependency-graph", stack.Id), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("expected error body, got: %s", rec.Body.String())
	}
}
