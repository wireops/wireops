package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"github.com/wireops/wireops/internal/buildinfo"
	"github.com/wireops/wireops/internal/protocol"
	wiretls "github.com/wireops/wireops/pkg/tls"
	"github.com/wireops/wireops/worker/api"
	"github.com/wireops/wireops/worker/handlers"
	"github.com/wireops/wireops/worker/metrics"
	"github.com/wireops/wireops/worker/spool"
	"github.com/wireops/wireops/worker/telemetry"
)

type DisconnectReason int

const (
	ReasonUnknown DisconnectReason = iota
	ReasonRevoked
	ReasonShutdown
)

var (
	connWriteMu     sync.Mutex
	activeConnMu    sync.RWMutex
	activeConn      *websocket.Conn
	currentStackDir string
	storeMu         sync.RWMutex
	outboxStore     *spool.Store
)

type WorkerSender struct{}

func (w WorkerSender) SendResult(result protocol.CommandResult) {
	if result.MessageID == "" {
		result.MessageID = newMessageID()
	}
	env := protocol.Envelope{Type: protocol.MsgResult, Payload: result}
	if err := persistAndSend(result.MessageID, "command_result", env); err != nil {
		log.Printf("[worker] result send failed command=%s message=%s error=%v", result.CommandID, result.MessageID, err)
	}
}

func (w WorkerSender) SendEnvelope(env protocol.Envelope) {
	if err := sendEnvelope(env); err != nil {
		log.Printf("[worker] envelope send failed type=%s error=%v", env.Type, err)
	}
}

func (w WorkerSender) ReportJobCompleted(msg protocol.JobCompletedMessage) {
	activeJobsDelete(msg.JobRunID)

	if msg.MessageID == "" {
		msg.MessageID = newMessageID()
	}
	env := protocol.Envelope{
		Type:    protocol.MsgJobCompleted,
		Payload: msg,
	}
	if err := persistAndSend(msg.MessageID, "job_completed", env); err != nil {
		log.Printf("[worker] job completion send failed job_run=%s message=%s error=%v", msg.JobRunID, msg.MessageID, err)
	}
}

func (w WorkerSender) QueuedEnvelopesLen() int {
	results, _ := pendingCounts()
	return results
}

func (w WorkerSender) QueuedJobsLen() int {
	_, jobs := pendingCounts()
	return jobs
}

func activeJobsDelete(jobRunID string) {
	handlers.ActiveJobs.Delete(jobRunID)
}

func setOutboxStore(store *spool.Store) {
	storeMu.Lock()
	outboxStore = store
	storeMu.Unlock()
}

func getOutboxStore() *spool.Store {
	storeMu.RLock()
	defer storeMu.RUnlock()
	return outboxStore
}

func persistAndSend(messageID, kind string, env protocol.Envelope) error {
	store := getOutboxStore()
	if store == nil {
		return fmt.Errorf("outbox store is not initialized")
	}
	if err := store.Enqueue(messageID, kind, env); err != nil {
		return err
	}
	return sendEnvelope(env)
}

func sendEnvelope(env protocol.Envelope) error {
	msg, err := json.Marshal(env)
	if err != nil {
		return err
	}

	activeConnMu.RLock()
	c := activeConn
	activeConnMu.RUnlock()
	if c == nil {
		return errors.New("no active connection")
	}

	connWriteMu.Lock()
	_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = c.WriteMessage(websocket.TextMessage, msg)
	connWriteMu.Unlock()
	return err
}

func pendingCounts() (results int, jobs int) {
	store := getOutboxStore()
	if store == nil {
		return 0, 0
	}
	pending, err := store.Pending()
	if err != nil {
		return 0, 0
	}
	for _, msg := range pending {
		switch msg.Kind {
		case "command_result":
			results++
		case "job_completed":
			jobs++
		}
	}
	return results, jobs
}

