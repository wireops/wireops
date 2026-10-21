package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	_ "github.com/wireops/wireops/pb_migrations"

	"github.com/wireops/wireops/internal/logstream"
)

// syncWriter is a ResponseWriter a test can read while the SSE handler is
// still writing to it. httptest.ResponseRecorder is not safe for that, and the
// stream handler only returns when the request context is cancelled.
type syncWriter struct {
	mu     sync.Mutex
	header http.Header
	body   strings.Builder
	code   int
}

func newSyncWriter() *syncWriter {
	return &syncWriter{header: make(http.Header)}
}

func (w *syncWriter) Header() http.Header { return w.header }

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(p)
}

func (w *syncWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.code = code
}

func (w *syncWriter) Flush() {}

func (w *syncWriter) snapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

// setupStreamTestApp registers GET /api/custom/stacks/{id}/stream against a
// real broker, authenticated as an admin so rbac.Require(CapViewLogs) passes.
func setupStreamTestApp(t *testing.T) (core.App, http.Handler, *logstream.Broker) {
	t.Helper()

	app := newSetupTestApp(t)
	admin := createTestUser(t, app, "admin-stream@example.com", "password123", "admin")
	broker := logstream.New()

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
	rr := routeRegistrar{r: r, app: app, logBroker: broker}
	rr.registerStreamRoutes()

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux, broker
}

// createStreamFixture creates a stack with one finished sync_logs row, the
// historical output a reconnecting client would otherwise be replayed.
func createStreamFixture(t *testing.T, app core.App) (stackID, oldLogID string) {
	t.Helper()

	stacks, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatalf("find stacks collection: %v", err)
	}
	stack := core.NewRecord(stacks)
	stack.Set("name", "stream-fixture")
	if err := app.Save(stack); err != nil {
		t.Fatalf("create stack: %v", err)
	}

	syncLogs, err := app.FindCollectionByNameOrId("sync_logs")
	if err != nil {
		t.Fatalf("find sync_logs collection: %v", err)
	}
	old := core.NewRecord(syncLogs)
	old.Set("stack", stack.Id)
	old.Set("status", "success")
	old.Set("output", "[compose_up] old deploy line")
	if err := app.Save(old); err != nil {
		t.Fatalf("create sync log: %v", err)
	}
	return stack.Id, old.Id
}

// streamUntil serves the stream request in the background, republishing ev
// until want shows up in the response (the handler subscribes to the broker
// itself, so an event published before that would be dropped; both apply and
// applyStatus ignore a repeat, so retrying is free). It returns everything
// written.
func streamUntil(t *testing.T, mux http.Handler, url string, broker *logstream.Broker, stackID string, ev logstream.Event, want string) string {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := newSyncWriter()
	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil).WithContext(ctx))
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		broker.Publish(stackID, ev)
		if strings.Contains(w.snapshot(), want) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	body := w.snapshot()
	cancel()
	<-done
	return body
}

func TestStackStreamEmitsStatusEventOnTransition(t *testing.T) {
	app, mux, broker := setupStreamTestApp(t)
	stackID, _ := createStreamFixture(t, app)

	ev := logstream.Event{RecordID: "live-record-1", Output: "[compose_up] pulling", Status: "running"}
	body := streamUntil(t, mux, "/api/custom/stacks/"+stackID+"/stream", broker, stackID, ev, "event: status")

	wantStatus := "event: status\ndata: {\"record_id\":\"live-record-1\",\"status\":\"running\"}\n\n"
	if !strings.Contains(body, wantStatus) {
		t.Fatalf("expected a status frame %q in:\n%s", wantStatus, body)
	}
	if !strings.Contains(body, "data: [compose_up] pulling") {
		t.Errorf("expected the output line to still be streamed as an unnamed frame:\n%s", body)
	}
	// One transition, one frame: a repeated event must not re-announce it.
	if got := strings.Count(body, "event: status"); got != 1 {
		t.Errorf("expected exactly 1 status frame, got %d:\n%s", got, body)
	}
}

func TestStackStreamDoesNotReplayStoredStatus(t *testing.T) {
	app, mux, broker := setupStreamTestApp(t)
	stackID, oldLogID := createStreamFixture(t, app)

	// The stored row is already "success". A client that connects now and waits
	// for a reconcile must not be handed that as the outcome, so the snapshot
	// contributes no status frame — only the live event does.
	ev := logstream.Event{RecordID: "live-record-2", Output: "[compose_up] starting", Status: "running"}
	body := streamUntil(t, mux, "/api/custom/stacks/"+stackID+"/stream", broker, stackID, ev, "event: status")

	if strings.Contains(body, oldLogID) {
		t.Errorf("stored sync log %q should not produce a status frame:\n%s", oldLogID, body)
	}
	if !strings.Contains(body, "\"status\":\"running\"") {
		t.Errorf("expected the live status frame:\n%s", body)
	}
}

func TestStackStreamHistoryFalseSkipsStoredOutput(t *testing.T) {
	app, mux, broker := setupStreamTestApp(t)
	stackID, _ := createStreamFixture(t, app)

	ev := logstream.Event{RecordID: "live-record-3", Output: "[compose_up] fresh line", Status: "running"}
	body := streamUntil(t, mux, "/api/custom/stacks/"+stackID+"/stream?history=false", broker, stackID, ev, "fresh line")

	if strings.Contains(body, "old deploy line") {
		t.Errorf("history=false must not replay stored output:\n%s", body)
	}
	if !strings.Contains(body, "data: [compose_up] fresh line") {
		t.Errorf("expected the live line:\n%s", body)
	}
}

func TestStackStreamDefaultsToReplayingHistory(t *testing.T) {
	app, mux, broker := setupStreamTestApp(t)
	stackID, _ := createStreamFixture(t, app)

	ev := logstream.Event{RecordID: "live-record-4", Output: "[compose_up] later line", Status: "running"}
	body := streamUntil(t, mux, "/api/custom/stacks/"+stackID+"/stream", broker, stackID, ev, "later line")

	if !strings.Contains(body, "data: [compose_up] old deploy line") {
		t.Errorf("expected stored output to be replayed by default:\n%s", body)
	}
}

func TestStreamHandoffStateApplyStatus(t *testing.T) {
	state := newStreamHandoffState()

	if _, changed := state.applyStatus(logstream.Event{RecordID: "a", Status: ""}); changed {
		t.Error("an empty status is not a transition")
	}
	status, changed := state.applyStatus(logstream.Event{RecordID: "a", Status: "running"})
	if !changed || status != "running" {
		t.Fatalf("first status: got (%q, %v), want (running, true)", status, changed)
	}
	if _, changed := state.applyStatus(logstream.Event{RecordID: "a", Status: "running"}); changed {
		t.Error("a repeated status is not a transition")
	}
	status, changed = state.applyStatus(logstream.Event{RecordID: "a", Status: "success"})
	if !changed || status != "success" {
		t.Fatalf("terminal status: got (%q, %v), want (success, true)", status, changed)
	}
	// Per-record bookkeeping: another record's identical status still counts.
	status, changed = state.applyStatus(logstream.Event{RecordID: "b", Status: "success"})
	if !changed || status != "success" {
		t.Fatalf("second record: got (%q, %v), want (success, true)", status, changed)
	}
}
