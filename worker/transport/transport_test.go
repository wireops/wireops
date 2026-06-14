package transport

import (
	"testing"

	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/worker/spool"
)

func TestPendingCounts(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := spool.New(tmpDir, "token-123")
	if err != nil {
		t.Fatalf("spool.New failed: %v", err)
	}
	setOutboxStore(store)
	t.Cleanup(func() { setOutboxStore(nil) })

	if err := store.Enqueue("msg-result", "command_result", protocol.Envelope{
		Type:    protocol.MsgResult,
		Payload: protocol.CommandResult{MessageID: "msg-result", CommandID: "cmd-1", Output: "ok"},
	}); err != nil {
		t.Fatalf("enqueue result failed: %v", err)
	}

	if err := store.Enqueue("msg-job", "job_completed", protocol.Envelope{
		Type: protocol.MsgJobCompleted,
		Payload: protocol.JobCompletedMessage{
			MessageID: "msg-job",
			JobRunID:  "job-1",
			Success:   true,
		},
	}); err != nil {
		t.Fatalf("enqueue job failed: %v", err)
	}

	results, jobs := pendingCounts()
	if results != 1 || jobs != 1 {
		t.Fatalf("pendingCounts() = (%d, %d), want (1, 1)", results, jobs)
	}
}

func TestNewMessageID(t *testing.T) {
	a := newMessageID()
	b := newMessageID()
	if a == "" || b == "" {
		t.Fatalf("expected non-empty message IDs")
	}
	if a == b {
		t.Fatalf("expected distinct message IDs, got %q and %q", a, b)
	}
}
