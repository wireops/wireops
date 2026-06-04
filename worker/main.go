package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	gosync "sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/worker/api"
	"github.com/wireops/wireops/worker/executor"
	wsync "github.com/wireops/wireops/worker/sync"
	"github.com/wireops/wireops/pkg/logger"
)

var (
	activeJobs        gosync.Map
	connWriteMu       gosync.Mutex
	activeConnMu      gosync.RWMutex
	activeConn        *websocket.Conn
	completedJobsMu   gosync.Mutex
	completedJobs     []protocol.JobCompletedMessage
	queuedEnvelopesMu gosync.Mutex
	queuedEnvelopes   [][]byte
	taskSemaphore     chan struct{}
)

const (
	maxBackoff     = 5 * time.Minute
	initialBackoff = 5 * time.Second
)

type disconnectReason int

const (
	reasonUnknown disconnectReason = iota
	reasonRevoked
	reasonShutdown
)

func main() {
	logger.InitLogger()
	serverURL := os.Getenv("SERVER_URL")
	workerToken := os.Getenv("WORKER_TOKEN")
	hostname := os.Getenv("HOSTNAME")

	if serverURL == "" {
		log.Fatal("SERVER_URL must be set")
	}
	if workerToken == "" {
		log.Fatal("WORKER_TOKEN must be set")
	}
	if hostname == "" {
		h, err := os.Hostname()
		if err == nil {
			hostname = h
		} else {
			hostname = "unknown-worker"
		}
	}

	concurrencyLimit := 3
	if limitStr := os.Getenv("WORKER_MAX_CONCURRENT_TASKS"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			concurrencyLimit = val
		}
	}
	taskSemaphore = make(chan struct{}, concurrencyLimit)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	tags := parseTags(os.Getenv("WORKER_TAGS"))
	backoff := initialBackoff

	for {
		reason := runSession(serverURL, workerToken, hostname, tags, sigChan)

		switch reason {
		case reasonRevoked:
			log.Fatal("[worker] token rejected by server. Issue a new token to continue.")

		case reasonShutdown:
			log.Println("[worker] shutting down")
			return

		default:
			log.Printf("[worker] disconnected reconnecting_in=%v", backoff)
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
		}
	}
}