func resolveWebSocketURL(serverURL string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", err
	}

	scheme := u.Scheme
	switch scheme {
	case "ws", "wss":
		// Keep unchanged
	case "https":
		scheme = "wss"
	case "http":
		scheme = "ws"
	default:
		scheme = "ws"
	}
	u.Scheme = scheme
	u.Path = "/worker/ws"
	return u.String(), nil
}

func Connect(serverURL, token string) (*websocket.Conn, error) {
	dialer := *websocket.DefaultDialer

	if tlsCfg := wiretls.BuildClientTLSConfig(); tlsCfg != nil {
		log.Printf("[worker] custom TLS client config applied (WORKER_TLS_SKIP_VERIFY)")
		dialer.TLSClientConfig = tlsCfg
	}

	resolvedURL, err := resolveWebSocketURL(serverURL)
	if err != nil {
		return nil, err
	}

	headers := make(http.Header)
	headers.Set("X-Wireops-Worker-Token", strings.TrimSpace(token))

	log.Printf("[worker] websocket dialing url=%s", resolvedURL)
	conn, resp, err := dialer.Dial(resolvedURL, headers)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("websocket dial failed (status %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("websocket dial failed: %w", err)
	}

	log.Printf("[worker] websocket connected")
	return conn, nil
}

func RunSession(serverURL, workerToken, hostname, stackDir string, tags []string, shutdownCtx context.Context) (DisconnectReason, bool) {
	currentStackDir = stackDir
	store, err := spool.New(stackDir, workerToken)
	if err != nil {
		log.Printf("[worker] failed to initialize spool: %v", err)
		return ReasonUnknown, false
	}
	setOutboxStore(store)

	client := api.NewClient()

	for i := 1; i <= 5; i++ {
		atomic.AddUint64(&metrics.ConnAttempts, 1)
		err := api.Register(client, serverURL, workerToken, hostname, buildinfo.Version, tags)
		if err == nil {
			break
		}
		if errors.Is(err, api.ErrRevoked) || errors.Is(err, api.ErrUnauthorized) {
			_ = purgeOutbox()
			return ReasonRevoked, false
		}

		select {
		case <-shutdownCtx.Done():
			return ReasonShutdown, false
		default:
		}

		log.Printf("[worker] registration attempt=%d error=%v retrying_in=5s", i, err)
		if i == 5 {
			log.Printf("[worker] registration failed after 5 attempts")
			return ReasonUnknown, false
		}

		select {
		case <-shutdownCtx.Done():
			return ReasonShutdown, false
		case <-time.After(5 * time.Second):
		}
	}

	conn, err := Connect(serverURL, workerToken)
	if err != nil {
		log.Printf("[worker] websocket connect error=%v", err)
		return ReasonUnknown, false
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
	atomic.StoreInt64(&metrics.Connected, 1)
	defer atomic.StoreInt64(&metrics.Connected, 0)

	handlers.SetAcceptingWork(true)
	log.Println("[worker] connected")

	sendInitialHeartbeat()
	FlushPersistentMessages()

	disconnectCh := make(chan DisconnectReason, 1)
	go readLoop(conn, disconnectCh)

	intervalSecs := heartbeatIntervalSecs()
	ticker := time.NewTicker(time.Duration(intervalSecs) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-shutdownCtx.Done():
			handlers.SetAcceptingWork(false)
			drainAndFlush()
			return ReasonShutdown, true

		case reason := <-disconnectCh:
			if reason == ReasonRevoked {
				_ = purgeOutbox()
			}
			return reason, true

		case <-ticker.C:
			sendHeartbeat()
			FlushPersistentMessages()
		}
	}
}

func CloseActiveConnection() {
	activeConnMu.Lock()
	defer activeConnMu.Unlock()
	if activeConn != nil {
		connWriteMu.Lock()
		_ = activeConn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "worker shutting down"),
			time.Now().Add(2*time.Second),
		)
		connWriteMu.Unlock()
		activeConn.Close()
		activeConn = nil
	}
}

func PurgeSpool() error {
	return purgeOutbox()
}

func purgeOutbox() error {
	store := getOutboxStore()
	if store == nil {
		return nil
	}
	return store.Purge()
}

