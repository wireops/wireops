package sync

import (
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// newPhaseTrackerTestApp is an alias for newReconcilerPhase1TestApp (defined
// in reconciler_test.go), which already provisions the sync_log_phases
// collection alongside stacks/sync_logs/stack_pending_reconciles.
func newPhaseTrackerTestApp(t *testing.T) (*tests.TestApp, *core.Record) {
	t.Helper()
	return newReconcilerPhase1TestApp(t)
}

func TestPhaseTrackerStartFinishRecordsSuccess(t *testing.T) {
	app, stack := newPhaseTrackerTestApp(t)
	r := &Reconciler{app: app}

	syncLog, err := r.createSyncLog(stack.Id, "manual", "abc123", "test")
	if err != nil {
		t.Fatalf("createSyncLog failed: %v", err)
	}

	pt := newPhaseTracker(app, syncLog.Id)
	if err := pt.start("git_fetch"); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if err := pt.finish("git_fetch", "success", ""); err != nil {
		t.Fatalf("finish failed: %v", err)
	}

	rows, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": syncLog.Id})
	if err != nil {
		t.Fatalf("failed to query phases: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("phase rows = %d, want 1", len(rows))
	}
	if got := rows[0].GetString("status"); got != "success" {
		t.Fatalf("phase status = %q, want success", got)
	}
	if got := rows[0].GetString("phase"); got != "git_fetch" {
		t.Fatalf("phase = %q, want git_fetch", got)
	}
}

func TestPhaseTrackerStartClosesPreviousPhaseAsSuccess(t *testing.T) {
	app, stack := newPhaseTrackerTestApp(t)
	r := &Reconciler{app: app}

	syncLog, err := r.createSyncLog(stack.Id, "manual", "abc123", "test")
	if err != nil {
		t.Fatalf("createSyncLog failed: %v", err)
	}

	pt := newPhaseTracker(app, syncLog.Id)
	if err := pt.start("git_fetch"); err != nil {
		t.Fatalf("start git_fetch failed: %v", err)
	}
	if err := pt.start("render"); err != nil {
		t.Fatalf("start render failed: %v", err)
	}

	rows, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": syncLog.Id})
	if err != nil {
		t.Fatalf("failed to query phases: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("phase rows = %d, want 2", len(rows))
	}

	var fetchRow, renderRow *core.Record
	for _, row := range rows {
		switch row.GetString("phase") {
		case "git_fetch":
			fetchRow = row
		case "render":
			renderRow = row
		}
	}
	if fetchRow == nil || renderRow == nil {
		t.Fatalf("expected git_fetch and render rows, got %+v", rows)
	}
	if got := fetchRow.GetString("status"); got != "success" {
		t.Fatalf("git_fetch status = %q, want success (auto-closed by next start)", got)
	}
	if got := renderRow.GetString("status"); got != "running" {
		t.Fatalf("render status = %q, want running", got)
	}
}

func TestPhaseTrackerFinishCurrentAsError(t *testing.T) {
	app, stack := newPhaseTrackerTestApp(t)
	r := &Reconciler{app: app}

	syncLog, err := r.createSyncLog(stack.Id, "manual", "abc123", "test")
	if err != nil {
		t.Fatalf("createSyncLog failed: %v", err)
	}

	pt := newPhaseTracker(app, syncLog.Id)
	if err := pt.start("dispatch"); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	pt.finishCurrentAsError("deploy aborted")

	rows, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": syncLog.Id})
	if err != nil {
		t.Fatalf("failed to query phases: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("phase rows = %d, want 1", len(rows))
	}
	if got := rows[0].GetString("status"); got != "error" {
		t.Fatalf("status = %q, want error", got)
	}
	if got := rows[0].GetString("detail"); got != "deploy aborted" {
		t.Fatalf("detail = %q, want %q", got, "deploy aborted")
	}

	// A no-op finish after the current phase already closed shouldn't panic
	// or create a second row.
	pt.finishCurrentAsError("should be a no-op")
	rows, err = app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": syncLog.Id})
	if err != nil {
		t.Fatalf("failed to query phases: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("phase rows after no-op finish = %d, want 1", len(rows))
	}
}

func TestPhaseTrackerRecordSkipped(t *testing.T) {
	app, stack := newPhaseTrackerTestApp(t)
	r := &Reconciler{app: app}

	syncLog, err := r.createSyncLog(stack.Id, "manual", "abc123", "test")
	if err != nil {
		t.Fatalf("createSyncLog failed: %v", err)
	}

	pt := newPhaseTracker(app, syncLog.Id)
	if err := pt.recordSkipped("policy_check", "policy=never"); err != nil {
		t.Fatalf("recordSkipped failed: %v", err)
	}

	rows, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": syncLog.Id})
	if err != nil {
		t.Fatalf("failed to query phases: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("phase rows = %d, want 1", len(rows))
	}
	if got := rows[0].GetString("status"); got != "skipped" {
		t.Fatalf("status = %q, want skipped", got)
	}
	if got := rows[0].GetInt("duration_ms"); got != 0 {
		t.Fatalf("duration_ms = %d, want 0", got)
	}
}

func TestPhaseTrackerRecordCompletedUsesExplicitDuration(t *testing.T) {
	app, stack := newPhaseTrackerTestApp(t)
	r := &Reconciler{app: app}

	syncLog, err := r.createSyncLog(stack.Id, "manual", "abc123", "test")
	if err != nil {
		t.Fatalf("createSyncLog failed: %v", err)
	}

	pt := newPhaseTracker(app, syncLog.Id)
	start := time.Now().Add(-5 * time.Second)
	if err := pt.recordCompleted("git_fetch", "success", start, 1234, ""); err != nil {
		t.Fatalf("recordCompleted failed: %v", err)
	}

	rows, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": syncLog.Id})
	if err != nil {
		t.Fatalf("failed to query phases: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("phase rows = %d, want 1", len(rows))
	}
	if got := rows[0].GetInt("duration_ms"); got != 1234 {
		t.Fatalf("duration_ms = %d, want 1234 (explicit, not derived from now)", got)
	}
	if got := rows[0].GetInt("seq"); got != 0 {
		t.Fatalf("seq for git_fetch = %d, want 0 (fixed canonical index)", got)
	}
}

func TestPhaseSeqIsFixedRegardlessOfRecordOrder(t *testing.T) {
	app, stack := newPhaseTrackerTestApp(t)
	r := &Reconciler{app: app}

	syncLog, err := r.createSyncLog(stack.Id, "manual", "abc123", "test")
	if err != nil {
		t.Fatalf("createSyncLog failed: %v", err)
	}

	// Simulate two independent tracker instances writing to the same
	// sync_log out of canonical order (as happens when policy_check is
	// recorded by wait_jobs.go's own tracker before a later tracker
	// retroactively records git_fetch).
	pt1 := newPhaseTracker(app, syncLog.Id)
	if err := pt1.recordSkipped("policy_check", "policy=never"); err != nil {
		t.Fatalf("recordSkipped failed: %v", err)
	}
	pt2 := newPhaseTracker(app, syncLog.Id)
	if err := pt2.recordCompleted("git_fetch", "success", time.Now(), 10, ""); err != nil {
		t.Fatalf("recordCompleted failed: %v", err)
	}

	rows, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": syncLog.Id})
	if err != nil {
		t.Fatalf("failed to query phases: %v", err)
	}
	seqByPhase := map[string]int{}
	for _, row := range rows {
		seqByPhase[row.GetString("phase")] = row.GetInt("seq")
	}
	if seqByPhase["git_fetch"] >= seqByPhase["policy_check"] {
		t.Fatalf("expected git_fetch seq (%d) < policy_check seq (%d) despite recording order", seqByPhase["git_fetch"], seqByPhase["policy_check"])
	}
}

func TestPhaseTrackerNilSafe(t *testing.T) {
	var pt *phaseTracker
	if err := pt.start("git_fetch"); err != nil {
		t.Fatalf("nil tracker start should be a no-op, got: %v", err)
	}
	if err := pt.finish("git_fetch", "success", ""); err != nil {
		t.Fatalf("nil tracker finish should be a no-op, got: %v", err)
	}
	pt.finishCurrentAsError("x") // must not panic
	if err := pt.recordSkipped("policy_check", "x"); err != nil {
		t.Fatalf("nil tracker recordSkipped should be a no-op, got: %v", err)
	}
}
