package tools

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

// waitServer fakes the four routes a waited deploy touches: the SSE stream,
// the trigger, the stack record and the sync_logs listing. The stream only
// publishes its status frames once the trigger has been POSTed, which is what
// makes the subscribe-before-trigger ordering observable — a waiter that
// triggers first would be served frames it then has to race for.
type waitServer struct {
	*httptest.Server

	mu          sync.Mutex
	triggered   bool
	triggerPath string
	historyPara string
	streams     int

	statuses    []string
	stackStatus string
	logOutput   string
	hang        bool
}

func newWaitServer(t *testing.T) *waitServer {
	t.Helper()

	ws := &waitServer{
		statuses:    []string{"running", "success"},
		stackStatus: "active",
		logOutput:   "[compose_up] done",
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/custom/stacks/stack0000000001/stream", func(w http.ResponseWriter, r *http.Request) {
		ws.mu.Lock()
		ws.streams++
		ws.historyPara = r.URL.Query().Get("history")
		statuses := append([]string(nil), ws.statuses...)
		hang := ws.hang
		ws.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// The real route commits its 200 and flushes before any log output, so
		// a subscriber can tell "connected" from "something happened".
		fmt.Fprint(w, ": connected\n\n")
		if flusher != nil {
			flusher.Flush()
		}

		deadline := time.Now().Add(5 * time.Second)
		for !ws.wasTriggered() && time.Now().Before(deadline) {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
		if hang {
			<-r.Context().Done()
			return
		}
		for _, status := range statuses {
			fmt.Fprintf(w, "event: status\ndata: {\"record_id\":\"synclog00000001\",\"status\":%q}\n\n", status)
			if flusher != nil {
				flusher.Flush()
			}
		}
		<-r.Context().Done()
	})

	trigger := func(w http.ResponseWriter, r *http.Request) {
		ws.mu.Lock()
		ws.triggered = true
		ws.triggerPath = r.URL.Path
		ws.mu.Unlock()
		w.Write([]byte(`{"status":"triggered"}`))
	}
	mux.HandleFunc("/api/custom/stacks/stack0000000001/sync", trigger)
	mux.HandleFunc("/api/custom/stacks/stack0000000001/force-redeploy", trigger)
	mux.HandleFunc("/api/custom/stacks/stack0000000001/rollback", trigger)

	mux.HandleFunc("/api/collections/stacks/records/stack0000000001", func(w http.ResponseWriter, r *http.Request) {
		ws.mu.Lock()
		status := ws.stackStatus
		ws.mu.Unlock()
		fmt.Fprintf(w, `{"status":%q,"deployed_version":7}`, status)
	})
	mux.HandleFunc("/api/collections/sync_logs/records", func(w http.ResponseWriter, r *http.Request) {
		ws.mu.Lock()
		output := ws.logOutput
		ws.mu.Unlock()
		fmt.Fprintf(w, `{"items":[{"id":"synclog00000001","status":"success","output":%q}]}`, output)
	})

	ws.Server = httptest.NewServer(mux)
	t.Cleanup(ws.Close)
	return ws
}

func (ws *waitServer) wasTriggered() bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.triggered
}

