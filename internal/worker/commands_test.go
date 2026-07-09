package worker

import (
	"context"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
)

func TestLogCommandDispatchSetsDurableFields(t *testing.T) {
	app := newWorkerTestApp(t)
	svc := NewService(app)

	rec, err := svc.LogCommandDispatch(context.Background(), "worker-1", "cmd-1", "cmd-1", "msg-1", "deploy", map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatalf("LogCommandDispatch failed: %v", err)
	}

	if got := rec.GetString("status"); got != "dispatched" {
		t.Fatalf("status = %q, want dispatched", got)
	}
	if got := rec.GetString("message_id"); got != "msg-1" {
		t.Fatalf("message_id = %q, want msg-1", got)
	}
	if got := rec.GetString("idempotency_key"); got != "cmd-1" {
		t.Fatalf("idempotency_key = %q, want cmd-1", got)
	}
	if got := int(rec.GetFloat("attempt_count")); got != 1 {
		t.Fatalf("attempt_count = %d, want 1", got)
	}

	// Redispatching the same CommandID (e.g. after a reconnect) must bump
	// attempt_count instead of resetting it, so operators can see how many
	// times a command had to be redelivered.
	rec2, err := svc.LogCommandDispatch(context.Background(), "worker-1", "cmd-1", "cmd-1", "msg-2", "deploy", nil)
	if err != nil {
		t.Fatalf("second LogCommandDispatch failed: %v", err)
	}
	if got := int(rec2.GetFloat("attempt_count")); got != 2 {
		t.Fatalf("attempt_count after redispatch = %d, want 2", got)
	}
	if got := rec2.GetString("message_id"); got != "msg-2" {
		t.Fatalf("message_id after redispatch = %q, want msg-2", got)
	}
}

func TestLogCommandQueuedThenDispatch(t *testing.T) {
	app := newWorkerTestApp(t)
	svc := NewService(app)

	next := time.Now().Add(5 * time.Second)
	rec, err := svc.LogCommandQueued(context.Background(), "worker-1", "cmd-queued-1", "cmd-queued-1", "deploy", nil, next)
	if err != nil {
		t.Fatalf("LogCommandQueued failed: %v", err)
	}
	if got := rec.GetString("status"); got != "queued" {
		t.Fatalf("status = %q, want queued", got)
	}

	// A worker coming online should transition the same command_id record
	// from queued straight to dispatched, not create a duplicate row.
	if _, err := svc.LogCommandDispatch(context.Background(), "worker-1", "cmd-queued-1", "cmd-queued-1", "msg-1", "deploy", nil); err != nil {
		t.Fatalf("LogCommandDispatch after queue failed: %v", err)
	}

	records, err := app.FindAllRecords("worker_commands", dbx.HashExp{"command_id": "cmd-queued-1"})
	if err != nil {
		t.Fatalf("FindAllRecords failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected exactly 1 worker_commands row for command, got %d", len(records))
	}
	if got := records[0].GetString("status"); got != "dispatched" {
		t.Fatalf("status = %q, want dispatched", got)
	}
}

func TestLogCommandAckIsNoopForUnknownOrTerminalCommand(t *testing.T) {
	app := newWorkerTestApp(t)
	svc := NewService(app)

	// Unknown message_id: no-op, no error.
	if err := svc.LogCommandAck("does-not-exist"); err != nil {
		t.Fatalf("LogCommandAck for unknown message should be a no-op, got error: %v", err)
	}

	rec, err := svc.LogCommandDispatch(context.Background(), "worker-1", "cmd-ack-1", "cmd-ack-1", "msg-ack-1", "deploy", nil)
	if err != nil {
		t.Fatalf("LogCommandDispatch failed: %v", err)
	}

	if err := svc.LogCommandAck("msg-ack-1"); err != nil {
		t.Fatalf("LogCommandAck failed: %v", err)
	}
	updated, err := app.FindRecordById("worker_commands", rec.Id)
	if err != nil {
		t.Fatalf("FindRecordById failed: %v", err)
	}
	if got := updated.GetString("status"); got != "acked" {
		t.Fatalf("status = %q, want acked", got)
	}

	// Once the command reaches a terminal state, a late/duplicate ack must
	// not regress the status back to 'acked'.
	if err := svc.LogCommandFinish("cmd-ack-1", "success", nil, 10); err != nil {
		t.Fatalf("LogCommandFinish failed: %v", err)
	}
	if err := svc.LogCommandAck("msg-ack-1"); err != nil {
		t.Fatalf("LogCommandAck after finish failed: %v", err)
	}
	final, err := app.FindRecordById("worker_commands", rec.Id)
	if err != nil {
		t.Fatalf("FindRecordById failed: %v", err)
	}
	if got := final.GetString("status"); got != "success" {
		t.Fatalf("status after late ack = %q, want success (unchanged)", got)
	}
}

func TestPendingCommandsForWorkerOrderedOldestFirst(t *testing.T) {
	app := newWorkerTestApp(t)
	svc := NewService(app)

	if _, err := svc.LogCommandDispatch(context.Background(), "worker-1", "cmd-a", "cmd-a", "msg-a", "deploy", nil); err != nil {
		t.Fatalf("dispatch cmd-a failed: %v", err)
	}
	if _, err := svc.LogCommandDispatch(context.Background(), "worker-1", "cmd-b", "cmd-b", "msg-b", "teardown", nil); err != nil {
		t.Fatalf("dispatch cmd-b failed: %v", err)
	}
	// A finished command should not show up as pending.
	if _, err := svc.LogCommandDispatch(context.Background(), "worker-1", "cmd-c", "cmd-c", "msg-c", "deploy", nil); err != nil {
		t.Fatalf("dispatch cmd-c failed: %v", err)
	}
	if err := svc.LogCommandFinish("cmd-c", "success", nil, 5); err != nil {
		t.Fatalf("finish cmd-c failed: %v", err)
	}

	pending, err := svc.PendingCommandsForWorker("worker-1")
	if err != nil {
		t.Fatalf("PendingCommandsForWorker failed: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending commands, got %d", len(pending))
	}
	if pending[0].GetString("command_id") != "cmd-a" || pending[1].GetString("command_id") != "cmd-b" {
		t.Fatalf("expected order [cmd-a, cmd-b], got [%s, %s]", pending[0].GetString("command_id"), pending[1].GetString("command_id"))
	}
}
