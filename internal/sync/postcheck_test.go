package sync

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/protocol"
)

func TestEvaluatePostCheckAllHealthy(t *testing.T) {
	expected := []string{"web", "db"}
	statuses := []compose.ServiceStatus{
		{ServiceName: "web", Status: "running", Health: "healthy"},
		{ServiceName: "db", Status: "running", Health: "none"},
	}

	res := evaluatePostCheck(expected, statuses)
	if res.Status != "active" {
		t.Fatalf("status = %q, want active; detail=%s", res.Status, res.Detail)
	}
}

func TestEvaluatePostCheckMissingContainer(t *testing.T) {
	expected := []string{"web", "db"}
	statuses := []compose.ServiceStatus{
		{ServiceName: "web", Status: "running", Health: "none"},
		// db never came up at all.
	}

	res := evaluatePostCheck(expected, statuses)
	if res.Status != "degraded" {
		t.Fatalf("status = %q, want degraded; detail=%s", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, "db") {
		t.Fatalf("detail = %q, want it to mention missing service db", res.Detail)
	}
}

func TestEvaluatePostCheckAllMissingIsError(t *testing.T) {
	expected := []string{"web", "db"}
	statuses := []compose.ServiceStatus{
		{ServiceName: "web", Status: "exited", Health: "none"},
		{ServiceName: "db", Status: "exited", Health: "none"},
	}

	res := evaluatePostCheck(expected, statuses)
	if res.Status != "error" {
		t.Fatalf("status = %q, want error; detail=%s", res.Status, res.Detail)
	}
}

func TestEvaluatePostCheckUnhealthyIsDegraded(t *testing.T) {
	expected := []string{"web", "db"}
	statuses := []compose.ServiceStatus{
		{ServiceName: "web", Status: "running", Health: "healthy"},
		{ServiceName: "db", Status: "running", Health: "unhealthy"},
	}

	res := evaluatePostCheck(expected, statuses)
	if res.Status != "degraded" {
		t.Fatalf("status = %q, want degraded; detail=%s", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, "unhealthy") || !strings.Contains(res.Detail, "db") {
		t.Fatalf("detail = %q, want it to call out db as unhealthy", res.Detail)
	}
}

func TestEvaluatePostCheckRestartLoopIsDegraded(t *testing.T) {
	expected := []string{"web"}
	statuses := []compose.ServiceStatus{
		{
			ServiceName:  "web",
			Status:       "running",
			Health:       "none",
			RestartCount: 5,
			StartedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		},
	}

	res := evaluatePostCheck(expected, statuses)
	if res.Status != "error" {
		// With only one expected service and it looping, 0/1 are "ok" -> error.
		t.Fatalf("status = %q, want error (single looping service); detail=%s", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, "restart looping") {
		t.Fatalf("detail = %q, want it to mention restart looping", res.Detail)
	}
}

func TestEvaluatePostCheckOldRestartsDoNotCountAsLooping(t *testing.T) {
	expected := []string{"web"}
	statuses := []compose.ServiceStatus{
		{
			ServiceName:  "web",
			Status:       "running",
			Health:       "none",
			RestartCount: 5,
			StartedAt:    time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339Nano),
		},
	}

	res := evaluatePostCheck(expected, statuses)
	if res.Status != "active" {
		t.Fatalf("status = %q, want active (restarts are old, container is stable now); detail=%s", res.Status, res.Detail)
	}
}

func TestEvaluatePostCheckNoHealthcheckDefined(t *testing.T) {
	expected := []string{"web"}
	statuses := []compose.ServiceStatus{
		{ServiceName: "web", Status: "running", Health: "none"},
	}

	res := evaluatePostCheck(expected, statuses)
	if res.Status != "active" {
		t.Fatalf("status = %q, want active (no healthcheck defined should not block)", res.Status)
	}
}

func TestEvaluatePostCheckOrphanServiceIgnored(t *testing.T) {
	// A container running that isn't in the expected list (e.g. an unrelated
	// leftover) must not affect the outcome for the services we do expect.
	expected := []string{"web"}
	statuses := []compose.ServiceStatus{
		{ServiceName: "web", Status: "running", Health: "healthy"},
		{ServiceName: "orphan", Status: "running", Health: "healthy"},
	}

	res := evaluatePostCheck(expected, statuses)
	if res.Status != "active" {
		t.Fatalf("status = %q, want active", res.Status)
	}
}

// fakeDispatcher implements WorkerDispatcher for postDeployCheck tests.
type fakeDispatcher struct {
	connected bool
	results   []protocol.CommandResult
	errs      []error
	calls     int
}

func (f *fakeDispatcher) Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error) {
	idx := f.calls
	f.calls++
	if idx >= len(f.results) {
		idx = len(f.results) - 1
	}
	var err error
	if idx < len(f.errs) {
		err = f.errs[idx]
	}
	return f.results[idx], err
}

func (f *fakeDispatcher) IsConnected(workerID string) bool {
	return f.connected
}

func statusResult(statuses []compose.ServiceStatus) protocol.CommandResult {
	encoded, _ := json.Marshal(statuses)
	return protocol.CommandResult{Output: string(encoded)}
}

const testCompose = `services:
  web:
    image: nginx
  db:
    image: postgres
`

func TestPostDeployCheckReturnsActiveWhenAllHealthyOnFirstTry(t *testing.T) {
	r := &Reconciler{
		dispatcher: &fakeDispatcher{
			connected: true,
			results: []protocol.CommandResult{
				statusResult([]compose.ServiceStatus{
					{ServiceName: "web", Status: "running", Health: "healthy"},
					{ServiceName: "db", Status: "running", Health: "healthy"},
				}),
			},
		},
	}

	res := r.postDeployCheck(context.Background(), "worker-1", "stack-1", "/tmp/workdir", []byte(testCompose))
	if res.Status != "active" {
		t.Fatalf("status = %q, want active; detail=%s", res.Status, res.Detail)
	}
}

func TestPostDeployCheckSkipsWhenComposeUnparseable(t *testing.T) {
	r := &Reconciler{dispatcher: &fakeDispatcher{connected: true}}

	res := r.postDeployCheck(context.Background(), "worker-1", "stack-1", "/tmp/workdir", []byte("not: [valid"))
	if res.Status != "active" {
		t.Fatalf("status = %q, want active (fallback when compose can't be parsed)", res.Status)
	}
	if !strings.Contains(res.Detail, "skipped") {
		t.Fatalf("detail = %q, want it to say the check was skipped", res.Detail)
	}
}

func TestPostDeployCheckFallsBackToActiveWhenStatusQueryFails(t *testing.T) {
	orig := postCheckInterval
	postCheckInterval = 10 * time.Millisecond
	defer func() { postCheckInterval = orig }()

	r := &Reconciler{
		dispatcher: &fakeDispatcher{
			connected: true,
			results:   []protocol.CommandResult{{}, {}, {}},
			errs: []error{
				context.DeadlineExceeded,
				context.DeadlineExceeded,
				context.DeadlineExceeded,
			},
		},
	}

	start := time.Now()
	res := r.postDeployCheck(context.Background(), "worker-1", "stack-1", "/tmp/workdir", []byte(testCompose))
	if res.Status != "active" {
		t.Fatalf("status = %q, want active (deploy itself already succeeded, only the status query failed)", res.Status)
	}
	if !strings.Contains(res.Detail, "could not query worker status") {
		t.Fatalf("detail = %q, want it to explain the status query failure", res.Detail)
	}
	if elapsed := time.Since(start); elapsed < 2*postCheckInterval {
		t.Fatalf("expected retries to actually wait between attempts, elapsed=%s", elapsed)
	}
}

func TestPostDeployCheckDegradedWhenOneServiceMissing(t *testing.T) {
	orig := postCheckInterval
	postCheckInterval = 10 * time.Millisecond
	defer func() { postCheckInterval = orig }()

	r := &Reconciler{
		dispatcher: &fakeDispatcher{
			connected: true,
			results: []protocol.CommandResult{
				statusResult([]compose.ServiceStatus{
					{ServiceName: "web", Status: "running", Health: "healthy"},
				}),
				statusResult([]compose.ServiceStatus{
					{ServiceName: "web", Status: "running", Health: "healthy"},
				}),
				statusResult([]compose.ServiceStatus{
					{ServiceName: "web", Status: "running", Health: "healthy"},
				}),
			},
		},
	}

	res := r.postDeployCheck(context.Background(), "worker-1", "stack-1", "/tmp/workdir", []byte(testCompose))
	if res.Status != "degraded" {
		t.Fatalf("status = %q, want degraded; detail=%s", res.Status, res.Detail)
	}
}
