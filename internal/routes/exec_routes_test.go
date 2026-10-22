package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	_ "github.com/wireops/wireops/pb_migrations"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/protocol"
	wiresync "github.com/wireops/wireops/internal/sync"
)

// recordingExecDispatcher captures the command the route dispatches and
// answers with a canned result, so the route can be exercised without a
// worker or a Docker daemon. Its fields are rewritten per subtest, which is
// what lets every scenario share one bootstrap.
type recordingExecDispatcher struct {
	connected bool
	result    protocol.CommandResult
	err       error

	gotWorkerID string
	gotCommand  protocol.ExecCommand
	calls       int
	hadDeadline bool
	// deadlineIn is how long the dispatch context had left when it arrived,
	// which is what the route's grace period adds to.
	deadlineIn time.Duration
}

func (d *recordingExecDispatcher) Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error) {
	d.calls++
	d.gotWorkerID = workerID
	if execCmd, ok := cmd.(protocol.ExecCommand); ok {
		d.gotCommand = execCmd
	}
	var deadline time.Time
	deadline, d.hadDeadline = ctx.Deadline()
	if d.hadDeadline {
		d.deadlineIn = time.Until(deadline)
	}
	return d.result, d.err
}

func (d *recordingExecDispatcher) IsConnected(string) bool { return d.connected }

// reset clears what a previous subtest recorded and arms the next one's
// answer, so state cannot leak between scenarios sharing the dispatcher.
func (d *recordingExecDispatcher) reset(result protocol.CommandResult) {
	*d = recordingExecDispatcher{connected: true, result: result}
}

// setupExecTestApp registers the exec route as an authenticated admin. One
// PocketBase bootstrap costs about a second, so it runs once for the whole
// test and each subtest makes its own rows with newExecStack.
func setupExecTestApp(t *testing.T, dispatcher *recordingExecDispatcher) (core.App, http.Handler) {
	t.Helper()

	app := newSetupTestApp(t)
	admin := createTestUser(t, app, "admin-exec@example.com", "password123", "admin")

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Auth:  admin,
			Event: router.Event{Response: w, Request: req},
		}, nil
	})
	r.BindFunc(audit.CustomRouteMiddleware(app))

	rr := routeRegistrar{
		r:         r,
		app:       app,
		scheduler: wiresync.NewScheduler(app, dispatcher),
		workerSvc: dispatcher,
	}
	rr.registerExecRoutes()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux
}

// newExecStack creates a stack of its own, assigned to a worker advertising
// the given capabilities (nil for a worker too old to know any).
func newExecStack(t *testing.T, app core.App, name string, capabilities []string) *core.Record {
	t.Helper()

	workerCol, err := app.FindCollectionByNameOrId("workers")
	if err != nil {
		t.Fatalf("find workers collection: %v", err)
	}
	worker := core.NewRecord(workerCol)
	worker.Set("hostname", "exec-worker-"+name)
	worker.Set("status", "ACTIVE")
	worker.Set("fingerprint", "exec-worker-fp-"+core.GenerateDefaultRandomId())
	if capabilities != nil {
		worker.Set("capabilities", capabilities)
	}
	if err := app.Save(worker); err != nil {
		t.Fatalf("create test worker: %v", err)
	}

	stackCol, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatalf("find stacks collection: %v", err)
	}
	stack := core.NewRecord(stackCol)
	stack.Set("name", "exec-stack-"+name)
	stack.Set("worker", worker.Id)
	// compose_project_name short-circuits DeployedProjectName, which would
	// otherwise want a repository checkout on disk.
	stack.Set("compose_project_name", "exec-stack-"+name)
	if err := app.Save(stack); err != nil {
		t.Fatalf("create test stack: %v", err)
	}
	return stack
}

func postExec(t *testing.T, mux http.Handler, stackID string, body map[string]any) (int, map[string]any) {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/custom/stacks/"+stackID+"/exec", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	var decoded map[string]any
	if res.Body.Len() > 0 {
		if err := json.Unmarshal(res.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decoding response %q: %v", res.Body.String(), err)
		}
	}
	return res.Code, decoded
}