// runSession handles one full connect-register-websocket cycle.
// Returns the reason the session ended so the caller can decide what to do.
func runSession(serverURL, workerToken, hostname string, tags []string, sigChan <-chan os.Signal) disconnectReason {
	client := api.NewClient()

	for i := 1; i <= 5; i++ {
		err := api.Register(client, serverURL, workerToken, hostname, "1.0.0", tags)
		if err == nil {
			break
		}
		if errors.Is(err, api.ErrRevoked) || errors.Is(err, api.ErrUnauthorized) {
			return reasonRevoked
		}

		select {
		case <-sigChan:
			return reasonShutdown
		default:
		}

		log.Printf("[worker] registration attempt=%d error=%v retrying_in=5s", i, err)
		if i == 5 {
			log.Printf("[worker] registration failed after 5 attempts")
			return reasonUnknown
		}

		select {
		case <-sigChan:
			return reasonShutdown
		case <-time.After(5 * time.Second):
		}
	}

	conn, err := wsync.Connect(serverURL, workerToken)
	if err != nil {
		log.Printf("[worker] websocket connect error=%v", err)
		return reasonUnknown
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

	// Send initial heartbeat to announce active jobs immediately on reconnect
	sendInitialHeartbeat()

	// Flush any queued completions that finished while disconnected
	flushCompletedJobs()

	// Flush any queued result/envelope messages
	flushQueuedEnvelopes()

	disconnectCh := make(chan disconnectReason, 1)

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
		case <-sigChan:
			return reasonShutdown

		case reason := <-disconnectCh:
			return reason

		case <-ticker.C:
			var activeIDs []string
			activeJobs.Range(func(k, _ any) bool {
				activeIDs = append(activeIDs, k.(string))
				return true
			})
			heartbeat, _ := json.Marshal(protocol.Envelope{
				Type:    protocol.MsgHeartbeat,
				Payload: protocol.HeartbeatPayload{ActiveJobRunIDs: activeIDs},
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

func runThrottled(msgType protocol.MessageType, fn func()) {
	select {
	case taskSemaphore <- struct{}{}:
		// Acquired immediately
	default:
		log.Printf("[worker] task %s queued due to concurrency limits", msgType)
		taskSemaphore <- struct{}{}
	}
	defer func() { <-taskSemaphore }()
	fn()
}

func readLoop(conn *websocket.Conn, disconnectCh chan<- disconnectReason) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if closeErr, ok := err.(*websocket.CloseError); ok && closeErr.Code == websocket.ClosePolicyViolation {
				disconnectCh <- reasonRevoked
				return
			}
			log.Printf("[worker] websocket read error=%v", err)
			disconnectCh <- reasonUnknown
			return
		}

		var env protocol.Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			log.Printf("[worker] message parse error=%v", err)
			continue
		}

		switch env.Type {
		case protocol.MsgDeploy:
			go runThrottled(env.Type, func() { handleDeploy(env.Payload) })
		case protocol.MsgRedeploy:
			go runThrottled(env.Type, func() { handleRedeploy(env.Payload) })
		case protocol.MsgTeardown:
			go runThrottled(env.Type, func() { handleTeardown(env.Payload) })
		case protocol.MsgProbe:
			go handleProbe(env.Payload)
		case protocol.MsgInspect:
			go handleInspect(env.Payload)
		case protocol.MsgGetStatus:
			go handleGetStatus(env.Payload)
		case protocol.MsgGetResources:
			go handleGetResources(env.Payload)
		case protocol.MsgStopContainer:
			go handleStopContainer(env.Payload)
		case protocol.MsgRestartContainer:
			go handleRestartContainer(env.Payload)
		case protocol.MsgDiscoverProjects:
			go handleDiscoverProjects(env.Payload)
		case protocol.MsgReadFile:
			go handleReadFile(env.Payload)
		case protocol.MsgRunJob:
			go handleRunJob(env.Payload)
		case protocol.MsgKillJob:
			go handleKillJob(env.Payload)

		default:
			log.Printf("[worker] unknown message type=%s", env.Type)
		}
	}
}

func handleDeploy(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.DeployCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid deploy payload error=%v", err)
		return
	}
	result := executor.Deploy(context.Background(), cmd)
	sendResult(result)
}

func handleRedeploy(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.RedeployCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid redeploy payload error=%v", err)
		return
	}
	result := executor.Redeploy(context.Background(), cmd)
	sendResult(result)
}

func handleTeardown(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.TeardownCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid teardown payload error=%v", err)
		return
	}
	result := executor.Teardown(context.Background(), cmd)
	sendResult(result)
}

func handleProbe(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.ProbeCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid probe payload error=%v", err)
		return
	}
	result := executor.Probe(context.Background(), cmd)
	sendResult(result)
}

func handleInspect(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.InspectCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid inspect payload error=%v", err)
		return
	}
	result := executor.Inspect(context.Background(), cmd)
	sendResult(result)
}

func handleGetResources(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.GetResourcesCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid get_resources payload error=%v", err)
		return
	}
	result := executor.GetResources(context.Background(), cmd)
	sendResult(result)
}

func handleGetStatus(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.GetStatusCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid get_status payload error=%v", err)
		return
	}
	result := executor.GetStatus(context.Background(), cmd)
	sendResult(result)
}

func handleStopContainer(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.ContainerActionCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid stop_container payload error=%v", err)
		return
	}
	result := executor.StopContainer(context.Background(), cmd)
	sendResult(result)
}

func handleRestartContainer(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.ContainerActionCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid restart_container payload error=%v", err)
		return
	}
	result := executor.RestartContainer(context.Background(), cmd)
	sendResult(result)
}

func handleDiscoverProjects(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.DiscoverProjectsCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid discover_projects payload error=%v", err)
		return
	}
	result := executor.DiscoverProjects(context.Background(), cmd)
	sendResult(result)
}

