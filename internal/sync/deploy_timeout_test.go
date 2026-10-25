package sync

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/protocol"
)

// convergeDispatcher answers the two different reads convergeAfterTimeout
// makes — the post-check's GetStatusCommand and the commit-label
// InspectCommand — by command type rather than by call order, since the
// post-check's retry count varies with what it finds.
type convergeDispatcher struct {
	statuses  []compose.ServiceStatus
	statusRaw string
	statusErr error

	commitSHA  string
	inspectErr error

	statusCalls  int
	inspectCalls int
}

func (d *convergeDispatcher) Dispatch(_ context.Context, _ string, cmd interface{}) (protocol.CommandResult, error) {
	switch cmd.(type) {
	case protocol.GetStatusCommand:
		d.statusCalls++
		if d.statusErr != nil {
			return protocol.CommandResult{}, d.statusErr
		}
		if d.statusRaw != "" {
			return protocol.CommandResult{Output: d.statusRaw}, nil
		}
		encoded, _ := json.Marshal(d.statuses)
		return protocol.CommandResult{Output: string(encoded)}, nil
	case protocol.InspectCommand:
		d.inspectCalls++
		if d.inspectErr != nil {
			return protocol.CommandResult{}, d.inspectErr
		}
		encoded, _ := json.Marshal(protocol.InspectResult{CommitSHA: d.commitSHA})
		return protocol.CommandResult{Output: string(encoded)}, nil
	}
	return protocol.CommandResult{}, errors.New("unexpected command")
}

func (d *convergeDispatcher) IsConnected(string) bool { return true }

// shrinkPostCheckInterval keeps the post-check's retry loop fast for the
// degraded/error cases, which do not short-circuit on the first reply.
func shrinkPostCheckInterval(t *testing.T) {
	t.Helper()
	original := postCheckInterval
	postCheckInterval = time.Millisecond
	t.Cleanup(func() { postCheckInterval = original })
}

// TestConvergeAfterTimeout covers the decision at the heart of issue #178
// item 2: after a deploy its own budget killed, does the record follow the
// host or stay where it was?
func TestConvergeAfterTimeout(t *testing.T) {
	const target = "commit-new"

	cases := []struct {
		name          string
		dispatcher    *convergeDispatcher
		commitSHA     string
		wantConverged bool
		wantStatus    string
		wantDetail    string
	}{
		{
			name: "AllServicesUpAtTheAttemptedRevisionConvergesActive",
			dispatcher: &convergeDispatcher{
				statuses: []compose.ServiceStatus{
					{ServiceName: "web", Status: "running", Health: "healthy"},
					{ServiceName: "db", Status: "running", Health: "healthy"},
				},
				commitSHA: target,
			},
			commitSHA:     target,
			wantConverged: true,
			wantStatus:    "active",
			wantDetail:    "record converged",
		},
		{
			// The issue's own scenario: the gating service came up, the one
			// waiting on it never started. The host does run this revision,
			// so the record has to say so — "degraded" is exactly that state.
			name: "PartiallyUpAtTheAttemptedRevisionConvergesDegraded",
			dispatcher: &convergeDispatcher{
				statuses: []compose.ServiceStatus{
					{ServiceName: "web", Status: "running", Health: "healthy"},
					{ServiceName: "db", Status: "created"},
				},
				commitSHA: target,
			},
			commitSHA:     target,
			wantConverged: true,
			wantStatus:    "degraded",
			wantDetail:    "record converged",
		},
		{
			name: "ContainersStillOnTheOldRevisionConvergesNothing",
			dispatcher: &convergeDispatcher{
				statuses: []compose.ServiceStatus{
					{ServiceName: "web", Status: "running", Health: "healthy"},
					{ServiceName: "db", Status: "running", Health: "healthy"},
				},
				commitSHA: "commit-old",
			},
			commitSHA:  target,
			wantDetail: "commit-old",
		},
		{
			name: "AnUnreadableCommitLabelConvergesNothing",
			dispatcher: &convergeDispatcher{
				statuses: []compose.ServiceStatus{
					{ServiceName: "web", Status: "running", Health: "healthy"},
					{ServiceName: "db", Status: "running", Health: "healthy"},
				},
				inspectErr: errors.New("worker went away"),
			},
			commitSHA:  target,
			wantDetail: "an unknown revision",
		},
		{
			// The post-check reports "active" for its skip paths so the
			// success path can fall back on compose having succeeded. Here it
			// did not succeed, so there is nothing to fall back on.
			name: "AnUnverifiablePostCheckConvergesNothing",
			dispatcher: &convergeDispatcher{
				statusErr: errors.New("worker not answering"),
				commitSHA: target,
			},
			commitSHA:  target,
			wantDetail: "post-check skipped",
		},
		{
			name:       "NoAttemptedCommitConvergesNothing",
			dispatcher: &convergeDispatcher{commitSHA: target},
			commitSHA:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shrinkPostCheckInterval(t)
			r := &Reconciler{dispatcher: tc.dispatcher}

			got := r.convergeAfterTimeout(context.Background(), "worker-1", "stack-1", "/tmp/workdir", []byte(testCompose), tc.commitSHA)

			if got.Converged != tc.wantConverged {
				t.Fatalf("Converged = %v, want %v (detail=%q)", got.Converged, tc.wantConverged, got.Detail)
			}
			if tc.wantConverged && got.Status != tc.wantStatus {
				t.Fatalf("Status = %q, want %q (detail=%q)", got.Status, tc.wantStatus, got.Detail)
			}
			if tc.wantDetail != "" && !strings.Contains(got.Detail, tc.wantDetail) {
				t.Fatalf("Detail = %q, want it to mention %q", got.Detail, tc.wantDetail)
			}
			if !tc.wantConverged && got.Converged {
				t.Fatal("converged on evidence that does not support it")
			}
		})
	}
}

