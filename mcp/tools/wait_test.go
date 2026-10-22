package tools

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	// logStatus is what the sync_logs listing reports, which is what the
	// waiter falls back to when it observed no status frame. A reconcile
	// still in flight has a "running" row, not a finished one.
	logStatus string
	hang      bool
	// endStream closes the stream after its frames instead of holding it
	// open, standing in for a server restart or an idle proxy cut.
	endStream bool
	// frameLogID is the record id the status frames carry. The sync_logs
	// listing answers with priorLogID until the trigger lands and with
	// frameLogID after, mirroring the reconciler writing one row per
	// reconcile.
	frameLogID string
	priorLogID string
}

func newWaitServer(t *testing.T) *waitServer {
	t.Helper()

	ws := &waitServer{
		statuses:    []string{"running", "success"},
		stackStatus: "active",
		logOutput:   "[compose_up] done",
		logStatus:   "success",
		frameLogID:  "synclog00000002",
		priorLogID:  "synclog00000001",
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/custom/stacks/stack0000000001/stream", func(w http.ResponseWriter, r *http.Request) {
		ws.mu.Lock()
		ws.streams++
		ws.historyPara = r.URL.Query().Get("history")
		statuses := append([]string(nil), ws.statuses...)
		hang := ws.hang
		endStream := ws.endStream
		frameLogID := ws.frameLogID
		ws.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// The real route commits its 200 and flushes before any log output, so
		// a subscriber can tell "connected" from "something happened".
		writeFixture(w, ": connected\n\n")
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
			writeFixture(w, fmt.Sprintf("event: status\ndata: {\"record_id\":%q,\"status\":%q}\n\n", frameLogID, status))
			if flusher != nil {
				flusher.Flush()
			}
		}
		if endStream {
			return
		}
		<-r.Context().Done()
	})

	trigger := func(w http.ResponseWriter, r *http.Request) {
		ws.mu.Lock()
		ws.triggered = true
		ws.triggerPath = r.URL.Path
		ws.mu.Unlock()
		writeFixture(w, `{"status":"triggered"}`)
	}
	mux.HandleFunc("/api/custom/stacks/stack0000000001/sync", trigger)
	mux.HandleFunc("/api/custom/stacks/stack0000000001/force-redeploy", trigger)
	mux.HandleFunc("/api/custom/stacks/stack0000000001/rollback", trigger)

	mux.HandleFunc("/api/collections/stacks/records/stack0000000001", func(w http.ResponseWriter, r *http.Request) {
		ws.mu.Lock()
		status := ws.stackStatus
		ws.mu.Unlock()
		writeFixture(w, fmt.Sprintf(`{"status":%q,"deployed_version":7}`, status))
	})
	mux.HandleFunc("/api/collections/sync_logs/records", func(w http.ResponseWriter, r *http.Request) {
		ws.mu.Lock()
		output := ws.logOutput
		status := ws.logStatus
		id := ws.priorLogID
		if ws.triggered {
			// The reconciler writes a new row per reconcile, so the newest row
			// changes once the trigger lands. The waiter reads this route once
			// before triggering to tell the two apart.
			id = ws.frameLogID
		}
		ws.mu.Unlock()
		writeFixture(w, fmt.Sprintf(`{"items":[{"id":%q,"status":%q,"output":%q}]}`, id, status, output))
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
	// "error", not "failed": these are the values the sync_logs status field
	// actually allows (pb_migrations 01/40/43), and a status outside that set
	// would never be recognized as terminal.
	ws.statuses = []string{"running", "error"}
	ws.stackStatus = "error"
	ws.mu.Unlock()

	handler := deployStack(client.New(ws.URL))
	_, out, err := handler(ctxWithKey(), nil, models.DeployStackInput{StackID: "stack0000000001", Wait: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outcome := out.(waitOutcome)
	if outcome.SyncLogStatus != "error" {
		t.Fatalf("sync_log_status = %q, want error", outcome.SyncLogStatus)
	}
	if outcome.Notice == "" {
		t.Error("expected a notice pointing at the diagnosis tools")
	}
}

func TestDeployStackWaitRecognizesEveryTerminalStatus(t *testing.T) {
	// A status the waiter does not recognize as terminal means it waits out
	// its whole timeout and reports timed_out for a reconcile that actually
	// finished. "noop" is the one that matters most: it is what a sync whose
	// rendered output did not change ends as, i.e. the normal outcome of
	// deploy_stack when git has not moved.
	for _, status := range []string{"success", "error", "noop", "degraded"} {
		t.Run(status, func(t *testing.T) {
			ws := newWaitServer(t)
			ws.mu.Lock()
			ws.statuses = []string{"running", status}
			ws.mu.Unlock()

			handler := deployStack(client.New(ws.URL))
			_, out, err := handler(ctxWithKey(), nil, models.DeployStackInput{
				StackID: "stack0000000001", Wait: true, TimeoutSeconds: 5,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			outcome := out.(waitOutcome)
			if outcome.TimedOut {
				t.Fatalf("status %q was not recognized as terminal: %+v", status, outcome)
			}
			if outcome.SyncLogStatus != status {
				t.Fatalf("sync_log_status = %q, want %q", outcome.SyncLogStatus, status)
			}
			// Each non-success outcome has to say what it means, or "noop"
			// reads as a failed deploy and "degraded" as a successful one.
			if status != "success" && outcome.Notice == "" {
				t.Errorf("expected a notice explaining %q", status)
			}
		})
	}
}

func TestDeployStackWaitKeepsWaitingThroughNonTerminalStatuses(t *testing.T) {
	ws := newWaitServer(t)
	ws.mu.Lock()
	// queued = the worker was offline and the reconcile resumes on reconnect;
	// waiting_jobs = paused until in-flight job runs finish. Neither is an
	// outcome, so the wait must run on to the real one.
	ws.statuses = []string{"queued", "waiting_jobs", "running", "success"}
	ws.mu.Unlock()

	handler := deployStack(client.New(ws.URL))
	_, out, err := handler(ctxWithKey(), nil, models.DeployStackInput{
		StackID: "stack0000000001", Wait: true, TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outcome := out.(waitOutcome)
	if outcome.TimedOut || outcome.SyncLogStatus != "success" {
		t.Fatalf("expected the wait to continue to success, got %+v", outcome)
	}
}

func TestDeployStackWaitTimesOut(t *testing.T) {
	ws := newWaitServer(t)
	ws.mu.Lock()
	ws.hang = true
	// The reconcile is still in flight, so its row is not terminal either —
	// a "success" row here would be the previous reconcile's.
	ws.logStatus = "running"
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

func TestDeployStackWaitStreamEndingEarlyIsNotAFailure(t *testing.T) {
	ws := newWaitServer(t)
	ws.mu.Lock()
	// The stream closes cleanly after a non-terminal frame — a server restart
	// or a proxy cutting an idle connection. Client.Stream then returns nil,
	// and no deadline passed, so neither "timed out" nor "failed" is true.
	ws.statuses = []string{"running"}
	ws.endStream = true
	ws.logStatus = "running"
	ws.mu.Unlock()

	handler := deployStack(client.New(ws.URL))
	_, out, err := handler(ctxWithKey(), nil, models.DeployStackInput{
		StackID: "stack0000000001", Wait: true, TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outcome := out.(waitOutcome)

	if outcome.Completed {
		t.Errorf("a non-terminal status is not a completed wait: %+v", outcome)
	}
	if outcome.TimedOut {
		t.Errorf("no deadline passed, so timed_out must be false: %+v", outcome)
	}
	notice := outcome.Notice
	if !strings.Contains(notice, "stream ended") || !strings.Contains(notice, "not a failure") {
		t.Errorf("expected the notice to say the outcome is unknown, got %q", notice)
	}
}

func TestDeployStackWaitFlagsAnAlreadyRunningReconcile(t *testing.T) {
	ws := newWaitServer(t)
	ws.mu.Lock()
	// The terminal frame carries the row that already existed when the
	// trigger was sent: wireops skips a trigger for a stack that is already
	// syncing, so this is probably that reconcile's result, not this call's.
	// The server exposes no per-operation handle, so the ambiguity is
	// surfaced rather than hidden.
	ws.frameLogID = ws.priorLogID
	ws.mu.Unlock()

	handler := deployStack(client.New(ws.URL))
	_, out, err := handler(ctxWithKey(), nil, models.DeployStackInput{
		StackID: "stack0000000001", Wait: true, TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outcome := out.(waitOutcome)

	if !outcome.Completed || outcome.SyncLogStatus != "success" {
		t.Fatalf("expected the observed outcome to still be reported: %+v", outcome)
	}
	if !strings.Contains(outcome.Notice, "already existed when the trigger was sent") {
		t.Errorf("expected the correlation caveat, got %q", outcome.Notice)
	}
}

func TestDeployStackWaitIgnoresThePreviousReconcilesRow(t *testing.T) {
	ws := newWaitServer(t)
	ws.mu.Lock()
	// No frames at all, and the newest row is the one from before the
	// trigger: reporting its "success" would pass the previous reconcile's
	// outcome off as this call's.
	ws.statuses = nil
	ws.endStream = true
	ws.frameLogID = ws.priorLogID
	ws.mu.Unlock()

	handler := deployStack(client.New(ws.URL))
	_, out, err := handler(ctxWithKey(), nil, models.DeployStackInput{
		StackID: "stack0000000001", Wait: true, TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outcome := out.(waitOutcome)

	if outcome.Completed || outcome.SyncLogStatus != "" {
		t.Fatalf("expected no outcome to be adopted, got %+v", outcome)
	}
	// The log output is still handed over — it is the only evidence available
	// — but not as a verdict.
	if outcome.Output == "" {
		t.Error("expected the deploy log to still be reported")
	}
}
