package routes

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/rbac"
)

// repositoryRoutesMux registers registerRepositoryRoutes (compose-wireops-files,
// compose-definition, and their siblings) against a real test app, mirroring
// composeCreateRoutesMux in compose_create_route_test.go.
func repositoryRoutesMux(t *testing.T, app core.App, auth *core.Record) http.Handler {
	t.Helper()

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
			Auth:  auth,
		}, nil
	})

	rr := routeRegistrar{r: r, app: app}
	rr.registerRepositoryRoutes()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return mux
}

func setupRepositoryDiscoveryTest(t *testing.T, files map[string]string) (http.Handler, *core.Record) {
	t.Helper()
	workspace := t.TempDir()
	t.Setenv("REPOS_WORKSPACE", workspace)
	app := newSetupTestApp(t)
	operator := createTestUser(t, app, "repo-discovery-operator@example.com", "Password1!", rbac.RoleOperator)

	fixtureDir := t.TempDir()
	localGitFixture(t, fixtureDir, files)
	repo := createMigrateTestRepo(t, app, "repo-discovery-repo", fixtureDir)

	mux := repositoryRoutesMux(t, app, operator)
	return mux, repo
}

func TestComposeWireopsFilesDiscovery(t *testing.T) {
	mux, repo := setupRepositoryDiscoveryTest(t, map[string]string{
		"docker-compose.yml": "x-wireops:\n  version: wireops.v1\n  name: api\nservices:\n  web:\n    image: nginx\n",
		"other.yml":          "services:\n  web:\n    image: nginx\n",
	})

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/repositories/"+repo.Id+"/compose-wireops-files", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var files []string
	if err := json.Unmarshal(rec.Body.Bytes(), &files); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(files) != 1 || files[0] != "docker-compose.yml" {
		t.Fatalf("expected only docker-compose.yml, got %v", files)
	}
}

func TestComposeDefinitionRoute(t *testing.T) {
	mux, repo := setupRepositoryDiscoveryTest(t, map[string]string{
		"docker-compose.yml": "x-wireops:\n  version: wireops.v1\n  name: api\nservices:\n  web:\n    image: nginx\n",
	})

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/repositories/"+repo.Id+"/compose-definition?file=docker-compose.yml", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var def struct {
		Name                string `json:"name"`
		ResolvedComposeFile string `json:"resolved_compose_file"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &def); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if def.Name != "api" {
		t.Fatalf("expected name=api, got %q", def.Name)
	}
	if def.ResolvedComposeFile != "docker-compose.yml" {
		t.Fatalf("expected resolved_compose_file=docker-compose.yml, got %q", def.ResolvedComposeFile)
	}
}

func TestComposeDefinitionRouteMissingFileParam(t *testing.T) {
	mux, repo := setupRepositoryDiscoveryTest(t, map[string]string{
		"docker-compose.yml": "x-wireops:\n  version: wireops.v1\n  name: api\nservices:\n  web:\n    image: nginx\n",
	})

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/repositories/"+repo.Id+"/compose-definition", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing file param, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestComposeDefinitionRouteRejectsPathTraversal(t *testing.T) {
	mux, repo := setupRepositoryDiscoveryTest(t, map[string]string{
		"docker-compose.yml": "x-wireops:\n  version: wireops.v1\n  name: api\nservices:\n  web:\n    image: nginx\n",
	})

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/repositories/"+repo.Id+"/compose-definition?file=..%2Fescape.yml", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestComposeDefinitionRouteFileNotFound(t *testing.T) {
	mux, repo := setupRepositoryDiscoveryTest(t, map[string]string{
		"docker-compose.yml": "x-wireops:\n  version: wireops.v1\n  name: api\nservices:\n  web:\n    image: nginx\n",
	})

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/repositories/"+repo.Id+"/compose-definition?file=missing.yml", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing file, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestComposeDefinitionRouteNoXWireopsBlock(t *testing.T) {
	mux, repo := setupRepositoryDiscoveryTest(t, map[string]string{
		"docker-compose.yml": "services:\n  web:\n    image: nginx\n",
	})

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/repositories/"+repo.Id+"/compose-definition?file=docker-compose.yml", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for compose file without x-wireops, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestComposeDefinitionRouteMalformedXWireopsBlock(t *testing.T) {
	mux, repo := setupRepositoryDiscoveryTest(t, map[string]string{
		"docker-compose.yml": "x-wireops: [this, is, not, a, map]\nservices:\n  web:\n    image: nginx\n",
	})

	rec := doJSONRequest(t, mux, http.MethodGet, "/api/custom/repositories/"+repo.Id+"/compose-definition?file=docker-compose.yml", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for malformed x-wireops block, got %d: %s", rec.Code, rec.Body.String())
	}
}