// TestConvergeAfterTimeoutWithoutADispatcherIsANoop guards the paths that
// construct a Reconciler with no worker dispatcher (local stacks, tests).
func TestConvergeAfterTimeoutWithoutADispatcherIsANoop(t *testing.T) {
	r := &Reconciler{}
	got := r.convergeAfterTimeout(context.Background(), "worker-1", "stack-1", "/tmp/workdir", []byte(testCompose), "commit-new")
	if got.Converged || got.Detail != "" {
		t.Fatalf("convergeAfterTimeout = %+v, want a zero result", got)
	}
}

// TestApplyTimeoutConvergenceAdvancesDeployedStateWithoutClearingTheError is
// the point of the whole exercise: the record stops naming a commit that is
// not running, while still saying the deploy failed and how.
func TestApplyTimeoutConvergenceAdvancesDeployedStateWithoutClearingTheError(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)

	stack.Set("deployed_version", 1)
	stack.Set("deployed_commit", "commit-old")
	if err := app.Save(stack); err != nil {
		t.Fatal(err)
	}

	render := &RenderResult{Version: 2, Checksum: "sum-new", ProjectName: "app"}
	applyTimeoutConvergence(stack, render, "commit-new")

	r := &Reconciler{app: app}
	if err := r.markDeployFailure(stack, reasonTimeout, "degraded", "signal: killed", "commit-new"); err != nil {
		t.Fatalf("markDeployFailure: %v", err)
	}

	refreshed, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := refreshed.GetInt("deployed_version"); got != 2 {
		t.Fatalf("deployed_version = %d, want 2", got)
	}
	for field, want := range map[string]string{
		"deployed_commit":      "commit-new",
		"deployed_checksum":    "sum-new",
		"compose_project_name": "app",
		// Converging the revision must not imply the deploy succeeded.
		"status":              "degraded",
		"last_error_category": "deploy",
		"last_error_reason":   reasonTimeout,
		"last_error_commit":   "commit-new",
	} {
		if got := refreshed.GetString(field); got != want {
			t.Fatalf("%s = %q, want %q", field, got, want)
		}
	}
	if refreshed.GetString("last_error_message") == "" {
		t.Fatal("last_error_message is empty, want the failure still recorded")
	}
	if refreshed.GetString("deployed_at") == "" {
		t.Fatal("deployed_at is empty, want it stamped by the convergence")
	}
}
