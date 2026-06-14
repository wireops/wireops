package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	gosync "sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wireops/wireops/internal/docker"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/pkg/logger"
	"github.com/wireops/wireops/worker/api"
	"github.com/wireops/wireops/worker/executor"
	wsync "github.com/wireops/wireops/worker/sync"
)

var (
	activeJobs        gosync.Map
	connWriteMu       gosync.Mutex
	activeConnMu      gosync.RWMutex
	activeConn        *websocket.Conn
	completedJobsMu   gosync.Mutex
	completedJobs     []protocol.JobCompletedMessage
	isFlushingJobs    bool
	isFlushingJobsMu  gosync.Mutex
	queuedEnvelopesMu gosync.Mutex
	queuedEnvelopes   [][]byte
	taskSemaphore     chan struct{}
	activeCommands    gosync.Map // commandID -> context.CancelFunc
	cachedWorkerInfo  *protocol.WorkerInfo
	concurrencyLimit  int

	// Atomic metric counters
	metricsConnAttempts       uint64
	metricsQueuedTasks        int64
	metricsTasksDeploy        uint64
	metricsTasksRedeploy      uint64
	metricsTasksTeardown      uint64
	metricsTasksProbe         uint64
	metricsTasksInspect       uint64
	metricsTasksSuccess       uint64
	metricsTasksError         uint64
	metricsTasksDurationSumNs uint64
	metricsJobsTotal          uint64
	metricsJobsSuccess        uint64
	metricsJobsError          uint64
	metricsJobsDurationSumNs  uint64
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

var lastCPUTotal, lastCPUIdle uint64

func main() {
	logger.InitLogger()
	sanitizeProcessPATH()
	cleanupLeftoverWorkdirs()
	initWorkerInfo()

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

	concurrencyLimit = 3
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
		atomic.AddUint64(&metricsConnAttempts, 1)
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
				Type: protocol.MsgHeartbeat,
				Payload: protocol.HeartbeatPayload{
					ActiveJobRunIDs: activeIDs,
					WorkerInfo:      cachedWorkerInfo,
					Telemetry:       getTelemetry(),
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

func runThrottled(msgType protocol.MessageType, fn func()) {
	atomic.AddInt64(&metricsQueuedTasks, 1)
	select {
	case taskSemaphore <- struct{}{}:
		// Acquired immediately
	default:
		log.Printf("[worker] task %s queued due to concurrency limits", msgType)
		taskSemaphore <- struct{}{}
	}
	atomic.AddInt64(&metricsQueuedTasks, -1)
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
		case protocol.MsgGetContainerStats:
			go handleGetContainerStats(env.Payload)
		case protocol.MsgGetContainerLogs:
			go handleGetContainerLogs(env.Payload)
		case protocol.MsgDiscoverProjects:
			go handleDiscoverProjects(env.Payload)
		case protocol.MsgReadFile:
			go handleReadFile(env.Payload)
		case protocol.MsgRunJob:
			go handleRunJob(env.Payload)
		case protocol.MsgKillJob:
			go handleKillJob(env.Payload)
		case protocol.MsgCancelCommand:
			go handleCancelCommand(env.Payload)
		case protocol.MsgGetMetrics:
			go handleGetMetrics(env.Payload)

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
	ctx, cancel := context.WithCancel(context.Background())
	activeCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		activeCommands.Delete(cmd.CommandID)
	}()
	start := time.Now()
	result := executor.Deploy(ctx, cmd)
	duration := time.Since(start)

	atomic.AddUint64(&metricsTasksDeploy, 1)
	atomic.AddUint64(&metricsTasksDurationSumNs, uint64(duration.Nanoseconds()))
	if result.Error != "" {
		atomic.AddUint64(&metricsTasksError, 1)
	} else {
		atomic.AddUint64(&metricsTasksSuccess, 1)
	}

	sendResult(result)
}

func handleRedeploy(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.RedeployCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid redeploy payload error=%v", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	activeCommands.Store(cmd.DeployCommand.CommandID, cancel)
	defer func() {
		cancel()
		activeCommands.Delete(cmd.DeployCommand.CommandID)
	}()
	start := time.Now()
	result := executor.Redeploy(ctx, cmd)
	duration := time.Since(start)

	atomic.AddUint64(&metricsTasksRedeploy, 1)
	atomic.AddUint64(&metricsTasksDurationSumNs, uint64(duration.Nanoseconds()))
	if result.Error != "" {
		atomic.AddUint64(&metricsTasksError, 1)
	} else {
		atomic.AddUint64(&metricsTasksSuccess, 1)
	}

	sendResult(result)
}

func handleTeardown(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.TeardownCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid teardown payload error=%v", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	activeCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		activeCommands.Delete(cmd.CommandID)
	}()
	start := time.Now()
	result := executor.Teardown(ctx, cmd)
	duration := time.Since(start)

	atomic.AddUint64(&metricsTasksTeardown, 1)
	atomic.AddUint64(&metricsTasksDurationSumNs, uint64(duration.Nanoseconds()))
	if result.Error != "" {
		atomic.AddUint64(&metricsTasksError, 1)
	} else {
		atomic.AddUint64(&metricsTasksSuccess, 1)
	}

	sendResult(result)
}

func handleProbe(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.ProbeCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid probe payload error=%v", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	activeCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		activeCommands.Delete(cmd.CommandID)
	}()
	start := time.Now()
	result := executor.Probe(ctx, cmd)
	duration := time.Since(start)

	atomic.AddUint64(&metricsTasksProbe, 1)
	atomic.AddUint64(&metricsTasksDurationSumNs, uint64(duration.Nanoseconds()))
	if result.Error != "" {
		atomic.AddUint64(&metricsTasksError, 1)
	} else {
		atomic.AddUint64(&metricsTasksSuccess, 1)
	}

	sendResult(result)
}

