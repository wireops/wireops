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

func TestResolveWebSocketURL(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"https://example.com", "wss://example.com/worker/ws"},
		{"http://example.com", "ws://example.com/worker/ws"},
		{"wss://example.com", "wss://example.com/worker/ws"},
		{"ws://example.com", "ws://example.com/worker/ws"},
		{"ftp://example.com", "ws://example.com/worker/ws"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := resolveWebSocketURL(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolveWebSocketURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
