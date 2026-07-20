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

func TestStackRevisionRoute(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		unknownStack   bool
		writeRevision  bool
		expectedStatus int
		bodyContains   []string
	}{
		{
			name:           "valid revision",
			version:        "34",
			writeRevision:  true,
			expectedStatus: http.StatusOK,
			bodyContains:   []string{"nginx:1.28", `"filename":"v34.yml"`},
		},
		{
			name:           "missing revision",
			version:        "99",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "invalid version text",
			version:        "not-a-number",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "zero version",
			version:        "0",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "negative version",
			version:        "-1",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "unknown stack",
			version:        "1",
			unknownStack:   true,
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, mux := setupStackRevisionsTestApp(t)

			stackID := "nonexistent"
			if !tt.unknownStack {
				repo, worker := createOverridesTestRepoAndWorker(t, app)
				stack := createOverridesTestStack(t, app, repo.Id, worker.Id, t.TempDir())
				stackID = stack.Id
			}
			if tt.writeRevision {
				writeStackRevisionFile(t, app, stackID, 34, "name: my_stack\nservices:\n  web:\n    image: nginx:1.28\n")
			}

			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/custom/stacks/%s/revisions/%s", stackID, tt.version), nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Fatalf("expected %d, got %d: %s", tt.expectedStatus, rec.Code, rec.Body.String())
			}
			for _, want := range tt.bodyContains {
				if !strings.Contains(rec.Body.String(), want) {
					t.Fatalf("expected body to contain %q, got: %s", want, rec.Body.String())
				}
			}
		})
	}
}
