package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/protocol"
	wiresync "github.com/wireops/wireops/internal/sync"
)

// observationDispatcher is intentionally response-oriented: it lets these
// route tests share one PocketBase bootstrap while modelling both worker
// failures and valid observations without Docker.
type observationDispatcher struct {
	mu        sync.Mutex
	connected bool
	resources protocol.CommandResult
	resourceE error
	status    protocol.CommandResult
	statusE   error
	logs      protocol.CommandResult
	logsE     error
	commands  []any
}

func (d *observationDispatcher) Dispatch(_ context.Context, _ string, command interface{}) (protocol.CommandResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.commands = append(d.commands, command)
	switch command.(type) {
	case protocol.GetResourcesCommand:
		return d.resources, d.resourceE
	case protocol.GetStatusCommand:
		return d.status, d.statusE
	case protocol.GetContainerLogsCommand:
		return d.logs, d.logsE
	default:
		return protocol.CommandResult{}, nil
	}
}

func (d *observationDispatcher) IsConnected(string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.connected
}

func (d *observationDispatcher) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.resources = protocol.CommandResult{}
	d.resourceE = nil
	d.status = protocol.CommandResult{}
	d.statusE = nil
	d.logs = protocol.CommandResult{}
	d.logsE = nil
	d.commands = nil
	d.connected = true
}

func setupObservabilityTestApp(t *testing.T, dispatcher *observationDispatcher) (core.App, http.Handler) {
	t.Helper()
	t.Setenv("DATA_DIR", t.TempDir())
	app := newSetupTestApp(t)
	admin := createTestUser(t, app, "admin-observability@example.com", "password123", "admin")
	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{App: app, Auth: admin, Event: router.Event{Response: w, Request: req}}, nil
	})
	r.BindFunc(audit.CustomRouteMiddleware(app))
	rr := routeRegistrar{r: r, app: app, scheduler: wiresync.NewScheduler(app, dispatcher), workerSvc: dispatcher}
	rr.registerStackInspectionRoutes()
	rr.registerStackObservabilityRoutes()
	rr.registerContainerReadRoutes()
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux
}

func newObservedStack(t *testing.T, app core.App, name string, capabilities []string, rendered string) *core.Record {
	t.Helper()
	stack := newExecStack(t, app, name, capabilities)
	stack.Set("deployed_version", 1)
	stack.Set("desired_commit", "rendered-commit")
	stack.Set("deployed_commit", "deployed-commit")
	if err := app.Save(stack); err != nil {
		t.Fatalf("save observed stack: %v", err)
	}
	path := wiresync.NewRenderer(app).GetRevisionFilePath(stack.Id, 1)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("create revision directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(rendered), 0o600); err != nil {
		t.Fatalf("write revision: %v", err)
	}
	return stack
}

func getObservation(t *testing.T, mux http.Handler, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	body := map[string]any{}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response %q: %v", res.Body.String(), err)
	}
	return res.Code, body
}

func TestResourceInventoryRouteMakesCompletenessExplicit(t *testing.T) {
	dispatcher := &observationDispatcher{connected: true}
	app, mux := setupObservabilityTestApp(t, dispatcher)
	composeYAML := "name: resource-test\nservices:\n  web:\n    image: nginx\n"

	t.Run("old worker data is explicitly partial", func(t *testing.T) {
		dispatcher.reset()
		payload, _ := json.Marshal(protocol.GetResourcesResult{Volumes: []protocol.VolumeInfo{}, Networks: []protocol.NetworkInfo{}})
		dispatcher.resources = protocol.CommandResult{Output: string(payload)}
		stack := newObservedStack(t, app, "old-resource-worker", nil, composeYAML)

		code, body := getObservation(t, mux, "/api/custom/stacks/"+stack.Id+"/resources")
		if code != http.StatusOK || body["partial"] != true {
			t.Fatalf("expected confirmed partial inventory, got %d %#v", code, body)
		}
		warnings, _ := body["warnings"].([]any)
		if len(warnings) == 0 {
			t.Fatalf("expected upgrade warning, got %#v", body)
		}
	})

	t.Run("transport and invalid worker output are never empty success", func(t *testing.T) {
		stack := newObservedStack(t, app, "resource-errors", []string{protocol.CapabilityResourceInventoryV1}, composeYAML)
		dispatcher.reset()
		dispatcher.resourceE = errors.New("connection lost")
		code, _ := getObservation(t, mux, "/api/custom/stacks/"+stack.Id+"/resources")
		if code != http.StatusServiceUnavailable {
			t.Fatalf("transport status = %d, want 503", code)
		}
		dispatcher.reset()
		dispatcher.resources = protocol.CommandResult{Output: "{"}
		code, _ = getObservation(t, mux, "/api/custom/stacks/"+stack.Id+"/resources")
		if code != http.StatusBadGateway {
			t.Fatalf("invalid worker JSON status = %d, want 502", code)
		}
	})
}

