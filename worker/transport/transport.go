package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wireops/wireops/internal/protocol"
	wiretls "github.com/wireops/wireops/pkg/tls"
	"github.com/wireops/wireops/worker/api"
	"github.com/wireops/wireops/worker/handlers"
	"github.com/wireops/wireops/worker/metrics"
	"github.com/wireops/wireops/worker/telemetry"
)

type DisconnectReason int

const (
	ReasonUnknown DisconnectReason = iota
	ReasonRevoked
	ReasonShutdown
)

var (
	connWriteMu       sync.Mutex
	activeConnMu      sync.RWMutex
	activeConn        *websocket.Conn
	completedJobsMu   sync.Mutex
	completedJobs     []protocol.JobCompletedMessage
	isFlushingJobs    bool
	isFlushingJobsMu  sync.Mutex
	queuedEnvelopesMu sync.Mutex
	queuedEnvelopes   [][]byte
	currentStackDir   string
)

type WorkerSender struct{}

func (w WorkerSender) SendResult(result protocol.CommandResult) {
	msg, err := json.Marshal(protocol.Envelope{Type: protocol.MsgResult, Payload: result})
	if err != nil {
		log.Printf("[worker] result marshal error=%v", err)
		return
	}
	activeConnMu.RLock()
	c := activeConn
	activeConnMu.RUnlock()
	if c == nil {
		log.Printf("[worker] result send failed: no active connection (command %s). Enqueueing for retry.", result.CommandID)
		appendQueuedEnvelope(msg)
		return
	}
	connWriteMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = c.WriteMessage(websocket.TextMessage, msg)
	connWriteMu.Unlock()
	if err != nil {
		log.Printf("[worker] result send error=%v. Enqueueing for retry.", err)
		appendQueuedEnvelope(msg)
	}
}

func (w WorkerSender) SendEnvelope(env protocol.Envelope) {
	msg, err := json.Marshal(env)
	if err != nil {
		log.Printf("[worker] envelope marshal error=%v", err)
		return
	}
	activeConnMu.RLock()
	c := activeConn
	activeConnMu.RUnlock()
	if c == nil {
		log.Printf("[worker] envelope send failed: no active connection. Enqueueing for retry.")
		appendQueuedEnvelope(msg)
		return
	}
	connWriteMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = c.WriteMessage(websocket.TextMessage, msg)
	connWriteMu.Unlock()
	if err != nil {
		log.Printf("[worker] envelope send error=%v. Enqueueing for retry.", err)
		appendQueuedEnvelope(msg)
	}
}

func (w WorkerSender) ReportJobCompleted(msg protocol.JobCompletedMessage) {
	activeJobsDelete(msg.JobRunID)

	envelope := protocol.Envelope{
		Type:    protocol.MsgJobCompleted,
		Payload: msg,
	}
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		log.Printf("[worker] job completion marshal error=%v", err)
		return
	}

	activeConnMu.RLock()
	c := activeConn
	activeConnMu.RUnlock()

	var writeErr error
	if c != nil {
		connWriteMu.Lock()
		_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
		writeErr = c.WriteMessage(websocket.TextMessage, envelopeBytes)
		connWriteMu.Unlock()
	} else {
		writeErr = errors.New("no active connection")
	}

	if writeErr != nil {
		log.Printf("[worker] job completion send error job_run=%s error=%v. Queueing for retry.", msg.JobRunID, writeErr)
		appendCompletedJob(msg)

		go TriggerCompletedJobsFlush()
	} else {
		log.Printf("[worker] job completion sent successfully job_run=%s", msg.JobRunID)
	}
}

func (w WorkerSender) QueuedEnvelopesLen() int {
	queuedEnvelopesMu.Lock()
	defer queuedEnvelopesMu.Unlock()
	return len(queuedEnvelopes)
}

func (w WorkerSender) QueuedJobsLen() int {
	completedJobsMu.Lock()
	defer completedJobsMu.Unlock()
	return len(completedJobs)
}

func activeJobsDelete(jobRunID string) {
	handlers.ActiveJobs.Delete(jobRunID)
}

func appendQueuedEnvelope(msg []byte) {
	limit := 1000
	if limitStr := os.Getenv("WORKER_MAX_QUEUED_MESSAGES"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			limit = val
		}
	}

	queuedEnvelopesMu.Lock()
	defer queuedEnvelopesMu.Unlock()

	if len(queuedEnvelopes) >= limit {
		log.Printf("[worker] queuedEnvelopes limit reached (%d), dropping oldest result message", limit)
		queuedEnvelopes = queuedEnvelopes[1:]
		atomic.AddUint64(&metrics.DroppedMessagesTotal, 1)
	}
	queuedEnvelopes = append(queuedEnvelopes, msg)
}

