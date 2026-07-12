package deploymetrics

import (
	"strings"
	"testing"

	"github.com/wireops/wireops/internal/constants"
)

func TestRecordPhaseDurationAccumulatesAndSerializes(t *testing.T) {
	// Reset global state so this test is independent of others in the package,
	// then restore it afterward so later tests/packages don't inherit this
	// test's modified state.
	phaseStatsMu.Lock()
	original := phaseStats
	phaseStats = map[string]*phaseStat{}
	phaseStatsMu.Unlock()
	t.Cleanup(func() {
		phaseStatsMu.Lock()
		phaseStats = original
		phaseStatsMu.Unlock()
	})

	RecordPhaseDuration(constants.PhaseGitFetch, constants.PhaseStatusSuccess, 100)
	RecordPhaseDuration(constants.PhaseGitFetch, constants.PhaseStatusError, 50)

	out := Serialize()

	if !strings.Contains(out, `wireops_deploy_phase_duration_ms_sum{phase="git_fetch"} 150`) {
		t.Fatalf("expected accumulated duration sum of 150, got:\n%s", out)
	}
	if !strings.Contains(out, `wireops_deploy_phase_duration_ms_count{phase="git_fetch"} 2`) {
		t.Fatalf("expected count of 2, got:\n%s", out)
	}
	if !strings.Contains(out, `wireops_deploy_phase_failures_total{phase="git_fetch"} 1`) {
		t.Fatalf("expected 1 failure, got:\n%s", out)
	}
	if strings.Contains(out, `phase="render"`) {
		t.Fatalf("expected no render metrics to be present (never recorded), got:\n%s", out)
	}
}
