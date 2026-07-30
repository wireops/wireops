package sync

import (
	"errors"
	"testing"
	"time"

	"github.com/pocketbase/dbx"

	"github.com/wireops/wireops/internal/constants"
	"github.com/wireops/wireops/internal/lint"
)

func TestLintResultDetail(t *testing.T) {
	cases := []struct {
		name string
		res  lintResult
		want string
	}{
		{
			name: "clean",
			res:  lintResult{report: lint.Report{}},
			want: "no findings",
		},
		{
			name: "counts",
			res: lintResult{report: lint.Report{
				Findings: make([]lint.Finding, 3),
				Errors:   1, Warnings: 2,
			}},
			want: "1 error, 2 warnings",
		},
		{
			name: "skipped",
			res:  lintResult{err: errors.New("compose file not found")},
			want: "lint skipped: compose file not found",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.res.detail(); got != tc.want {
				t.Errorf("detail() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRecordLintPhaseNeverRecordsAnError pins the design decision that lint is
// advisory: even a report full of error-severity findings leaves the timeline
// step green, because those findings did not stop the deploy. A red step would
// claim otherwise.
func TestRecordLintPhaseNeverRecordsAnError(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)
	r := &Reconciler{app: app}

	syncLog, err := r.createSyncLog(stack.Id, "manual", "abc123", "test")
	if err != nil {
		t.Fatalf("createSyncLog failed: %v", err)
	}

	pt := newPhaseTracker(app, syncLog.Id)
	recordLintPhase(pt, lintResult{
		report: lint.Report{
			Findings: make([]lint.Finding, 2),
			Errors:   2,
		},
		started:  time.Now().Add(-time.Second),
		duration: 120,
	})

	phases, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": syncLog.Id, "phase": constants.PhaseLint})
	if err != nil || len(phases) != 1 {
		t.Fatalf("lint phase rows = %d, err = %v, want exactly 1", len(phases), err)
	}
	if got := phases[0].GetString("status"); got != constants.PhaseStatusSuccess {
		t.Errorf("lint phase status = %q, want success even with error findings", got)
	}
	if got := phases[0].GetString("detail"); got != "2 errors" {
		t.Errorf("lint phase detail = %q, want %q", got, "2 errors")
	}
	if got := phases[0].GetInt("duration_ms"); got != 120 {
		t.Errorf("lint phase duration_ms = %d, want 120", got)
	}
}

func TestRecordLintPhaseRecordsSkippedWhenLintCouldNotRun(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)
	r := &Reconciler{app: app}

	syncLog, err := r.createSyncLog(stack.Id, "manual", "abc123", "test")
	if err != nil {
		t.Fatalf("createSyncLog failed: %v", err)
	}

	pt := newPhaseTracker(app, syncLog.Id)
	recordLintPhase(pt, lintResult{err: errors.New("compose file not found")})

	phases, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": syncLog.Id, "phase": constants.PhaseLint})
	if err != nil || len(phases) != 1 {
		t.Fatalf("lint phase rows = %d, err = %v, want exactly 1", len(phases), err)
	}
	if got := phases[0].GetString("status"); got != constants.PhaseStatusSkipped {
		t.Errorf("lint phase status = %q, want skipped", got)
	}
	if got := phases[0].GetString("detail"); got != "lint skipped: compose file not found" {
		t.Errorf("lint phase detail = %q, want it to explain the skip", got)
	}
}

// TestLintPhaseSortsBetweenGitFetchAndRender guards the timeline ordering:
// seq comes from constants.DeployPhaseOrder, so inserting lint in the wrong
// place there would render it out of sequence in the UI.
func TestLintPhaseSortsBetweenGitFetchAndRender(t *testing.T) {
	gitFetch := phaseSeq(constants.PhaseGitFetch)
	lintSeq := phaseSeq(constants.PhaseLint)
	render := phaseSeq(constants.PhaseRender)

	if !(gitFetch < lintSeq && lintSeq < render) {
		t.Errorf("phase seq order = git_fetch:%d lint:%d render:%d, want lint strictly between the other two", gitFetch, lintSeq, render)
	}
}