func appendCompletedJob(msg protocol.JobCompletedMessage) {
	limit := 1000
	if limitStr := os.Getenv("WORKER_MAX_QUEUED_MESSAGES"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			limit = val
		}
	}

	completedJobsMu.Lock()
	defer completedJobsMu.Unlock()

	if len(completedJobs) >= limit {
		log.Printf("[worker] completedJobs limit reached (%d), dropping oldest job completed message for job_run=%s", limit, completedJobs[0].JobRunID)
		completedJobs = completedJobs[1:]
		atomic.AddUint64(&metrics.DroppedMessagesTotal, 1)
	}
	completedJobs = append(completedJobs, msg)
}

func Connect(serverURL, token string) (*websocket.Conn, error) {
	dialer := *websocket.DefaultDialer

	if tlsCfg := wiretls.BuildClientTLSConfig(); tlsCfg != nil {
		log.Printf("[worker] custom TLS client config applied (WORKER_TLS_SKIP_VERIFY)")
		dialer.TLSClientConfig = tlsCfg
	}

	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}

	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	u.Scheme = scheme
	u.Path = "/worker/ws"

	headers := make(http.Header)
	headers.Set("X-Wireops-Worker-Token", strings.TrimSpace(token))

	log.Printf("[worker] websocket dialing url=%s", u.String())
	conn, resp, err := dialer.Dial(u.String(), headers)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("websocket dial failed (status %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("websocket dial failed: %w", err)
	}

	log.Printf("[worker] websocket connected")
	return conn, nil
}

func RunSession(serverURL, workerToken, hostname, stackDir string, tags []string, shutdownCtx context.Context) DisconnectReason {
	currentStackDir = stackDir
	client := api.NewClient()

	for i := 1; i <= 5; i++ {
		atomic.AddUint64(&metrics.ConnAttempts, 1)
		err := api.Register(client, serverURL, workerToken, hostname, "1.0.0", tags)
		if err == nil {
			break
		}
		if errors.Is(err, api.ErrRevoked) || errors.Is(err, api.ErrUnauthorized) {
			return ReasonRevoked
		}

		select {
		case <-shutdownCtx.Done():
			return ReasonShutdown
		default:
		}

		log.Printf("[worker] registration attempt=%d error=%v retrying_in=5s", i, err)
		if i == 5 {
			log.Printf("[worker] registration failed after 5 attempts")
			return ReasonUnknown
		}

		select {
		case <-shutdownCtx.Done():
			return ReasonShutdown
		case <-time.After(5 * time.Second):
		}
	}

	conn, err := Connect(serverURL, workerToken)
	if err != nil {
		log.Printf("[worker] websocket connect error=%v", err)
		return ReasonUnknown
	}
	defer func() {
		activeConnMu.Lock()
		if activeConn == conn {
			activeConn = nil
		}
		activeConnMu.Unlock()
		conn.Close()
	}()

	activeConnMu.Lock()
	activeConn = conn
	activeConnMu.Unlock()

	log.Println("[worker] connected")

	sendInitialHeartbeat()
	FlushCompletedJobs()
	FlushQueuedEnvelopes()

	disconnectCh := make(chan DisconnectReason, 1)

	go readLoop(conn, disconnectCh)

	intervalStr := os.Getenv("HEARTBEAT_INTERVAL")
	if intervalStr == "" {
		intervalStr = "30"
	}
	intervalSecs, parseErr := strconv.Atoi(intervalStr)
	if parseErr != nil || intervalSecs <= 0 {
		intervalSecs = 30
	}

	ticker := time.NewTicker(time.Duration(intervalSecs) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-shutdownCtx.Done():
			return ReasonShutdown

		case reason := <-disconnectCh:
			return reason

		case <-ticker.C:
			activeIDs := handlers.GetActiveJobsList()
			heartbeat, _ := json.Marshal(protocol.Envelope{
				Type: protocol.MsgHeartbeat,
				Payload: protocol.HeartbeatPayload{
					ActiveJobRunIDs: activeIDs,
					WorkerInfo:      telemetry.CachedWorkerInfo,
					Telemetry:       telemetry.GetTelemetry(currentStackDir),
				},
			})
			activeConnMu.RLock()
			c := activeConn
			activeConnMu.RUnlock()
			if c != nil {
				connWriteMu.Lock()
				_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
				writeErr := c.WriteMessage(websocket.TextMessage, heartbeat)
				connWriteMu.Unlock()
				if writeErr != nil {
					log.Printf("[worker] heartbeat error=%v", writeErr)
				}
			}
		}
	}
}

