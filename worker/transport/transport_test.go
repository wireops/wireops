package transport

import (
	"os"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/worker/metrics"
)

func TestQueueBounds(t *testing.T) {
	// Setup env limit to 5
	os.Setenv("WORKER_MAX_QUEUED_MESSAGES", "5")
	defer os.Unsetenv("WORKER_MAX_QUEUED_MESSAGES")

	// Clear queues
	queuedEnvelopesMu.Lock()
	queuedEnvelopes = nil
	queuedEnvelopesMu.Unlock()

	completedJobsMu.Lock()
	completedJobs = nil
	completedJobsMu.Unlock()

	atomic.StoreUint64(&metrics.DroppedMessagesTotal, 0)

	// Append 10 envelopes
	for i := 1; i <= 10; i++ {
		appendQueuedEnvelope([]byte("msg-" + strconv.Itoa(i)))
	}

	// Check that we only kept the last 5 (FIFO)
	queuedEnvelopesMu.Lock()
	qLen := len(queuedEnvelopes)
	firstVal := string(queuedEnvelopes[0])
	lastVal := string(queuedEnvelopes[qLen-1])
	queuedEnvelopesMu.Unlock()

	if qLen != 5 {
		t.Errorf("expected queued envelopes length 5, got %d", qLen)
	}
	if firstVal != "msg-6" {
		t.Errorf("expected oldest msg to be msg-6, got %q", firstVal)
	}
	if lastVal != "msg-10" {
		t.Errorf("expected newest msg to be msg-10, got %q", lastVal)
	}

	droppedCount := atomic.LoadUint64(&metrics.DroppedMessagesTotal)
	if droppedCount != 5 {
		t.Errorf("expected metrics.DroppedMessagesTotal to be 5, got %d", droppedCount)
	}

	// Repeat for completedJobs queue
	for i := 1; i <= 10; i++ {
		appendCompletedJob(protocol.JobCompletedMessage{
			JobRunID: "run-" + strconv.Itoa(i),
		})
	}

	completedJobsMu.Lock()
	jLen := len(completedJobs)
	firstJob := completedJobs[0].JobRunID
	lastJob := completedJobs[jLen-1].JobRunID
	completedJobsMu.Unlock()

	if jLen != 5 {
		t.Errorf("expected completed jobs length 5, got %d", jLen)
	}
	if firstJob != "run-6" {
		t.Errorf("expected oldest job to be run-6, got %q", firstJob)
	}
	if lastJob != "run-10" {
		t.Errorf("expected newest job to be run-10, got %q", lastJob)
	}

	// Total drops should now be 10 (5 from envelopes + 5 from jobs)
	droppedCount = atomic.LoadUint64(&metrics.DroppedMessagesTotal)
	if droppedCount != 10 {
		t.Errorf("expected total metrics.DroppedMessagesTotal to be 10, got %d", droppedCount)
	}
}