func sendInitialHeartbeat() {
	sendHeartbeat()
	activeIDs := handlers.GetActiveJobsList()
	log.Printf("[worker] sent initial heartbeat: active_jobs=%d", len(activeIDs))
}

func sendHeartbeat() {
	activeIDs := handlers.GetActiveJobsList()
	hb := protocol.Envelope{
		Type: protocol.MsgHeartbeat,
		Payload: protocol.HeartbeatPayload{
			ActiveJobRunIDs: activeIDs,
			WorkerInfo:      telemetry.CachedWorkerInfo,
			Telemetry:       telemetry.GetTelemetry(currentStackDir),
		},
	}
	if err := sendEnvelope(hb); err != nil {
		log.Printf("[worker] heartbeat error=%v", err)
	}
}

func FlushPersistentMessages() {
	store := getOutboxStore()
	if store == nil {
		return
	}
	pending, err := store.Pending()
	if err != nil {
		log.Printf("[worker] failed to list pending spool messages: %v", err)
		return
	}
	if len(pending) == 0 {
		return
	}

	for _, msg := range pending {
		if err := store.MarkAttempt(msg.MessageID); err != nil {
			log.Printf("[worker] failed to mark spool attempt message=%s error=%v", msg.MessageID, err)
		}
		atomic.AddUint64(&metrics.FlushAttemptsTotal, 1)
		if err := sendEnvelope(msg.Envelope); err != nil {
			atomic.AddUint64(&metrics.FlushFailedTotal, 1)
			log.Printf("[worker] failed to flush spool message=%s kind=%s error=%v", msg.MessageID, msg.Kind, err)
			return
		}
	}
}

func drainAndFlush() {
	timeout := shutdownTimeout()
	deadline := time.Now().Add(timeout)

	for {
		activeCount := handlers.GetActiveCommandsCount() + handlers.GetActiveJobsCount()
		if activeCount == 0 {
			break
		}
		if time.Now().After(deadline) {
			log.Printf("[worker] shutdown timeout exceeded while draining active work")
			break
		}
		log.Printf("[worker] draining %d active tasks/jobs before shutdown", activeCount)
		time.Sleep(500 * time.Millisecond)
	}

	for {
		FlushPersistentMessages()
		if pending, err := pendingTotal(); err == nil && pending == 0 {
			return
		}
		if time.Now().After(deadline) {
			log.Printf("[worker] shutdown timeout exceeded while waiting for spool ACKs")
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func pendingTotal() (int, error) {
	store := getOutboxStore()
	if store == nil {
		return 0, nil
	}
	return store.CountPending()
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
		case protocol.MsgAck:
			payloadBytes, _ := json.Marshal(env.Payload)
			var ack protocol.AckMessage
			if err := json.Unmarshal(payloadBytes, &ack); err == nil {
				if err := ackPersistentMessage(ack.MessageID); err != nil {
					log.Printf("[worker] ack handling failed message=%s error=%v", ack.MessageID, err)
				}
			}
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

func ackPersistentMessage(messageID string) error {
	store := getOutboxStore()
	if store == nil {
		return nil
	}
	if err := store.Ack(messageID); err != nil {
		return err
	}
	atomic.AddUint64(&metrics.FlushAckedTotal, 1)
	return nil
}

func heartbeatIntervalSecs() int {
	intervalStr := os.Getenv("HEARTBEAT_INTERVAL")
	if intervalStr == "" {
		return 30
	}
	intervalSecs, err := strconv.Atoi(intervalStr)
	if err != nil || intervalSecs <= 0 {
		return 30
	}
	return intervalSecs
}

func shutdownTimeout() time.Duration {
	if envTimeout := os.Getenv("WORKER_SHUTDOWN_TIMEOUT"); envTimeout != "" {
		if val, err := strconv.Atoi(envTimeout); err == nil && val > 0 {
			return time.Duration(val) * time.Second
		}
	}
	return 300 * time.Second
}

func newMessageID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("msg-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