func CloseActiveConnection() {
	activeConnMu.Lock()
	defer activeConnMu.Unlock()
	if activeConn != nil {
		_ = activeConn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "worker shutting down"),
			time.Now().Add(2*time.Second),
		)
		activeConn.Close()
		activeConn = nil
	}
}

func sendInitialHeartbeat() {
	activeIDs := handlers.GetActiveJobsList()
	hb, _ := json.Marshal(protocol.Envelope{
		Type: protocol.MsgHeartbeat,
		Payload: protocol.HeartbeatPayload{
			ActiveJobRunIDs: activeIDs,
			WorkerInfo:      telemetry.CachedWorkerInfo,
			Telemetry:       telemetry.GetTelemetry(currentStackDir),
		},
	})
	activeConnMu.RLock()
	c := activeConn
	activeConnMu.RUnlock()
	if c != nil {
		connWriteMu.Lock()
		_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_ = c.WriteMessage(websocket.TextMessage, hb)
		connWriteMu.Unlock()
		log.Printf("[worker] sent initial heartbeat: active_jobs=%d", len(activeIDs))
	}
}

func FlushQueuedEnvelopes() {
	queuedEnvelopesMu.Lock()
	queue := queuedEnvelopes
	queuedEnvelopes = nil
	queuedEnvelopesMu.Unlock()

	if len(queue) == 0 {
		return
	}

	log.Printf("[worker] flushing %d queued result/envelope messages...", len(queue))
	var remaining [][]byte
	for _, msg := range queue {
		activeConnMu.RLock()
		c := activeConn
		activeConnMu.RUnlock()

		if c == nil {
			log.Printf("[worker] flush failed: no active connection. Re-queueing remaining messages.")
			remaining = append(remaining, msg)
			continue
		}

		connWriteMu.Lock()
		_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err := c.WriteMessage(websocket.TextMessage, msg)
		connWriteMu.Unlock()

		if err != nil {
			log.Printf("[worker] flush send error=%v. Re-queueing message.", err)
			remaining = append(remaining, msg)
		}
	}

	if len(remaining) > 0 {
		limit := 1000
		if limitStr := os.Getenv("WORKER_MAX_QUEUED_MESSAGES"); limitStr != "" {
			if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
				limit = val
			}
		}
		queuedEnvelopesMu.Lock()
		combined := append(remaining, queuedEnvelopes...)
		if len(combined) > limit {
			droppedCount := len(combined) - limit
			log.Printf("[worker] queuedEnvelopes limit reached (%d) during retry, dropping oldest %d messages", limit, droppedCount)
			combined = combined[droppedCount:]
			atomic.AddUint64(&metrics.DroppedMessagesTotal, uint64(droppedCount))
		}
		queuedEnvelopes = combined
		queuedEnvelopesMu.Unlock()
	}
}

func TriggerCompletedJobsFlush() {
	isFlushingJobsMu.Lock()
	if isFlushingJobs {
		isFlushingJobsMu.Unlock()
		return
	}
	isFlushingJobs = true
	isFlushingJobsMu.Unlock()

	defer func() {
		isFlushingJobsMu.Lock()
		isFlushingJobs = false
		isFlushingJobsMu.Unlock()
	}()

	for {
		completedJobsMu.Lock()
		if len(completedJobs) == 0 {
			completedJobsMu.Unlock()
			break
		}
		queue := completedJobs
		completedJobs = nil
		completedJobsMu.Unlock()

		var remaining []protocol.JobCompletedMessage
		for _, msg := range queue {
			activeConnMu.RLock()
			c := activeConn
			activeConnMu.RUnlock()

			var writeErr error
			if c != nil {
				envelope := protocol.Envelope{
					Type:    protocol.MsgJobCompleted,
					Payload: msg,
				}
				envelopeBytes, _ := json.Marshal(envelope)
				connWriteMu.Lock()
				_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
				writeErr = c.WriteMessage(websocket.TextMessage, envelopeBytes)
				connWriteMu.Unlock()
			} else {
				writeErr = errors.New("no active connection")
			}

			if writeErr != nil {
				log.Printf("[worker] flush failed for job_run=%s: %v", msg.JobRunID, writeErr)
				remaining = append(remaining, msg)
			} else {
				log.Printf("[worker] flushed completed job_run=%s", msg.JobRunID)
			}
		}

		if len(remaining) > 0 {
			limit := 1000
			if limitStr := os.Getenv("WORKER_MAX_QUEUED_MESSAGES"); limitStr != "" {
				if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
					limit = val
				}
			}
			completedJobsMu.Lock()
			combined := append(remaining, completedJobs...)
			if len(combined) > limit {
				droppedCount := len(combined) - limit
				log.Printf("[worker] completedJobs limit reached (%d) during retry, dropping oldest %d messages", limit, droppedCount)
				combined = combined[droppedCount:]
				atomic.AddUint64(&metrics.DroppedMessagesTotal, uint64(droppedCount))
			}
			completedJobs = combined
			completedJobsMu.Unlock()
			break // Connection dropped, stop flushing
		}
	}
}

