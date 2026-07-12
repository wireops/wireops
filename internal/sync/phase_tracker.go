package sync

import (
	"fmt"
	"log"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/constants"
	"github.com/wireops/wireops/internal/deploymetrics"
)

// deployPhases re-exports constants.DeployPhaseOrder under the name used
// throughout this package/its tests.
var deployPhases = constants.DeployPhaseOrder

// phaseSeq returns the fixed ordering index for phase, so sync_log_phases
// rows sort consistently in the timeline UI even when written by different
// phaseTracker instances (e.g. one created inside wait_jobs.go, another
// later in the same deploy) or out of chronological order.
func phaseSeq(phase string) int {
	for i, p := range deployPhases {
		if p == phase {
			return i
		}
	}
	return len(deployPhases)
}

// phaseTracker records structured sync_log_phases rows for a single
// sync_logs.id, replacing the ad-hoc time.Since(start) + string-concat
// pattern previously scattered across the reconciler's deploy flows.
type phaseTracker struct {
	app       core.App
	syncLogID string
	current   *core.Record
}

func newPhaseTracker(app core.App, syncLogID string) *phaseTracker {
	return &phaseTracker{app: app, syncLogID: syncLogID}
}

// start closes any currently-open phase as "success" (a clean handoff to the
// next phase), then opens a new sync_log_phases row for phase.
func (t *phaseTracker) start(phase string) error {
	if t == nil {
		return nil
	}
	if t.current != nil {
		if err := t.finish(t.current.GetString("phase"), constants.PhaseStatusSuccess, ""); err != nil {
			return err
		}
	}

	collection, err := t.app.FindCollectionByNameOrId("sync_log_phases")
	if err != nil {
		return fmt.Errorf("phase tracker start phase=%s: %w", phase, err)
	}
	record := core.NewRecord(collection)
	record.Set("sync_log", t.syncLogID)
	record.Set("phase", phase)
	record.Set("status", constants.PhaseStatusRunning)
	record.Set("started_at", time.Now())
	record.Set("seq", phaseSeq(phase))
	if err := t.app.Save(record); err != nil {
		return fmt.Errorf("phase tracker start phase=%s: %w", phase, err)
	}
	t.current = record
	return nil
}

// finish closes the currently-open phase with the given terminal status
// ("success"|"error"|"skipped") and detail text, computing duration_ms from
// the phase's own started_at. It is a no-op if phase does not match the
// currently open phase (logged, not returned as an error, since this is
// defensive bookkeeping rather than a caller-facing contract).
func (t *phaseTracker) finish(phase, status, detail string) error {
	if t == nil || t.current == nil {
		return nil
	}
	if t.current.GetString("phase") != phase {
		log.Printf("[phase_tracker] finish called for phase=%s but current open phase is %s (sync_log=%s)", phase, t.current.GetString("phase"), t.syncLogID)
		return nil
	}

	record := t.current
	startedAt := record.GetDateTime("started_at").Time()
	durationMs := time.Since(startedAt).Milliseconds()
	record.Set("status", status)
	record.Set("duration_ms", durationMs)
	record.Set("detail", detail)
	if err := t.app.Save(record); err != nil {
		return fmt.Errorf("phase tracker finish phase=%s: %w", phase, err)
	}
	t.current = nil
	deploymetrics.RecordPhaseDuration(phase, status, durationMs)
	return nil
}

// finishCurrentAsError closes whatever phase is currently open (if any) as
// "error" with the given detail. Meant for defer-style cleanup so an early
// return or panic mid-phase still closes the open row instead of leaving it
// stuck at status=running forever.
func (t *phaseTracker) finishCurrentAsError(detail string) {
	if t == nil || t.current == nil {
		return
	}
	phase := t.current.GetString("phase")
	if err := t.finish(phase, constants.PhaseStatusError, detail); err != nil {
		log.Printf("[phase_tracker] finishCurrentAsError phase=%s: %v", phase, err)
	}
}

// recordSkipped writes a single already-closed row for a phase that doesn't
// apply to this run (e.g. policy_check when wait_running_jobs is "never", or
// worker_ack/dispatch for a local-only deploy) — keeps the phase set uniform
// across every deploy flow so the timeline UI never has to special-case a
// missing phase.
func (t *phaseTracker) recordSkipped(phase, detail string) error {
	if t == nil {
		return nil
	}
	collection, err := t.app.FindCollectionByNameOrId("sync_log_phases")
	if err != nil {
		return fmt.Errorf("phase tracker recordSkipped phase=%s: %w", phase, err)
	}
	record := core.NewRecord(collection)
	record.Set("sync_log", t.syncLogID)
	record.Set("phase", phase)
	record.Set("status", constants.PhaseStatusSkipped)
	record.Set("started_at", time.Now())
	record.Set("duration_ms", 0)
	record.Set("detail", detail)
	record.Set("seq", phaseSeq(phase))
	if err := t.app.Save(record); err != nil {
		return fmt.Errorf("phase tracker recordSkipped phase=%s: %w", phase, err)
	}
	deploymetrics.RecordPhaseDuration(phase, constants.PhaseStatusSkipped, 0)
	return nil
}

// recordCompleted writes a single already-closed row for a phase that ran
// and finished before this tracker (or even the owning sync_logs row)
// existed — e.g. git_fetch/render, which happen ahead of the point in
// ReconcileStack where the sync_logs row is created. durationMs is the
// caller's own locally-measured elapsed time, not derived from "now", since
// by the time this is called the phase may have finished long ago.
func (t *phaseTracker) recordCompleted(phase, status string, startedAt time.Time, durationMs int64, detail string) error {
	if t == nil {
		return nil
	}
	collection, err := t.app.FindCollectionByNameOrId("sync_log_phases")
	if err != nil {
		return fmt.Errorf("phase tracker recordCompleted phase=%s: %w", phase, err)
	}
	record := core.NewRecord(collection)
	record.Set("sync_log", t.syncLogID)
	record.Set("phase", phase)
	record.Set("status", status)
	record.Set("started_at", startedAt)
	record.Set("duration_ms", durationMs)
	record.Set("detail", detail)
	record.Set("seq", phaseSeq(phase))
	if err := t.app.Save(record); err != nil {
		return fmt.Errorf("phase tracker recordCompleted phase=%s: %w", phase, err)
	}
	deploymetrics.RecordPhaseDuration(phase, status, durationMs)
	return nil
}
