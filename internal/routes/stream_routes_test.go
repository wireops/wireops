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

// createStreamStack creates a stack with one finished sync_logs row, the
// historical output a reconnecting client would otherwise be replayed.
func createStreamStack(t *testing.T, app core.App, name string) (stackID, oldLogID string) {
	t.Helper()

	stacks, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		t.Fatalf("find stacks collection: %v", err)
	}
	stack := core.NewRecord(stacks)
	stack.Set("name", name)
	if err := app.Save(stack); err != nil {
		t.Fatalf("create stack: %v", err)
	}

	old := createSyncLog(t, app, stack.Id, "success", "[compose_up] old deploy line")
	return stack.Id, old.Id
}

func createSyncLog(t *testing.T, app core.App, stackID, status, output string) *core.Record {
	t.Helper()

	syncLogs, err := app.FindCollectionByNameOrId("sync_logs")
	if err != nil {
		t.Fatalf("find sync_logs collection: %v", err)
	}
	rec := core.NewRecord(syncLogs)
	rec.Set("stack", stackID)
	rec.Set("status", status)
	rec.Set("output", output)
	if err := app.Save(rec); err != nil {
		t.Fatalf("create sync log: %v", err)
	}
	return rec
}

// streamUntil serves the stream request in the background and waits for want
// to appear in the response, running onTick on every poll with whatever has
// been written so far (e.g. to publish a broker event — the handler subscribes
// itself, so an event published before that would be dropped; both apply and
// applyStatus ignore a repeat, so retrying is free). It returns everything
// written.
func streamUntil(t *testing.T, mux http.Handler, url, want string, onTick func(body string)) string {
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
		body := w.snapshot()
		if onTick != nil {
			onTick(body)
		}
		if strings.Contains(body, want) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	body := w.snapshot()
	cancel()
	<-done
	return body
}

// TestStackStream covers GET /api/custom/stacks/{id}/stream as subtests
// sharing one app/mux bootstrap, each with its own stack and log rows — one
// PocketBase spin-up per case costs about a second and this package's suite is
// already close to go test's per-package timeout (see AGENTS.md § Testing &
// Coverage).
func TestStackStream(t *testing.T) {
	// Shrink the recovery poll so the dropped-event subtest does not wait out
	// the production interval.
	original := statusRecoveryInterval
	statusRecoveryInterval = 20 * time.Millisecond
	t.Cleanup(func() { statusRecoveryInterval = original })

	app, mux, broker := setupStreamTestApp(t)

	t.Run("EmitsStatusEventOnTransition", func(t *testing.T) {
		stackID, _ := createStreamStack(t, app, "stream-transition")

		ev := logstream.Event{RecordID: "live-record-1", Output: "[compose_up] pulling", Status: "running"}
		body := streamUntil(t, mux, "/api/custom/stacks/"+stackID+"/stream", "event: status",
			func(string) { broker.Publish(stackID, ev) })

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
	})

	t.Run("DoesNotReplayStoredStatus", func(t *testing.T) {
		stackID, oldLogID := createStreamStack(t, app, "stream-stored-status")

		// The stored row is already "success". A client that connects now and
		// waits for a reconcile must not be handed that as the outcome, so
		// neither the snapshot nor the recovery poll may announce it.
		ev := logstream.Event{RecordID: "live-record-2", Output: "[compose_up] starting", Status: "running"}
		body := streamUntil(t, mux, "/api/custom/stacks/"+stackID+"/stream", "event: status",
			func(string) { broker.Publish(stackID, ev) })

		if strings.Contains(body, oldLogID) {
			t.Errorf("stored sync log %q should not produce a status frame:\n%s", oldLogID, body)
		}
		if !strings.Contains(body, "\"status\":\"running\"") {
			t.Errorf("expected the live status frame:\n%s", body)
		}
	})

	t.Run("RecoversAStatusWhoseEventWasDropped", func(t *testing.T) {
		stackID, _ := createStreamStack(t, app, "stream-recovery")

		// Nothing is published at all, standing in for the broker dropping
		// the event (logstream.Publish skips a subscriber whose 16-slot
		// buffer is full, and a deploy publishes one event per output line).
		// The poll re-reads the newest row, so a completion arrives late
		// rather than never.
		var created bool
		body := streamUntil(t, mux, "/api/custom/stacks/"+stackID+"/stream", "event: status", func(body string) {
			// Only after the handler has written its snapshot: a row created
			// before that lands *in* the snapshot, whose status is seeded
			// silently on purpose, which is a different case (and the one
			// DoesNotReplayStoredStatus covers).
			if !created && strings.Contains(body, "old deploy line") {
				created = true
				createSyncLog(t, app, stackID, "error", "[compose_up] boom")
			}
		})

		if !strings.Contains(body, "\"status\":\"error\"") {
			t.Fatalf("expected the recovered terminal status:\n%s", body)
		}
		if got := strings.Count(body, "event: status"); got != 1 {
			t.Errorf("the poll must not repeat an unchanged status, got %d frames:\n%s", got, body)
		}
	})

	t.Run("HistoryFalseSkipsStoredOutput", func(t *testing.T) {
		stackID, _ := createStreamStack(t, app, "stream-no-history")

		ev := logstream.Event{RecordID: "live-record-3", Output: "[compose_up] fresh line", Status: "running"}
		body := streamUntil(t, mux, "/api/custom/stacks/"+stackID+"/stream?history=false", "fresh line",
			func(string) { broker.Publish(stackID, ev) })

		if strings.Contains(body, "old deploy line") {
			t.Errorf("history=false must not replay stored output:\n%s", body)
		}
		if !strings.Contains(body, "data: [compose_up] fresh line") {
			t.Errorf("expected the live line:\n%s", body)
		}
	})

	t.Run("DefaultsToReplayingHistory", func(t *testing.T) {
		stackID, _ := createStreamStack(t, app, "stream-history")

		ev := logstream.Event{RecordID: "live-record-4", Output: "[compose_up] later line", Status: "running"}
		body := streamUntil(t, mux, "/api/custom/stacks/"+stackID+"/stream", "later line",
			func(string) { broker.Publish(stackID, ev) })

		if !strings.Contains(body, "data: [compose_up] old deploy line") {
			t.Errorf("expected stored output to be replayed by default:\n%s", body)
		}
	})
}

func TestStreamHandoffStateStatusTracking(t *testing.T) {
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

	// A status seeded from the snapshot is known but never announced, which is
	// what keeps a finished deploy from being replayed as a fresh outcome.
	state.observeStatus("c", "success")
	if _, changed := state.applyStatus(logstream.Event{RecordID: "c", Status: "success"}); changed {
		t.Error("a seeded status must not be reported")
	}
	if _, changed := state.applyStatus(logstream.Event{RecordID: "c", Status: "running"}); !changed {
		t.Error("a seeded record still reports a later change")
	}
}
