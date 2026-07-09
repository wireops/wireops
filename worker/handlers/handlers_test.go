package handlers

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/worker/metrics"
)

type MockSender struct {
	mu        sync.Mutex
	results   []protocol.CommandResult
	envelopes []protocol.Envelope
}

func (m *MockSender) SendResult(res protocol.CommandResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results = append(m.results, res)
}
func (m *MockSender) SendEnvelope(env protocol.Envelope) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.envelopes = append(m.envelopes, env)
}
func (m *MockSender) ReportJobCompleted(msg protocol.JobCompletedMessage) {}
func (m *MockSender) QueuedEnvelopesLen() int                             { return 0 }
func (m *MockSender) QueuedJobsLen() int                                  { return 0 }

func (m *MockSender) Results() []protocol.CommandResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]protocol.CommandResult, len(m.results))
	copy(out, m.results)
	return out
}

func (m *MockSender) Envelopes() []protocol.Envelope {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]protocol.Envelope, len(m.envelopes))
	copy(out, m.envelopes)
	return out
}

func TestExtractCommandID(t *testing.T) {
	cases := []struct {
		name    string
		payload interface{}
		want    string
	}{
		{
			name:    "nil payload",
			payload: nil,
			want:    "",
		},
		{
			name:    "empty map",
			payload: map[string]interface{}{},
			want:    "",
		},
		{
			name: "valid command_id",
			payload: map[string]interface{}{
				"command_id": "cmd-123",
			},
			want: "cmd-123",
		},
		{
			name: "wrong type command_id",
			payload: map[string]interface{}{
				"command_id": 123,
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractCommandID(tc.payload)
			if got != tc.want {
				t.Errorf("extractCommandID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUnmarshalPayloadOrReply(t *testing.T) {
	sender := &MockSender{}

	// 1. Test successful unmarshal
	validPayload := map[string]interface{}{
		"command_id": "deploy-1",
		"stack_id":   "stack-abc",
	}

	cmd, ok := unmarshalPayloadOrReply[protocol.DeployCommand](sender, validPayload, "")
	if !ok {
		t.Errorf("expected successful unmarshal, got false")
	}
	if cmd.CommandID != "deploy-1" || cmd.StackID != "stack-abc" {
		t.Errorf("unexpected command result: %+v", cmd)
	}

	// 2. Test failed unmarshal with command_id
	invalidPayload := map[string]interface{}{
		"command_id":  "deploy-2",
		"queue_total": "this-should-be-int-not-string", // type mismatch
	}

	_, ok = unmarshalPayloadOrReply[protocol.DeployCommand](sender, invalidPayload, "")
	if ok {
		t.Errorf("expected failed unmarshal, got true")
	}

	if len(sender.results) != 1 {
		t.Fatalf("expected 1 result reply, got %d", len(sender.results))
	}

	res := sender.results[0]
	if res.CommandID != "deploy-2" {
		t.Errorf("expected command_id deploy-2, got %q", res.CommandID)
	}
	if res.Error == "" {
		t.Errorf("expected non-empty error message")
	}
}

func TestIsQueueFull(t *testing.T) {
	InitSemaphores(1, 1, 1, 2)
	atomic.StoreInt64(&metrics.QueuedTasks, 0)
	if IsQueueFull() {
		t.Errorf("expected queue not full initially")
	}
	atomic.StoreInt64(&metrics.QueuedTasks, 2)
	if !IsQueueFull() {
		t.Errorf("expected queue to be full when QueuedTasks >= MaxQueueDepth")
	}
	atomic.StoreInt64(&metrics.QueuedTasks, 0) // reset
}

func TestRunThrottled(t *testing.T) {
	InitSemaphores(1, 1, 1, 2)
	sem := make(chan struct{}, 1)
	sem <- struct{}{} // acquire it

	started := make(chan struct{})
	go func() {
		RunThrottled(sem, protocol.MsgDeploy, func() {
			close(started)
		})
	}()

	select {
	case <-started:
		t.Fatalf("task should not run while sem is acquired")
	case <-time.After(10 * time.Millisecond):
	}

	<-sem // release it
	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("task should run after sem is released")
	}
}

func TestTryScheduleThrottledRejectsWhenQueueFull(t *testing.T) {
	InitSemaphores(1, 1, 1, 1)
	atomic.StoreInt64(&metrics.QueuedTasks, 0)

	sem := make(chan struct{}, 1)
	sem <- struct{}{}

	started := make(chan struct{})
	finished := make(chan struct{})

	if ok := TryScheduleThrottled(sem, protocol.MsgDeploy, func() {
		close(started)
		close(finished)
	}); !ok {
		t.Fatalf("expected first queued task to be accepted")
	}

	if got := atomic.LoadInt64(&metrics.QueuedTasks); got != 1 {
		t.Fatalf("expected queued task metric to be 1, got %d", got)
	}

	if ok := TryScheduleThrottled(sem, protocol.MsgDeploy, func() {
		t.Fatalf("second queued task should not execute")
	}); ok {
		t.Fatalf("expected second queued task to be rejected")
	}

	select {
	case <-started:
		t.Fatalf("queued task should not start while semaphore is occupied")
	default:
	}

	<-sem

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected queued task to start after semaphore was released")
	}

	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected queued task to finish")
	}

	if got := atomic.LoadInt64(&metrics.QueuedTasks); got != 0 {
		t.Fatalf("expected queued task metric to return to 0, got %d", got)
	}
}

func TestHandleRunJobRejectsWhenWorkerOverloaded(t *testing.T) {
	InitSemaphores(1, 1, 1, 0)
	atomic.StoreInt64(&metrics.QueuedTasks, 0)

	HeavySemaphore <- struct{}{}
	defer func() { <-HeavySemaphore }()

	sender := &MockSender{}
	payload := map[string]interface{}{
		"command_id": "job-cmd-1",
		"job_run_id": "job-run-1",
		"image":      "alpine:latest",
	}

	HandleRunJob(sender, payload)

	results := sender.Results()
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result, got %d", len(results))
	}
	if results[0].Error != "rejected: worker overloaded" {
		t.Fatalf("expected overload rejection, got %+v", results[0])
	}
	if results[0].Output != "" {
		t.Fatalf("expected no queued ack output on rejection, got %+v", results[0])
	}
}

func TestInitSemaphoresPanicsOnInvalidParameters(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for invalid heavy parameter")
		}
	}()
	InitSemaphores(0, 1, 1, 1)
}