func handleInspect(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.InspectCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid inspect payload error=%v", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	activeCommands.Store(cmd.CommandID, cancel)
	defer func() {
		cancel()
		activeCommands.Delete(cmd.CommandID)
	}()
	start := time.Now()
	result := executor.Inspect(ctx, cmd)
	duration := time.Since(start)

	atomic.AddUint64(&metricsTasksInspect, 1)
	atomic.AddUint64(&metricsTasksDurationSumNs, uint64(duration.Nanoseconds()))
	if result.Error != "" {
		atomic.AddUint64(&metricsTasksError, 1)
	} else {
		atomic.AddUint64(&metricsTasksSuccess, 1)
	}

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

func handleGetContainerStats(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.GetContainerStatsCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid get_container_stats payload error=%v", err)
		return
	}
	result := executor.GetContainerStats(context.Background(), cmd)
	sendResult(result)
}

func handleGetContainerLogs(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.GetContainerLogsCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid get_container_logs payload error=%v", err)
		return
	}
	result := executor.GetContainerLogs(context.Background(), cmd)
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

	receivedAt := time.Now()

	// Immediate acknowledgment that the job is received and queued:
	sendResult(protocol.CommandResult{CommandID: cmd.CommandID, Output: "queued"})

	// Run the actual job execution throttled in a background goroutine!
	go runThrottled(protocol.MsgRunJob, func() {
		activeJobs.Store(cmd.JobRunID, struct{}{})
		defer activeJobs.Delete(cmd.JobRunID)

		startedAt := time.Now()
		// Call executor.RunJob synchronously. It blocks until the container completes.
		msg := executor.RunJob(cmd)
		finishedAt := time.Now()
		duration := time.Since(startedAt)

		atomic.AddUint64(&metricsJobsTotal, 1)
		atomic.AddUint64(&metricsJobsDurationSumNs, uint64(duration.Nanoseconds()))
		if msg.Success {
			atomic.AddUint64(&metricsJobsSuccess, 1)
		} else {
			atomic.AddUint64(&metricsJobsError, 1)
		}

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

func handleCancelCommand(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.CancelCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid cancel payload error=%v", err)
		return
	}
	if cancel, ok := activeCommands.Load(cmd.TargetCommandID); ok {
		log.Printf("[worker] cancelling command: %s", cmd.TargetCommandID)
		cancel.(context.CancelFunc)()
	} else {
		log.Printf("[worker] command %s not running or already finished", cmd.TargetCommandID)
	}
}

func handleGetMetrics(payload interface{}) {
	cmd, err := unmarshalPayload[protocol.GetMetricsCommand](payload)
	if err != nil {
		log.Printf("[worker] invalid get_metrics payload error=%v", err)
		return
	}
	resPayload := protocol.GetMetricsResult{
		CommandID: cmd.CommandID,
		Metrics:   serializeMetrics(),
	}
	msg, err := json.Marshal(protocol.Envelope{
		Type:    protocol.MsgGetMetricsResult,
		Payload: resPayload,
	})
	if err != nil {
		log.Printf("[worker] failed to marshal metrics result: %v", err)
		return
	}
	activeConnMu.RLock()
	c := activeConn
	activeConnMu.RUnlock()
	if c == nil {
		log.Printf("[worker] metrics send failed: no active connection (command %s). Enqueueing for retry.", cmd.CommandID)
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
		log.Printf("[worker] metrics send error=%v. Enqueueing for retry.", err)
		queuedEnvelopesMu.Lock()
		queuedEnvelopes = append(queuedEnvelopes, msg)
		queuedEnvelopesMu.Unlock()
	}
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
		Type: protocol.MsgHeartbeat,
		Payload: protocol.HeartbeatPayload{
			ActiveJobRunIDs: activeIDs,
			WorkerInfo:      cachedWorkerInfo,
			Telemetry:       getTelemetry(),
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

		go triggerCompletedJobsFlush()
	} else {
		log.Printf("[worker] job completion sent successfully job_run=%s", msg.JobRunID)
	}
}

func triggerCompletedJobsFlush() {
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
			completedJobsMu.Lock()
			completedJobs = append(remaining, completedJobs...)
			completedJobsMu.Unlock()
			break // Connection dropped, stop flushing
		}
	}
}