func handleReadFile(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.ReadFileCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid read_file payload error=%v", err)
		return
	}
	result := executor.ReadFile(context.Background(), cmd)
	sendResult(result)
}

func handleRunJob(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.RunJobCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid run_job payload error=%v", err)
		return
	}

	activeJobs.Store(cmd.JobRunID, struct{}{})
	receivedAt := time.Now()

	// Immediate start acknowledgment to the server:
	sendResult(protocol.CommandResult{CommandID: cmd.CommandID, Output: "started"})

	// Run the actual job execution throttled in a background goroutine!
	go runThrottled(protocol.MsgRunJob, func() {
		startedAt := time.Now()
		// Call executor.RunJob synchronously. It blocks until the container completes.
		msg := executor.RunJob(cmd)
		finishedAt := time.Now()

		queueTime := startedAt.UnixMilli() - receivedAt.UnixMilli()
		if queueTime < 0 {
			queueTime = 0
		}
		msg.QueueTimeMs = queueTime
		msg.ExecutionTimeMs = finishedAt.UnixMilli() - startedAt.UnixMilli()

		// Send completion report
		reportJobCompleted(msg)
	})
}

func handleKillJob(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.KillJobCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid kill_job payload error=%v", err)
		return
	}
	result := executor.KillJob(cmd)
	sendResult(result)
}

func sendResult(result protocol.CommandResult) {
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
		queuedEnvelopesMu.Lock()
		queuedEnvelopes = append(queuedEnvelopes, msg)
		queuedEnvelopesMu.Unlock()
		return
	}
	connWriteMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = c.WriteMessage(websocket.TextMessage, msg)
	connWriteMu.Unlock()
	if err != nil {
		log.Printf("[worker] result send error=%v. Enqueueing for retry.", err)
		queuedEnvelopesMu.Lock()
		queuedEnvelopes = append(queuedEnvelopes, msg)
		queuedEnvelopesMu.Unlock()
	}
}

func flushQueuedEnvelopes() {
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
		queuedEnvelopesMu.Lock()
		queuedEnvelopes = append(remaining, queuedEnvelopes...)
		queuedEnvelopesMu.Unlock()
	}
}

func sendInitialHeartbeat() {
	var activeIDs []string
	activeJobs.Range(func(k, _ any) bool {
		activeIDs = append(activeIDs, k.(string))
		return true
	})
	hb, _ := json.Marshal(protocol.Envelope{
		Type:    protocol.MsgHeartbeat,
		Payload: protocol.HeartbeatPayload{ActiveJobRunIDs: activeIDs},
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

func reportJobCompleted(msg protocol.JobCompletedMessage) {
	activeJobs.Delete(msg.JobRunID)

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
		completedJobsMu.Lock()
		completedJobs = append(completedJobs, msg)
		completedJobsMu.Unlock()

		// In reportJobCompleted, when a writeErr occurs and we append to completedJobs,
		// we immediately trigger a retry via flushCompletedJobs asynchronously.
		// To avoid deadlocks, completedJobsMu is unlocked before calling flushCompletedJobs.
		// This retry is only triggered if the current activeConn is non-nil/healthy.
		activeConnMu.RLock()
		conn := activeConn
		activeConnMu.RUnlock()
		if conn != nil {
			go flushCompletedJobs()
		}
	} else {
		log.Printf("[worker] job completion sent successfully job_run=%s", msg.JobRunID)
	}
}

func flushCompletedJobs() {
	completedJobsMu.Lock()
	queue := completedJobs
	completedJobs = nil
	completedJobsMu.Unlock()

	if len(queue) == 0 {
		return
	}

	log.Printf("[worker] flushing %d queued completed jobs...", len(queue))
	for _, msg := range queue {
		reportJobCompleted(msg)
	}
}

func parseTags(raw string) []string {
	var tags []string
	for _, t := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(t); trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	return tags
}

func unmarshalPayload[T any](payload interface{}) (T, error) {
	var zero T
	b, err := json.Marshal(payload)
	if err != nil {
		return zero, err
	}
	if err := json.Unmarshal(b, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}