func TestDeployStackWaitReportsTheOutcome(t *testing.T) {
	ws := newWaitServer(t)

	handler := deployStack(client.New(ws.URL))
	_, out, err := handler(ctxWithKey(), nil, models.DeployStackInput{StackID: "stack0000000001", Wait: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outcome, ok := out.(waitOutcome)
	if !ok {
		t.Fatalf("expected a waitOutcome, got %T", out)
	}
	if !outcome.Waited || outcome.TimedOut {
		t.Fatalf("expected a completed wait, got %+v", outcome)
	}
	if outcome.SyncLogStatus != "success" {
		t.Errorf("sync_log_status = %q, want success", outcome.SyncLogStatus)
	}
	if outcome.StackStatus != "active" {
		t.Errorf("stack_status = %q, want active", outcome.StackStatus)
	}
	if outcome.Output != "[compose_up] done" {
		t.Errorf("expected the deploy log, got %q", outcome.Output)
	}
	if outcome.Notice != "" {
		t.Errorf("a successful deploy needs no notice, got %q", outcome.Notice)
	}
	// The whole point of the stream is not replaying a stack's log history
	// before the part being waited for.
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.historyPara != "false" {
		t.Errorf("expected history=false on the watch, got %q", ws.historyPara)
	}
	if ws.triggerPath != "/api/custom/stacks/stack0000000001/sync" {
		t.Errorf("unexpected trigger path %q", ws.triggerPath)
	}
}

func TestDeployStackWaitSubscribesBeforeTriggering(t *testing.T) {
	ws := newWaitServer(t)
	// The fake stream withholds every frame until the trigger arrives, so a
	// completed wait proves the subscription was already open — the trigger
	// routes are fire-and-forget and a frame published in the gap is gone.
	handler := deployStack(client.New(ws.URL))
	if _, _, err := handler(ctxWithKey(), nil, models.DeployStackInput{StackID: "stack0000000001", Wait: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.streams != 1 {
		t.Errorf("expected exactly one stream connection, got %d", ws.streams)
	}
}

func TestDeployStackWaitOnFailureNamesTheNextStep(t *testing.T) {
	ws := newWaitServer(t)
	ws.mu.Lock()
	ws.statuses = []string{"running", "failed"}
	ws.stackStatus = "error"
	ws.mu.Unlock()

	handler := deployStack(client.New(ws.URL))
	_, out, err := handler(ctxWithKey(), nil, models.DeployStackInput{StackID: "stack0000000001", Wait: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outcome := out.(waitOutcome)
	if outcome.SyncLogStatus != "failed" {
		t.Fatalf("sync_log_status = %q, want failed", outcome.SyncLogStatus)
	}
	if outcome.Notice == "" {
		t.Error("expected a notice pointing at the diagnosis tools")
	}
}

func TestDeployStackWaitTimesOut(t *testing.T) {
	ws := newWaitServer(t)
	ws.mu.Lock()
	ws.hang = true
	ws.mu.Unlock()

	handler := deployStack(client.New(ws.URL))
	_, out, err := handler(ctxWithKey(), nil, models.DeployStackInput{
		StackID: "stack0000000001", Wait: true, TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outcome := out.(waitOutcome)
	if !outcome.TimedOut {
		t.Fatalf("expected timed_out, got %+v", outcome)
	}
	// A timeout still reports what is known, and says the reconcile may be
	// running — not that it failed.
	if outcome.StackStatus == "" {
		t.Error("expected the stack status to still be read after a timeout")
	}
	if outcome.Notice == "" {
		t.Error("expected a notice explaining the timeout")
	}
	if !ws.wasTriggered() {
		t.Error("the deploy must still have been triggered")
	}
}

func TestDeployStackWithoutWaitDoesNotStream(t *testing.T) {
	ws := newWaitServer(t)

	handler := deployStack(client.New(ws.URL))
	_, out, err := handler(ctxWithKey(), nil, models.DeployStackInput{StackID: "stack0000000001"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Unchanged default: the raw {"status":"triggered"} body, no stream.
	if _, isOutcome := out.(waitOutcome); isOutcome {
		t.Fatalf("expected the raw trigger response, got a waitOutcome")
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.streams != 0 {
		t.Errorf("expected no stream connection without wait, got %d", ws.streams)
	}
}

func TestClampWaitTimeout(t *testing.T) {
	if got := clampWaitTimeout(0); got != waitDefaultTimeout {
		t.Errorf("unset should default, got %v", got)
	}
	if got := clampWaitTimeout(-5); got != waitDefaultTimeout {
		t.Errorf("negative should default, got %v", got)
	}
	if got := clampWaitTimeout(30); got != 30*time.Second {
		t.Errorf("30 should pass through, got %v", got)
	}
	if got := clampWaitTimeout(100000); got != waitMaxTimeout {
		t.Errorf("oversized should clamp, got %v", got)
	}
}