func TestInitSemaphoresPanicsOnInvalidLight(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for invalid light parameter")
		}
	}()
	InitSemaphores(1, 0, 1, 1)
}

func TestInitSemaphoresPanicsOnInvalidInteractive(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for invalid interactive parameter")
		}
	}()
	InitSemaphores(1, 1, 0, 1)
}

func TestBeginDurableCommandSendsAckWhenMessageIDPresent(t *testing.T) {
	sender := &MockSender{}

	proceed := beginDurableCommand(sender, "cmd-ack-1", "msg-ack-1")
	if !proceed {
		t.Fatal("expected first delivery to proceed")
	}

	envelopes := sender.Envelopes()
	if len(envelopes) != 1 {
		t.Fatalf("expected 1 ack envelope, got %d", len(envelopes))
	}
	if envelopes[0].Type != protocol.MsgAck {
		t.Fatalf("expected MsgAck envelope, got %v", envelopes[0].Type)
	}
	ack, ok := envelopes[0].Payload.(protocol.AckMessage)
	if !ok || ack.MessageID != "msg-ack-1" {
		t.Fatalf("expected ack payload with message_id=msg-ack-1, got %+v", envelopes[0].Payload)
	}
}

func TestBeginDurableCommandNoAckWithoutMessageID(t *testing.T) {
	sender := &MockSender{}

	if !beginDurableCommand(sender, "cmd-noack-1", "") {
		t.Fatal("expected proceed=true")
	}
	if len(sender.Envelopes()) != 0 {
		t.Fatalf("expected no ack envelope when messageID is empty, got %d", len(sender.Envelopes()))
	}
}

func TestBeginDurableCommandSkipsWhileInFlight(t *testing.T) {
	sender := &MockSender{}
	commandID := "cmd-inflight-1"

	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	ActiveCommands.Store(commandID, cancel)
	defer ActiveCommands.Delete(commandID)

	if beginDurableCommand(sender, commandID, "msg-1") {
		t.Fatal("expected redelivery of an in-flight command to be rejected")
	}
	// The in-flight duplicate should still be acked at the transport level...
	if len(sender.Envelopes()) != 1 {
		t.Fatalf("expected ack envelope even for duplicate, got %d", len(sender.Envelopes()))
	}
	// ...but must not produce a second CommandResult (no re-execution happened).
	if len(sender.Results()) != 0 {
		t.Fatalf("expected no result echoed for in-flight duplicate, got %d", len(sender.Results()))
	}
}

func TestBeginDurableCommandReplaysCachedResultAfterFinish(t *testing.T) {
	sender := &MockSender{}
	commandID := "cmd-finished-1"

	finishDurableCommand(commandID, protocol.CommandResult{CommandID: commandID, Output: "deployed"})
	defer completedDurableCommands.Delete(commandID)

	if beginDurableCommand(sender, commandID, "msg-2") {
		t.Fatal("expected redelivery of an already-finished command to be rejected")
	}

	results := sender.Results()
	if len(results) != 1 || results[0].Output != "deployed" {
		t.Fatalf("expected cached result to be replayed, got %+v", results)
	}
}

func TestBeginDurableCommandExpiresCachedResult(t *testing.T) {
	sender := &MockSender{}
	commandID := "cmd-expired-1"

	completedDurableCommands.Store(commandID, durableResultEntry{
		result: protocol.CommandResult{CommandID: commandID, Output: "stale"},
		at:     time.Now().Add(-2 * completedDurableRetention),
	})
	defer completedDurableCommands.Delete(commandID)

	if !beginDurableCommand(sender, commandID, "msg-3") {
		t.Fatal("expected expired cache entry to allow re-execution")
	}
	if len(sender.Results()) != 0 {
		t.Fatalf("expected no cached result replayed once expired, got %d", len(sender.Results()))
	}
}

func TestHandleDeployDedupesRedeliveredDuplicate(t *testing.T) {
	InitSemaphores(2, 2, 2, 2)
	sender := &MockSender{}
	commandID := "cmd-deploy-dup-1"

	finishDurableCommand(commandID, protocol.CommandResult{CommandID: commandID, Output: "already-deployed"})
	defer completedDurableCommands.Delete(commandID)

	HandleDeploy(sender, map[string]interface{}{
		"command_id":       commandID,
		"message_id":       "msg-dup-1",
		"stack_id":         "stack-1",
		"compose_file_b64": "",
	})

	results := sender.Results()
	if len(results) != 1 || results[0].Output != "already-deployed" {
		t.Fatalf("expected cached deploy result to be replayed instead of re-executing, got %+v", results)
	}
}
