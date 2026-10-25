package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wireops/wireops/internal/compose"
)

// blockingStep returns a composeStep that waits for its context and reports
// the same error a SIGKILLed `docker compose` does — the string the server
// used to have to match on.
func blockingStep(out string) composeStep {
	return func(ctx context.Context, _ compose.RunOptions) (string, error) {
		<-ctx.Done()
		return out, errors.New("docker compose up failed: signal: killed")
	}
}

func okStep(out string) composeStep {
	return func(context.Context, compose.RunOptions) (string, error) { return out, nil }
}

// TestPullThenUpAttributesTheExpiredBudget pins which step pullThenUp blames,
// and — the case that matters for issue #178 — that a command the server
// withdrew is not reported as a timeout. The worker is the last place that can
// tell: exec.CommandContext SIGKILLs the child either way, so from here on the
// only evidence is `signal: killed`.
func TestPullThenUpAttributesTheExpiredBudget(t *testing.T) {
	cases := []struct {
		name string
		// cancelParent cancels the parent context instead of letting a step
		// deadline expire, standing in for MsgCancelCommand.
		cancelParent bool
		pull         composeStep
		up           composeStep
		pullTimeout  int
		upTimeout    int
		wantStep     string
		wantErr      bool
	}{
		{
			name:        "UpBudgetExpiredBlamesUp",
			pull:        okStep("pulled\n"),
			up:          blockingStep("up output\n"),
			pullTimeout: 60,
			upTimeout:   1,
			wantStep:    "up",
			wantErr:     true,
		},
		{
			name:        "PullBudgetExpiredBlamesPull",
			pull:        blockingStep("pull output\n"),
			up:          okStep("never runs\n"),
			pullTimeout: 1,
			upTimeout:   60,
			wantStep:    "pull",
			wantErr:     true,
		},
		{
			name:         "ServerWithdrawingTheCommandIsNotATimeout",
			cancelParent: true,
			pull:         okStep("pulled\n"),
			up:           blockingStep("up output\n"),
			pullTimeout:  60,
			upTimeout:    60,
			wantStep:     "",
			wantErr:      true,
		},
		{
			name: "AnOrdinaryComposeFailureIsNotATimeout",
			pull: okStep("pulled\n"),
			up: func(context.Context, compose.RunOptions) (string, error) {
				return "boom\n", errors.New("docker compose up failed: exit status 1")
			},
			pullTimeout: 60,
			upTimeout:   60,
			wantStep:    "",
			wantErr:     true,
		},
		{
			name:        "SuccessReportsNoStep",
			pull:        okStep("pulled\n"),
			up:          okStep("started\n"),
			pullTimeout: 60,
			upTimeout:   60,
			wantStep:    "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelParent {
				go func() {
					time.Sleep(50 * time.Millisecond)
					cancel()
				}()
			}

			_, _, step, err := pullThenUp(ctx, tc.pullTimeout, tc.upTimeout, compose.RunOptions{}, tc.pull, tc.up)

			if tc.wantErr && err == nil {
				t.Fatal("pullThenUp succeeded, want an error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("pullThenUp: %v", err)
			}
			if step != tc.wantStep {
				t.Fatalf("timedOutStep = %q, want %q", step, tc.wantStep)
			}
		})
	}
}

// TestPullThenUpReportsUpDurationOnlyAfterPullSucceeds guards what
// protocol.CommandResult.ComposeUpMs documents: 0 when pull failed before the
// up step ran, so the server shows "unknown" rather than a false 0ms.
func TestPullThenUpReportsUpDurationOnlyAfterPullSucceeds(t *testing.T) {
	_, upMs, _, err := pullThenUp(context.Background(), 1, 60, compose.RunOptions{},
		blockingStep("pull output\n"), okStep("never runs\n"))
	if err == nil {
		t.Fatal("pullThenUp succeeded, want the pull step to fail")
	}
	if upMs != 0 {
		t.Fatalf("upElapsedMs = %d after a failed pull, want 0", upMs)
	}
}

// TestStepDeadlineFired covers the predicate directly, including the case
// where both contexts are done — a parent cancelled at the same moment a step
// deadline passed must still not read as a timeout.
func TestStepDeadlineFired(t *testing.T) {
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	cancelled, cancelCancelled := context.WithCancel(context.Background())
	cancelCancelled()

	cases := []struct {
		name   string
		parent context.Context
		step   context.Context
		want   bool
	}{
		{name: "StepDeadlinePassed", parent: context.Background(), step: expired, want: true},
		{name: "ParentCancelled", parent: cancelled, step: cancelled, want: false},
		{name: "BothDoneStillPrefersTheParent", parent: cancelled, step: expired, want: false},
		{name: "NeitherDone", parent: context.Background(), step: context.Background(), want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stepDeadlineFired(tc.parent, tc.step); got != tc.want {
				t.Fatalf("stepDeadlineFired = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestInspectResultForKeepsTheFirstEntryInCommitSHA pins the back-compatible
// half of the inspect reply. CommitSHA is what this command returned before
// the set existed, so an older server — and the reconciler's cron fast path,
// which deliberately still reads only that field — must keep seeing it.
func TestInspectResultForKeepsTheFirstEntryInCommitSHA(t *testing.T) {
	cases := []struct {
		name          string
		commits       []string
		wantCommitSHA string
	}{
		{name: "NoContainers", commits: nil, wantCommitSHA: ""},
		{name: "SingleRevision", commits: []string{"commit-a"}, wantCommitSHA: "commit-a"},
		{name: "StraddlingReportsTheFirst", commits: []string{"commit-a", "commit-b"}, wantCommitSHA: "commit-a"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inspectResultFor(tc.commits)
			if got.CommitSHA != tc.wantCommitSHA {
				t.Fatalf("CommitSHA = %q, want %q", got.CommitSHA, tc.wantCommitSHA)
			}
			if len(got.CommitSHAs) != len(tc.commits) {
				t.Fatalf("CommitSHAs = %v, want %v", got.CommitSHAs, tc.commits)
			}
		})
	}
}
