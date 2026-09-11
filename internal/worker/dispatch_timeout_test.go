package worker

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wireops/wireops/internal/protocol"
)

// TestDispatchRespectsLongerCallerDeadlineOverFallback is a regression test
// for a bug where Dispatch's internal "no deadline" fallback fired
// regardless of a caller-provided ctx deadline, silently truncating any
// deploy dispatch configured to run longer than the fallback (e.g. a large
// pull_timeout). The fallback is shrunk well below the caller's own
// deadline so a regression would make Dispatch return early instead of
// requiring the test to wait out a real 5 minutes.
func TestDispatchRespectsLongerCallerDeadlineOverFallback(t *testing.T) {
	app := newWorkerTestApp(t)
	svc := NewService(app)
	server := NewWorkerServer(app, svc)
	httpServer := httptest.NewServer(server.engine)
	defer httpServer.Close()

	token, _, err := svc.IssueToken("admin-1")
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	dialWorker(t, httpServer.URL, token)
	workerID := workerIDFromConnection(t, server)

	orig := dispatchNoDeadlineFallback
	dispatchNoDeadlineFallback = 50 * time.Millisecond
	t.Cleanup(func() { dispatchNoDeadlineFallback = orig })

	callerDeadline := 300 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), callerDeadline)
	defer cancel()

	start := time.Now()
	_, dispatchErr := server.Dispatch(ctx, workerID, protocol.DeployCommand{
		CommandID: "cmd-long-deadline-1",
		StackID:   "stack-1",
	})
	elapsed := time.Since(start)

	if !errors.Is(dispatchErr, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", dispatchErr)
	}
	if elapsed < callerDeadline-100*time.Millisecond {
		t.Fatalf("Dispatch returned after %v, want it to honor the caller's ~%v deadline instead of the shrunk %v fallback", elapsed, callerDeadline, dispatchNoDeadlineFallback)
	}
}

// TestDispatchUsesFallbackWhenCtxHasNoDeadline confirms the fallback still
// bounds Dispatch calls whose ctx carries no deadline of its own.
func TestDispatchUsesFallbackWhenCtxHasNoDeadline(t *testing.T) {
	app := newWorkerTestApp(t)
	svc := NewService(app)
	server := NewWorkerServer(app, svc)
	httpServer := httptest.NewServer(server.engine)
	defer httpServer.Close()

	token, _, err := svc.IssueToken("admin-1")
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	dialWorker(t, httpServer.URL, token)
	workerID := workerIDFromConnection(t, server)

	orig := dispatchNoDeadlineFallback
	dispatchNoDeadlineFallback = 50 * time.Millisecond
	t.Cleanup(func() { dispatchNoDeadlineFallback = orig })

	start := time.Now()
	_, dispatchErr := server.Dispatch(context.Background(), workerID, protocol.DeployCommand{
		CommandID: "cmd-no-deadline-1",
		StackID:   "stack-1",
	})
	elapsed := time.Since(start)

	if dispatchErr == nil || !strings.Contains(dispatchErr.Error(), "timed out waiting for worker") {
		t.Fatalf("expected fallback timeout error, got %v", dispatchErr)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Dispatch took %v, want it bounded by the shrunk %v fallback", elapsed, dispatchNoDeadlineFallback)
	}
}
