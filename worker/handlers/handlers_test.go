package handlers

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/worker/metrics"
)

type MockSender struct {
	mu      sync.Mutex
	results []protocol.CommandResult
}

func (m *MockSender) SendResult(res protocol.CommandResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results = append(m.results, res)
}
func (m *MockSender) SendEnvelope(env protocol.Envelope)                  {}
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
