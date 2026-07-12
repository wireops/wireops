// Package deploymetrics accumulates and exposes Prometheus metrics for the
// deploy timeline (P2.1). It has no dependency on internal/sync, so
// internal/sync can import it to record observations while
// internal/metrics (which already depends on internal/sync for
// WorkerDispatcher) can independently import it to expose them — avoiding an
// import cycle between internal/sync and internal/metrics.
package deploymetrics

import (
	"fmt"
	"strings"
	"sync"

	"github.com/wireops/wireops/internal/constants"
)

// phaseStat accumulates duration/count/failure totals for one deploy
// timeline phase (git_fetch, render, ...). Server-side counterpart to
// worker/metrics/metrics.go's atomic counters — kept as its own leaf package
// since these are control-plane metrics, not worker-process metrics.
type phaseStat struct {
	durationMsSum int64
	count         int64
	failures      int64
}

var (
	phaseStatsMu sync.Mutex
	phaseStats   = map[string]*phaseStat{}
)

// RecordPhaseDuration accumulates one observed deploy phase duration for
// Prometheus exposition. Called by internal/sync's phaseTracker every time a
// phase closes (finish/recordSkipped/recordCompleted).
func RecordPhaseDuration(phase, status string, durationMs int64) {
	phaseStatsMu.Lock()
	defer phaseStatsMu.Unlock()

	st, ok := phaseStats[phase]
	if !ok {
		st = &phaseStat{}
		phaseStats[phase] = st
	}
	st.durationMsSum += durationMs
	st.count++
	if status == constants.PhaseStatusError {
		st.failures++
	}
}

// ResetForTest clears accumulated phase stats. Test-only helper: package
// state is process-global, so tests that record phase durations must reset
// it (e.g. via t.Cleanup) to avoid bleeding into other tests.
func ResetForTest() {
	phaseStatsMu.Lock()
	defer phaseStatsMu.Unlock()
	phaseStats = map[string]*phaseStat{}
}

// Serialize renders the accumulated deploy-phase metrics as Prometheus text
// exposition, following the same hand-rolled (no client library) format used
// by worker/metrics/metrics.go.
func Serialize() string {
	phaseStatsMu.Lock()
	defer phaseStatsMu.Unlock()

	var sb strings.Builder
	sb.WriteString("# HELP wireops_deploy_phase_duration_ms_sum Cumulative duration of deploy timeline phases in milliseconds\n")
	sb.WriteString("# TYPE wireops_deploy_phase_duration_ms_sum counter\n")
	sb.WriteString("# HELP wireops_deploy_phase_duration_ms_count Number of deploy timeline phase observations\n")
	sb.WriteString("# TYPE wireops_deploy_phase_duration_ms_count counter\n")
	sb.WriteString("# HELP wireops_deploy_phase_failures_total Number of deploy timeline phases that ended in error\n")
	sb.WriteString("# TYPE wireops_deploy_phase_failures_total counter\n")

	for _, phase := range constants.DeployPhaseOrder {
		st, ok := phaseStats[phase]
		if !ok {
			continue
		}
		label := fmt.Sprintf("{phase=%q}", phase)
		fmt.Fprintf(&sb, "wireops_deploy_phase_duration_ms_sum%s %d\n", label, st.durationMsSum)
		fmt.Fprintf(&sb, "wireops_deploy_phase_duration_ms_count%s %d\n", label, st.count)
		fmt.Fprintf(&sb, "wireops_deploy_phase_failures_total%s %d\n", label, st.failures)
	}

	return sb.String()
}
