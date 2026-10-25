package sync

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/manifest"
)

func newScheduleTestStack(t *testing.T, timeoutSeconds, intervalSeconds int) *core.Record {
	t.Helper()
	col := core.NewBaseCollection("stacks_schedule_test")
	col.Fields.Add(&core.TextField{Name: "config_source"})
	col.Fields.Add(&core.NumberField{Name: "deploy_timeout_seconds"})
	col.Fields.Add(&core.NumberField{Name: "sync_interval_seconds"})
	rec := core.NewRecord(col)
	rec.Set("deploy_timeout_seconds", timeoutSeconds)
	rec.Set("sync_interval_seconds", intervalSeconds)
	return rec
}

func TestScheduleRefreshApplies(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "ComposeEmbedded", source: "compose_embedded", want: true},
		{name: "WireopsFile", source: "wireops_file", want: true},
		{name: "ManualStackKeepsItsOwnSchedule", source: "manual", want: false},
		{name: "EmptySourceIsNotFileManaged", source: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scheduleRefreshApplies(tc.source); got != tc.want {
				t.Fatalf("scheduleRefreshApplies(%q) = %v, want %v", tc.source, got, tc.want)
			}
		})
	}
}

func TestScheduleChangesFor(t *testing.T) {
	cases := []struct {
		name            string
		storedTimeout   int
		storedInterval  int
		def             manifest.Definition
		wantChanges     map[string][2]int
		wantFinalValues [2]int
	}{
		{
			name:            "AppliesBothWhenGitDiverges",
			storedTimeout:   60,
			storedInterval:  10,
			def:             manifest.Definition{DeployTimeoutSeconds: 180, SyncIntervalSeconds: 45},
			wantChanges:     map[string][2]int{"deploy_timeout_seconds": {60, 180}, "sync_interval_seconds": {10, 45}},
			wantFinalValues: [2]int{180, 45},
		},
		{
			name:            "AppliesOnlyTheFieldThatDiffers",
			storedTimeout:   60,
			storedInterval:  45,
			def:             manifest.Definition{DeployTimeoutSeconds: 180, SyncIntervalSeconds: 45},
			wantChanges:     map[string][2]int{"deploy_timeout_seconds": {60, 180}},
			wantFinalValues: [2]int{180, 45},
		},
		{
			name:            "NoChangesWhenAlreadyConverged",
			storedTimeout:   180,
			storedInterval:  45,
			def:             manifest.Definition{DeployTimeoutSeconds: 180, SyncIntervalSeconds: 45},
			wantChanges:     map[string][2]int{},
			wantFinalValues: [2]int{180, 45},
		},
		{
			// Dropping `timeout:` from the file is a deliberate choice to
			// inherit the global default, so zero has to win over the stored
			// value rather than being treated as "nothing declared".
			name:            "ConvergesToZeroWhenTheKeyWasRemoved",
			storedTimeout:   60,
			storedInterval:  10,
			def:             manifest.Definition{},
			wantChanges:     map[string][2]int{"deploy_timeout_seconds": {60, 0}, "sync_interval_seconds": {10, 0}},
			wantFinalValues: [2]int{0, 0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stack := newScheduleTestStack(t, tc.storedTimeout, tc.storedInterval)
			def := tc.def

			changes := scheduleChangesFor(stack, &def)

			if len(changes) != len(tc.wantChanges) {
				t.Fatalf("got %d changes (%v), want %d", len(changes), changes, len(tc.wantChanges))
			}
			for _, change := range changes {
				want, ok := tc.wantChanges[change.Field]
				if !ok {
					t.Fatalf("unexpected change for %q: %v", change.Field, change)
				}
				if change.From != want[0] || change.To != want[1] {
					t.Fatalf("%s = %d -> %d, want %d -> %d", change.Field, change.From, change.To, want[0], want[1])
				}
			}
			if got := stack.GetInt("deploy_timeout_seconds"); got != tc.wantFinalValues[0] {
				t.Fatalf("deploy_timeout_seconds = %d, want %d", got, tc.wantFinalValues[0])
			}
			if got := stack.GetInt("sync_interval_seconds"); got != tc.wantFinalValues[1] {
				t.Fatalf("sync_interval_seconds = %d, want %d", got, tc.wantFinalValues[1])
			}
		})
	}
}

func TestFormatScheduleChanges(t *testing.T) {
	got := formatScheduleChanges([]scheduleChange{
		{Field: "deploy_timeout_seconds", From: 60, To: 180},
		{Field: "sync_interval_seconds", From: 10, To: 45},
	})
	want := "deploy_timeout_seconds 60 -> 180, sync_interval_seconds 10 -> 45"
	if got != want {
		t.Fatalf("formatScheduleChanges() = %q, want %q", got, want)
	}
}