func TestStackHealthUsesDeployedRevisionAndStrictReplicaState(t *testing.T) {
	dispatcher := &observationDispatcher{connected: true}
	app, mux := setupObservabilityTestApp(t, dispatcher)
	composeYAML := "name: health-test\nservices:\n  web:\n    image: nginx\n    deploy:\n      replicas: 2\n  migrate:\n    image: migrate\n    labels:\n      customization.init: 'true'\n  disabled:\n    image: nginx\n    deploy:\n      replicas: 0\n"
	stack := newObservedStack(t, app, "health", []string{protocol.CapabilityResourceInventoryV1}, composeYAML)

	t.Run("one unhealthy replica prevents healthy", func(t *testing.T) {
		dispatcher.reset()
		statuses, _ := json.Marshal([]compose.ServiceStatus{
			{ServiceName: "web", ContainerID: "one", Status: "running", Health: "healthy"},
			{ServiceName: "web", ContainerID: "two", Status: "running", Health: "unhealthy"},
			{ServiceName: "migrate", ContainerID: "init", Status: "exited", ExitCode: 0},
		})
		dispatcher.status = protocol.CommandResult{Output: string(statuses)}
		code, body := getObservation(t, mux, "/api/custom/stacks/"+stack.Id+"/health")
		if code != http.StatusOK || body["state"] != "degraded" || body["unhealthy_services"] != float64(1) {
			t.Fatalf("expected degraded strict health, got %d %#v", code, body)
		}
		if body["expected_services"] != float64(2) { // scale-zero is not expected
			t.Fatalf("scale-zero service leaked into health: %#v", body)
		}
	})

	t.Run("offline worker is unknown without a dispatch", func(t *testing.T) {
		dispatcher.reset()
		dispatcher.connected = false
		code, body := getObservation(t, mux, "/api/custom/stacks/"+stack.Id+"/health")
		if code != http.StatusOK || body["state"] != "unknown" {
			t.Fatalf("expected unknown offline health, got %d %#v", code, body)
		}
	})
}

func TestInventoryAndLogCapabilityRoutes(t *testing.T) {
	dispatcher := &observationDispatcher{connected: true}
	app, mux := setupObservabilityTestApp(t, dispatcher)
	composeYAML := "name: inventory-test\nservices:\n  web:\n    image: nginx\n"
	status, _ := json.Marshal([]compose.ServiceStatus{{ServiceName: "web", Status: "running", Health: "healthy"}})

	t.Run("inventory preserves a stack when one worker call fails", func(t *testing.T) {
		dispatcher.reset()
		dispatcher.status = protocol.CommandResult{Output: string(status)}
		dispatcher.resources = protocol.CommandResult{Error: "resource query failed"}
		stack := newObservedStack(t, app, "inventory", []string{protocol.CapabilityResourceInventoryV1}, composeYAML)
		code, body := getObservation(t, mux, "/api/custom/inventory?per_page=500")
		if code != http.StatusOK || body["per_page"] != float64(200) {
			t.Fatalf("expected capped inventory page, got %d %#v", code, body)
		}
		items, _ := body["stacks"].([]any)
		if len(items) == 0 {
			t.Fatal("expected inventory stack")
		}
		found := false
		for _, raw := range items {
			item := raw.(map[string]any)
			if item["id"] == stack.Id {
				found = true
				if item["resources_error"] == nil || item["health"] == nil {
					t.Fatalf("expected isolated resource failure, got %#v", item)
				}
			}
		}
		if !found {
			t.Fatalf("expected stack %s in inventory", stack.Id)
		}
	})

	t.Run("time filtering requires an updated worker and old workers still filter patterns", func(t *testing.T) {
		dispatcher.reset()
		stack := newObservedStack(t, app, "old-log-worker", nil, composeYAML)
		code, body := getObservation(t, mux, "/api/custom/stacks/"+stack.Id+"/container/c-web/logs?since=30m")
		if code != http.StatusConflict || body["required_capability"] != protocol.CapabilityContainerLogsFilterV1 {
			t.Fatalf("expected 409 capability response, got %d %#v", code, body)
		}
		dispatcher.logs = protocol.CommandResult{Output: "INFO ready\nERROR failed\n"}
		code, body = getObservation(t, mux, "/api/custom/stacks/"+stack.Id+"/container/c-web/logs?pattern=ERROR")
		if code != http.StatusOK || body["logs"] != "ERROR failed" || body["pattern"] != "ERROR" {
			t.Fatalf("expected server-side compatibility filter, got %d %#v", code, body)
		}
	})

	t.Run("updated worker reports the server-normalized filters", func(t *testing.T) {
		dispatcher.reset()
		stack := newObservedStack(t, app, "new-log-worker", []string{protocol.CapabilityContainerLogsFilterV1}, composeYAML)
		payload, _ := json.Marshal(protocol.ContainerLogsResult{Logs: "ERROR failed"})
		dispatcher.logs = protocol.CommandResult{Output: string(payload)}
		code, body := getObservation(t, mux, "/api/custom/stacks/"+stack.Id+"/container/c-web/logs?tail=3&since=1700000000&until=1700000300&pattern=ERROR")
		if code != http.StatusOK || body["applied_tail"] != "3" || body["applied_since"] != "1700000000" || body["applied_until"] != "1700000300" || body["pattern"] != "ERROR" {
			t.Fatalf("expected normalized applied filters, got %d %#v", code, body)
		}
		dispatcher.mu.Lock()
		defer dispatcher.mu.Unlock()
		command, ok := dispatcher.commands[len(dispatcher.commands)-1].(protocol.GetContainerLogsCommand)
		if !ok || command.Tail != "3" || command.Since != "1700000000" || command.Until != "1700000300" || command.Pattern != "ERROR" {
			t.Fatalf("unexpected worker command %#v", dispatcher.commands)
		}
	})

}