func TestExecRoute(t *testing.T) {
	dispatcher := &recordingExecDispatcher{connected: true}
	app, mux := setupExecTestApp(t, dispatcher)
	capable := []string{protocol.CapabilityExecOneShotV1}

	t.Run("returns separated streams and the exit code", func(t *testing.T) {
		execResult, _ := json.Marshal(protocol.ExecResult{Stdout: "hi\n", Stderr: "oops\n", ExitCode: 2})
		dispatcher.reset(protocol.CommandResult{CommandID: "exec-1", Output: string(execResult)})
		stack := newExecStack(t, app, "streams", capable)

		code, body := postExec(t, mux, stack.Id, map[string]any{
			"container_id":    "c-web",
			"cmd":             []string{"/bin/sh", "-c", "echo hi"},
			"timeout_seconds": 20,
		})
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %#v", code, body)
		}
		if body["stdout"] != "hi\n" || body["stderr"] != "oops\n" {
			t.Errorf("expected both streams, got %#v", body)
		}
		if body["exit_code"] != float64(2) {
			t.Errorf("exit_code = %v, want 2", body["exit_code"])
		}

		if dispatcher.gotCommand.ContainerID != "c-web" || len(dispatcher.gotCommand.Cmd) != 3 {
			t.Errorf("unexpected dispatched command %+v", dispatcher.gotCommand)
		}
		if dispatcher.gotCommand.ProjectName != "exec-stack-streams" {
			t.Errorf("project_name = %q, want exec-stack-streams", dispatcher.gotCommand.ProjectName)
		}
		// The worker gets the caller's own timeout; the grace period below is
		// the server's wait, not the worker's.
		if dispatcher.gotCommand.TimeoutSeconds != 20 {
			t.Errorf("worker timeout = %d, want 20", dispatcher.gotCommand.TimeoutSeconds)
		}
		// Dispatch's own fallback is five minutes and only applies with no
		// deadline on the context, so the route has to set one.
		if !dispatcher.hadDeadline {
			t.Error("expected the dispatch context to carry the request's timeout")
		}
	})

	t.Run("waits past the worker deadline so a timed-out run still answers", func(t *testing.T) {
		execResult, _ := json.Marshal(protocol.ExecResult{Stdout: "partial", ExitCode: -1, TimedOut: true})
		dispatcher.reset(protocol.CommandResult{CommandID: "exec-1", Output: string(execResult)})
		stack := newExecStack(t, app, "grace", capable)

		code, body := postExec(t, mux, stack.Id, map[string]any{
			"container_id":    "c-web",
			"cmd":             []string{"sleep", "60"},
			"timeout_seconds": 20,
		})
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %#v", code, body)
		}
		if body["timed_out"] != true {
			t.Errorf("expected timed_out to reach the caller, got %#v", body)
		}
		// The server's clock starts first and the worker spends time on its
		// ownership checks before its own starts, so an equal deadline would
		// always expire first and turn a timed-out run into a 503.
		if dispatcher.deadlineIn <= 20*time.Second {
			t.Errorf("dispatch deadline = %s, want more than the worker's 20s", dispatcher.deadlineIn)
		}
	})

	t.Run("refuses a worker without the capability", func(t *testing.T) {
		dispatcher.reset(protocol.CommandResult{})
		// An old worker never answers MsgExec, so dispatching would just sit
		// there until it timed out. Refuse up front and name the capability.
		stack := newExecStack(t, app, "oldworker", nil)

		code, body := postExec(t, mux, stack.Id, map[string]any{
			"container_id": "c-web",
			"cmd":          []string{"ls"},
		})
		if code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %#v", code, body)
		}
		if body["required_capability"] != protocol.CapabilityExecOneShotV1 {
			t.Errorf("expected the capability named, got %#v", body)
		}
		if dispatcher.calls != 0 {
			t.Errorf("expected no dispatch, got %d", dispatcher.calls)
		}
	})

	t.Run("validates the request", func(t *testing.T) {
		dispatcher.reset(protocol.CommandResult{})
		stack := newExecStack(t, app, "validation", capable)

		for _, tc := range []struct {
			name string
			body map[string]any
		}{
			{name: "no container", body: map[string]any{"cmd": []string{"ls"}}},
			{name: "no cmd", body: map[string]any{"container_id": "c-web"}},
			{name: "empty cmd", body: map[string]any{"container_id": "c-web", "cmd": []string{}}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				code, body := postExec(t, mux, stack.Id, tc.body)
				if code != http.StatusBadRequest {
					t.Fatalf("expected 400, got %d: %#v", code, body)
				}
			})
		}
		if dispatcher.calls != 0 {
			t.Errorf("a rejected request must not reach the worker, got %d dispatches", dispatcher.calls)
		}
	})

	t.Run("maps a worker refusal to forbidden", func(t *testing.T) {
		dispatcher.reset(protocol.CommandResult{
			CommandID: "exec-1",
			Error:     "container has the Docker socket mounted — terminal exec is blocked for containers with host Docker access",
		})
		stack := newExecStack(t, app, "refusal", capable)

		// The worker's host-escape gate is not a server error and not
		// something the caller can retry — it is a refusal.
		code, body := postExec(t, mux, stack.Id, map[string]any{
			"container_id": "c-web",
			"cmd":          []string{"ls"},
		})
		if code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %#v", code, body)
		}
	})

	t.Run("is audited with the argv", func(t *testing.T) {
		execResult, _ := json.Marshal(protocol.ExecResult{Stdout: "", ExitCode: 0})
		dispatcher.reset(protocol.CommandResult{CommandID: "exec-1", Output: string(execResult)})
		stack := newExecStack(t, app, "audit", capable)

		if code, body := postExec(t, mux, stack.Id, map[string]any{
			"container_id": "c-web",
			"cmd":          []string{"/bin/sh", "-c", "rm -rf /tmp/x"},
		}); code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %#v", code, body)
		}

		// The pty path records everything in a session transcript; a one-shot
		// exec must not be the quieter way to run the same command.
		records, err := app.FindAllRecords("audit_logs")
		if err != nil {
			t.Fatalf("reading audit logs: %v", err)
		}
		var found bool
		for _, rec := range records {
			if rec.GetString("action") == "stack.exec" && rec.GetString("resource_id") == stack.Id {
				found = true
				if metadata := rec.GetString("metadata_json"); !strings.Contains(metadata, "rm -rf /tmp/x") {
					t.Errorf("expected the argv in the audit metadata, got %s", metadata)
				}
			}
		}
		if !found {
			t.Error("expected a stack.exec audit entry")
		}
	})
}