func flushCompletedJobs() {
	triggerCompletedJobsFlush()
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

func queryDockerVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dockerPath, err := lookPathSecure("docker")
	if err != nil {
		return ""
	}
	cmd := exec.CommandContext(ctx, dockerPath, "version", "--format", "{{.Server.Version}}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func queryComposeVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dockerPath, err := lookPathSecure("docker")
	if err != nil {
		return ""
	}
	cmd := exec.CommandContext(ctx, dockerPath, "compose", "version", "--short")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func cleanupLeftoverWorkdirs() {
	stackDirVar := strings.TrimSpace(os.Getenv("WORKER_STACK_DIR"))
	if stackDirVar == "" {
		stackDirVar = filepath.Join(os.TempDir(), "wireops")
	}
	stacksPath := filepath.Join(stackDirVar, "stacks")
	if _, err := os.Stat(stacksPath); os.IsNotExist(err) {
		return
	}

	log.Printf("[worker] using stack directory: %s (for security, ensure this path is backed by a tmpfs/in-memory filesystem)", stackDirVar)
	log.Printf("[worker] checking for leftover work directories in %s...", stacksPath)

	stackDirs, err := os.ReadDir(stacksPath)
	if err != nil {
		return
	}
	for _, sd := range stackDirs {
		if !sd.IsDir() {
			continue
		}
		sdPath := filepath.Join(stacksPath, sd.Name())
		cmdDirs, err := os.ReadDir(sdPath)
		if err != nil {
			continue
		}
		for _, cd := range cmdDirs {
			if !cd.IsDir() || !strings.HasPrefix(cd.Name(), "cmd-") {
				continue
			}
			pathToDelete := filepath.Join(sdPath, cd.Name())
			log.Printf("[worker] cleaning up leftover workdir: %s", pathToDelete)
			_ = os.RemoveAll(pathToDelete)
		}
	}
}

func initWorkerInfo() {
	cachedWorkerInfo = &protocol.WorkerInfo{
		DockerVersion:  queryDockerVersion(),
		ComposeVersion: queryComposeVersion(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
	}
	log.Printf("[worker] detected environment: os=%s arch=%s docker=%s compose=%s",
		cachedWorkerInfo.OS, cachedWorkerInfo.Arch, cachedWorkerInfo.DockerVersion, cachedWorkerInfo.ComposeVersion)
}

func getTelemetry() *protocol.TelemetryInfo {
	info := &protocol.TelemetryInfo{
		DockerOnline: false,
	}

	// 1. Check Docker daemon connectivity
	if cli, err := docker.NewClient(); err == nil {
		info.DockerOnline = true
		_ = cli.Close()
	}

	// 2. CPU Usage
	if runtime.GOOS == "linux" {
		if file, err := os.Open("/proc/stat"); err == nil {
			scanner := bufio.NewScanner(file)
			if scanner.Scan() {
				line := scanner.Text()
				fields := strings.Fields(line)
				if len(fields) >= 5 && fields[0] == "cpu" {
					var total, idle uint64
					for i, field := range fields[1:] {
						val, _ := strconv.ParseUint(field, 10, 64)
						total += val
						if i == 3 { // idle field
							idle = val
						}
					}
					if lastCPUTotal > 0 {
						deltaTotal := total - lastCPUTotal
						deltaIdle := idle - lastCPUIdle
						if deltaTotal > 0 {
							info.CPUUsagePercent = float64(deltaTotal-deltaIdle) / float64(deltaTotal) * 100.0
						}
					}
					lastCPUTotal = total
					lastCPUIdle = idle
				}
			}
			_ = file.Close()
		}
	} else {
		// Mock CPU for Darwin development
		info.CPUUsagePercent = 5.0
	}

	// 3. Memory Usage
	if runtime.GOOS == "linux" {
		if file, err := os.Open("/proc/meminfo"); err == nil {
			scanner := bufio.NewScanner(file)
			var totalMem, availMem float64
			for scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) >= 2 {
					if fields[0] == "MemTotal:" {
						totalMem, _ = strconv.ParseFloat(fields[1], 64)
					} else if fields[0] == "MemAvailable:" {
						availMem, _ = strconv.ParseFloat(fields[1], 64)
					}
				}
			}
			_ = file.Close()
			if totalMem > 0 {
				usedMem := totalMem - availMem
				info.MemoryUsagePercent = (usedMem / totalMem) * 100.0
			}
		}
	} else {
		// Mock Memory for Darwin development
		info.MemoryUsagePercent = 45.0
	}

	// 4. Disk Usage
	stackDirVar := strings.TrimSpace(os.Getenv("WORKER_STACK_DIR"))
	if stackDirVar == "" {
		stackDirVar = filepath.Join(os.TempDir(), "wireops")
	}
	_ = os.MkdirAll(stackDirVar, 0700)
	var stat syscall.Statfs_t
	if err := syscall.Statfs(stackDirVar, &stat); err == nil {
		totalDisk := stat.Blocks * uint64(stat.Bsize)
		freeDisk := stat.Bavail * uint64(stat.Bsize)
		if totalDisk > 0 {
			usedDisk := totalDisk - freeDisk
			info.DiskUsagePercent = float64(usedDisk) / float64(totalDisk) * 100.0
		}
	}

	return info
}

func serializeMetrics() string {
	var sb strings.Builder

	writeMetric := func(name, help, mType string, value interface{}, labels ...string) {
		sb.WriteString("# HELP " + name + " " + help + "\n")
		sb.WriteString("# TYPE " + name + " " + mType + "\n")
		sb.WriteString(name)
		if len(labels) > 0 {
			sb.WriteString("{" + strings.Join(labels, ",") + "}")
		}
		sb.WriteString(" " + fmt.Sprintf("%v", value) + "\n")
	}

	// 1. Connection
	writeMetric("wireops_worker_connected", "WebSocket connection status", "gauge", 1)
	writeMetric("wireops_worker_connection_attempts_total", "Total registration/connection attempts", "counter", atomic.LoadUint64(&metricsConnAttempts))

	// 2. Concurrency
	activeTasksCount := 0
	activeCommands.Range(func(_, _ any) bool {
		activeTasksCount++
		return true
	})
	writeMetric("wireops_worker_concurrency_limit", "Configured task concurrency limit", "gauge", concurrencyLimit)
	writeMetric("wireops_worker_active_tasks", "Currently active task executions", "gauge", activeTasksCount)
	writeMetric("wireops_worker_queued_tasks", "Tasks currently waiting in the semaphore queue", "gauge", atomic.LoadInt64(&metricsQueuedTasks))

	// 3. Task Executions
	writeMetric("wireops_worker_tasks_total", "Total stack tasks processed by type", "counter", atomic.LoadUint64(&metricsTasksDeploy), "type=\"deploy\"")
	sb.WriteString(fmt.Sprintf("wireops_worker_tasks_total{type=\"redeploy\"} %d\n", atomic.LoadUint64(&metricsTasksRedeploy)))
	sb.WriteString(fmt.Sprintf("wireops_worker_tasks_total{type=\"teardown\"} %d\n", atomic.LoadUint64(&metricsTasksTeardown)))
	sb.WriteString(fmt.Sprintf("wireops_worker_tasks_total{type=\"probe\"} %d\n", atomic.LoadUint64(&metricsTasksProbe)))
	sb.WriteString(fmt.Sprintf("wireops_worker_tasks_total{type=\"inspect\"} %d\n", atomic.LoadUint64(&metricsTasksInspect)))

	writeMetric("wireops_worker_tasks_outcome_total", "Total stack tasks outcomes", "counter", atomic.LoadUint64(&metricsTasksSuccess), "status=\"success\"")
	sb.WriteString(fmt.Sprintf("wireops_worker_tasks_outcome_total{status=\"error\"} %d\n", atomic.LoadUint64(&metricsTasksError)))

	writeMetric("wireops_worker_task_duration_seconds_sum", "Total time spent processing tasks in seconds", "counter", float64(atomic.LoadUint64(&metricsTasksDurationSumNs))/1e9)
	writeMetric("wireops_worker_task_duration_seconds_count", "Total number of tasks measured", "counter", atomic.LoadUint64(&metricsTasksSuccess)+atomic.LoadUint64(&metricsTasksError))

	// 4. Job Executions
	writeMetric("wireops_worker_jobs_total", "Total Docker jobs executed by outcome", "counter", atomic.LoadUint64(&metricsJobsSuccess), "status=\"success\"")
	sb.WriteString(fmt.Sprintf("wireops_worker_jobs_total{status=\"error\"} %d\n", atomic.LoadUint64(&metricsJobsError)))

	activeJobsCount := 0
	activeJobs.Range(func(_, _ any) bool {
		activeJobsCount++
		return true
	})
	writeMetric("wireops_worker_active_jobs", "Currently active Docker job runs", "gauge", activeJobsCount)

	writeMetric("wireops_worker_job_duration_seconds_sum", "Total time spent executing jobs in seconds", "counter", float64(atomic.LoadUint64(&metricsJobsDurationSumNs))/1e9)
	writeMetric("wireops_worker_job_duration_seconds_count", "Total number of Docker jobs measured", "counter", atomic.LoadUint64(&metricsJobsSuccess)+atomic.LoadUint64(&metricsJobsError))

	// 5. Queued Messages
	queuedEnvelopesMu.Lock()
	qEnvLen := len(queuedEnvelopes)
	queuedEnvelopesMu.Unlock()
	completedJobsMu.Lock()
	qJobsLen := len(completedJobs)
	completedJobsMu.Unlock()

	writeMetric("wireops_worker_queued_messages", "Outbound messages buffered in memory", "gauge", qEnvLen, "queue=\"results\"")
	sb.WriteString(fmt.Sprintf("wireops_worker_queued_messages{queue=\"completed_jobs\"} %d\n", qJobsLen))

	return sb.String()
}

func sanitizeProcessPATH() {
	safeDirs := []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin"}
	safePath := strings.Join(safeDirs, string(filepath.ListSeparator))
	os.Setenv("PATH", safePath)
}

func lookPathSecure(file string) (string, error) {
	safeDirs := []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin"}
	for _, dir := range safeDirs {
		path := filepath.Join(dir, file)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("executable %q not found in safe paths", file)
}