func FlushCompletedJobs() {
	TriggerCompletedJobsFlush()
}

func readLoop(conn *websocket.Conn, disconnectCh chan<- DisconnectReason) {
	sender := WorkerSender{}

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if closeErr, ok := err.(*websocket.CloseError); ok && closeErr.Code == websocket.ClosePolicyViolation {
				disconnectCh <- ReasonRevoked
				return
			}
			log.Printf("[worker] websocket read error=%v", err)
			disconnectCh <- ReasonUnknown
			return
		}

		var env protocol.Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			log.Printf("[worker] message parse error=%v", err)
			continue
		}

		switch env.Type {
		case protocol.MsgDeploy:
			handlers.DispatchThrottled(sender, handlers.HeavySemaphore, env.Type, env.Payload, handlers.HandleDeploy)
		case protocol.MsgRedeploy:
			handlers.DispatchThrottled(sender, handlers.HeavySemaphore, env.Type, env.Payload, handlers.HandleRedeploy)
		case protocol.MsgTeardown:
			handlers.DispatchThrottled(sender, handlers.HeavySemaphore, env.Type, env.Payload, handlers.HandleTeardown)
		case protocol.MsgProbe:
			handlers.DispatchThrottled(sender, handlers.LightSemaphore, env.Type, env.Payload, handlers.HandleProbe)
		case protocol.MsgInspect:
			handlers.DispatchThrottled(sender, handlers.LightSemaphore, env.Type, env.Payload, handlers.HandleInspect)
		case protocol.MsgGetStatus:
			handlers.DispatchThrottled(sender, handlers.LightSemaphore, env.Type, env.Payload, handlers.HandleGetStatus)
		case protocol.MsgGetResources:
			handlers.DispatchThrottled(sender, handlers.LightSemaphore, env.Type, env.Payload, handlers.HandleGetResources)
		case protocol.MsgStopContainer:
			handlers.DispatchThrottled(sender, handlers.InteractiveSemaphore, env.Type, env.Payload, handlers.HandleStopContainer)
		case protocol.MsgRestartContainer:
			handlers.DispatchThrottled(sender, handlers.InteractiveSemaphore, env.Type, env.Payload, handlers.HandleRestartContainer)
		case protocol.MsgGetContainerStats:
			handlers.DispatchThrottled(sender, handlers.InteractiveSemaphore, env.Type, env.Payload, handlers.HandleGetContainerStats)
		case protocol.MsgGetContainerLogs:
			handlers.DispatchThrottled(sender, handlers.InteractiveSemaphore, env.Type, env.Payload, handlers.HandleGetContainerLogs)
		case protocol.MsgDiscoverProjects:
			handlers.DispatchThrottled(sender, handlers.InteractiveSemaphore, env.Type, env.Payload, handlers.HandleDiscoverProjects)
		case protocol.MsgReadFile:
			handlers.DispatchThrottled(sender, handlers.InteractiveSemaphore, env.Type, env.Payload, handlers.HandleReadFile)
		case protocol.MsgRunJob:
			handlers.HandleRunJob(sender, env.Payload)
		case protocol.MsgKillJob:
			handlers.DispatchThrottled(sender, handlers.InteractiveSemaphore, env.Type, env.Payload, handlers.HandleKillJob)
		case protocol.MsgCancelCommand:
			go handlers.HandleCancelCommand(sender, env.Payload)
		case protocol.MsgGetMetrics:
			handlers.DispatchThrottled(sender, handlers.InteractiveSemaphore, env.Type, env.Payload, handlers.HandleGetMetrics)

		default:
			log.Printf("[worker] unknown message type=%s", env.Type)
		}
	}
}
