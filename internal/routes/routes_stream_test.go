package routes

import (
	"testing"

	"github.com/wireops/wireops/internal/logstream"
)

// TestStreamHandoffStateReconcilesGapEvents covers the handoff window between
// Subscribe and the historical FindAllRecords snapshot: events published in
// that gap must be applied exactly once against the snapshot state, with
// neither a duplicate replay nor a dropped byte.
func TestStreamHandoffStateReconcilesGapEvents(t *testing.T) {
	state := newStreamHandoffState()

	// Snapshot loaded "cloning\n" for record "a" before the gap.
	state.observeSnapshot("a", "cloning\n")

	// A queued event that arrived during the subscribe/snapshot gap and is
	// already fully covered by the snapshot must be a no-op.
	if delta := state.apply(logstream.Event{RecordID: "a", Output: "cloning\n"}); delta != "" {
		t.Fatalf("expected duplicate gap event to produce no delta, got %q", delta)
	}

	// A queued event for the same record with new output beyond the
	// snapshot must emit only the unseen tail, exactly once.
	if delta := state.apply(logstream.Event{RecordID: "a", Output: "cloning\ndeploying\n"}); delta != "deploying\n" {
		t.Fatalf("expected gap event to emit only the new tail, got %q", delta)
	}

	// Re-applying the same event (e.g. also delivered via the live-tail
	// loop after the drain) must not re-emit it.
	if delta := state.apply(logstream.Event{RecordID: "a", Output: "cloning\ndeploying\n"}); delta != "" {
		t.Fatalf("expected re-applied event to produce no delta, got %q", delta)
	}
}

// TestStreamHandoffStateNewRecordDuringGap covers the edge case where a
// sync_logs record is created entirely within the subscribe/snapshot gap, so
// it never appears in the snapshot at all: the reconciler must still emit
// its full output exactly once, using the zero-value default for an unseen
// RecordID rather than dropping it.
func TestStreamHandoffStateNewRecordDuringGap(t *testing.T) {
	state := newStreamHandoffState()
	state.observeSnapshot("a", "cloning\n")

	// Record "b" was created after the snapshot query ran but before
	// Subscribe's caller started draining, so it has no snapshot entry.
	if delta := state.apply(logstream.Event{RecordID: "b", Output: "starting\n"}); delta != "starting\n" {
		t.Fatalf("expected full output for unseen record, got %q", delta)
	}

	// A follow-up update to the same new record must only emit the delta.
	if delta := state.apply(logstream.Event{RecordID: "b", Output: "starting\ndone\n"}); delta != "done\n" {
		t.Fatalf("expected delta-only emit for known record, got %q", delta)
	}
}
