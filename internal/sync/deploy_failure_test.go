package sync

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/wireops/wireops/internal/protocol"
)

// TestDeployFailureReasonClassifiesADispatch pins what lands in
// stacks.last_error_reason. The case that matters for issue #178 is the first
// two: the only evidence a timeout used to leave was `signal: killed` at the
// end of a compose progress log, which an OOM kill produces identically.
func TestDeployFailureReasonClassifiesADispatch(t *testing.T) {
	killed := "docker compose up failed: signal: killed"

	cases := []struct {
		name        string
		result      protocol.CommandResult
		dispatchErr error
		want        string
	}{
		{
			name:   "WorkerReportedItsOwnBudgetExpired",
			result: protocol.CommandResult{Error: killed, TimedOut: true, TimedOutStep: "up"},
			want:   reasonTimeout,
		},
		{
			name:        "ServerSideDeadlineIsAlsoATimeout",
			dispatchErr: context.DeadlineExceeded,
			want:        reasonTimeout,
		},
		{
			name:        "WrappedServerDeadlineIsStillATimeout",
			dispatchErr: fmt.Errorf("dispatch stack-1: %w", context.DeadlineExceeded),
			want:        reasonTimeout,
		},
		{
			// Matched with errors.Is, not on the text: an error that merely
			// reads like a deadline is a dispatch failure.
			name:        "ADeadlineLookalikeStringIsNotATimeout",
			dispatchErr: errors.New("dispatch: " + context.DeadlineExceeded.Error()),
			want:        reasonDispatch,
		},
		{
			name:        "DispatchFailureIsNotATimeout",
			dispatchErr: errors.New("timed out waiting for worker ack"),
			want:        reasonDispatch,
		},
		{
			// An older worker cannot set the flag, so the same kill reads as
			// an ordinary compose failure — exactly today's behaviour.
			name:   "OlderWorkerWithoutTheFlagReadsAsCompose",
			result: protocol.CommandResult{Error: killed},
			want:   reasonCompose,
		},
		{
			name:   "ComposeRejectedTheChange",
			result: protocol.CommandResult{Error: "docker compose up failed: exit status 1"},
			want:   reasonCompose,
		},
		{
			name:   "SuccessHasNoReason",
			result: protocol.CommandResult{Output: "done"},
			want:   "",
		},
		{
			// A dispatch that failed outright carries a zero-value result, so
			// a stale TimedOut must not override the real dispatch error.
			name:        "DispatchErrorWinsOverAResultFlag",
			result:      protocol.CommandResult{TimedOut: true},
			dispatchErr: errors.New("worker offline"),
			want:        reasonDispatch,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deployFailureReason(tc.result, tc.dispatchErr); got != tc.want {
				t.Fatalf("deployFailureReason = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMarkDeployFailureRecordsReasonAndStatus covers the writer the timeout
// path needs: a reason alongside the category, and a status other than the
// "error" markError hardcodes.
func TestMarkDeployFailureRecordsReasonAndStatus(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)
	r := &Reconciler{app: app}

	if err := r.markDeployFailure(stack, reasonTimeout, "degraded", "signal: killed", "commit-a"); err != nil {
		t.Fatalf("markDeployFailure: %v", err)
	}
	refreshed, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]string{
		"last_error_category": "deploy",
		"last_error_reason":   reasonTimeout,
		"last_error_commit":   "commit-a",
		"status":              "degraded",
	} {
		if got := refreshed.GetString(field); got != want {
			t.Fatalf("%s = %q, want %q", field, got, want)
		}
	}

	// clearLastError has to clear the reason too, or a stack that later
	// deploys cleanly keeps reading as having timed out.
	r.clearLastError(refreshed)
	if err := app.Save(refreshed); err != nil {
		t.Fatalf("save cleared error: %v", err)
	}
	refreshed, err = app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := refreshed.GetString("last_error_reason"); got != "" {
		t.Fatalf("last_error_reason after clearLastError = %q, want empty", got)
	}
}

// TestMarkDeployErrorLeavesTheReasonEmpty guards the compatibility half: the
// existing callers kept their signatures and must not start asserting a
// reason they cannot determine.
func TestMarkDeployErrorLeavesTheReasonEmpty(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)
	r := &Reconciler{app: app}

	if err := r.markDeployError(stack, "deploy failed", "commit-a"); err != nil {
		t.Fatalf("markDeployError: %v", err)
	}
	refreshed, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := refreshed.GetString("last_error_reason"); got != "" {
		t.Fatalf("last_error_reason = %q, want empty", got)
	}
	if got := refreshed.GetString("status"); got != "error" {
		t.Fatalf("status = %q, want error", got)
	}
}
