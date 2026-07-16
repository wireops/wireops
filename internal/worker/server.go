package worker

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/pocketbase/pocketbase/core"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/pkg/logger"
)

// newDispatchMessageID generates a unique identifier for a single delivery
// attempt of a server→worker command envelope.
func newDispatchMessageID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("dmsg-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// withDispatchMessageID returns a copy of cmd with its MessageID field set,
// for command types that participate in the durable server→worker queue
// (deploy, redeploy, teardown, run_job). Other command types are returned
// unchanged since they are not redelivered across reconnects.
func withDispatchMessageID(cmd interface{}, messageID string) interface{} {
	switch v := cmd.(type) {
	case protocol.DeployCommand:
		v.MessageID = messageID
		return v
	case protocol.RedeployCommand:
		v.MessageID = messageID
		return v
	case protocol.TeardownCommand:
		v.MessageID = messageID
		return v
	case protocol.RunJobCommand:
		v.MessageID = messageID
		return v
	default:
		return cmd
	}
}

// isDurableCommand reports whether msgType participates in the durable
// server→worker queue: persisted before send, redispatched verbatim if the
// worker reconnects before acking, and deduped by CommandID on the worker.
func isDurableCommand(msgType protocol.MessageType) bool {
	switch msgType {
	case protocol.MsgDeploy, protocol.MsgRedeploy, protocol.MsgTeardown, protocol.MsgRunJob:
		return true
	default:
		return false
	}
}

// pendingResult holds the channel to send a CommandResult back to the waiting
// caller, plus enough of the original envelope to resend it verbatim if the
// worker reconnects before the result arrives.
type pendingResult struct {
	ch        chan protocol.CommandResult
	workerID  string
	msgType   protocol.MessageType
	commandID string
	envelope  func(messageID string) protocol.Envelope
}

// WorkerServer handles authenticated connections from remote workers.
type WorkerServer struct {
	app       core.App
	workerSvc *Service
	engine    *gin.Engine
	upgrader  websocket.Upgrader

	// connMu protects connections, connWriteMu, pending, and workerTags maps.
	connMu       sync.RWMutex
	connections  map[string]*websocket.Conn // workerID → conn
	connWriteMu  map[string]*sync.Mutex     // workerID → write mutex
	pending      map[string]*pendingResult  // commandID → pending
	workerTags   map[string][]string        // workerID → tags declared via WORKER_TAGS
	seenMu       sync.Mutex
	seenMessages map[string]time.Time

	onConnect       func(workerID string)
	onDisconnect    func(workerID string)
	onJobCompleted  func(protocol.JobCompletedMessage)
	onHeartbeat     func(workerID string, activeIDs []string)
	onCommandOutput func(protocol.CommandOutputMessage)
}

// MTLSServer is kept as an internal alias while the codebase finishes moving
// away from the old naming.
type MTLSServer = WorkerServer

// SetOnConnect allows registering a callback to be notified when a worker successfully connects.
func (s *WorkerServer) SetOnConnect(f func(workerID string)) {
	s.onConnect = f
}

// SetOnDisconnect allows registering a callback to be notified when a worker disconnects.
func (s *WorkerServer) SetOnDisconnect(f func(workerID string)) {
	s.onDisconnect = f
}

// SetOnJobCompleted registers a callback invoked whenever a remote worker reports
// that a job container has exited. The callback is called in a new goroutine.
func (s *WorkerServer) SetOnJobCompleted(f func(protocol.JobCompletedMessage)) {
	s.onJobCompleted = f
}

// SetOnHeartbeat registers a callback invoked whenever a heartbeat is received from a worker.
func (s *WorkerServer) SetOnHeartbeat(f func(workerID string, activeIDs []string)) {
	s.onHeartbeat = f
}

// SetOnCommandOutput registers a callback invoked whenever a remote worker pushes an
// incremental output line for a running deploy/redeploy/teardown command. Unlike
// MsgResult, this is unsolicited and can arrive multiple times per command — the
// callback must not assume ordering guarantees beyond what CommandOutputMessage.Seq
// records, since transport-level retries can theoretically redeliver a line.
func (s *WorkerServer) SetOnCommandOutput(f func(protocol.CommandOutputMessage)) {
	s.onCommandOutput = f
}

// SetWorkerTags stores the tags reported by the worker at registration time.
func (s *WorkerServer) SetWorkerTags(workerID string, tags []string) {
	s.connMu.Lock()
	s.workerTags[workerID] = tags
	s.connMu.Unlock()
}

// GetWorkerTags returns the tags currently associated with the given worker.
// Returns an empty slice if the worker has no tags or is not registered.
func (s *WorkerServer) GetWorkerTags(workerID string) []string {
	s.connMu.RLock()
	tags, ok := s.workerTags[workerID]
	s.connMu.RUnlock()
	if !ok {
		return []string{}
	}
	return tags
}

// ClearWorkerTags removes the in-memory tags for the given worker.
// Called on disconnect and revocation so stale tags are not served.
func (s *WorkerServer) ClearWorkerTags(workerID string) {
	s.connMu.Lock()
	delete(s.workerTags, workerID)
	s.connMu.Unlock()
}

// GetWorkersByTags returns the IDs of all workers that are currently connected
// and whose tag set is a superset of the required tags. Empty required tags
// returns nil so callers can distinguish "no filter" from "no match".
func (s *WorkerServer) GetWorkersByTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}

	s.connMu.RLock()
	snapshot := make(map[string][]string, len(s.workerTags))
	for id, t := range s.workerTags {
		snapshot[id] = t
	}
	s.connMu.RUnlock()

	var result []string
	for workerID, workerTagList := range snapshot {
		if !s.IsConnected(workerID) {
			continue
		}
		if workerHasAllTags(workerTagList, tags) {
			result = append(result, workerID)
		}
	}
	return result
}

// workerHasAllTags reports whether workerTags contains every tag in required.
func workerHasAllTags(workerTags, required []string) bool {
	set := make(map[string]struct{}, len(workerTags))
	for _, t := range workerTags {
		set[t] = struct{}{}
	}
	for _, t := range required {
		if _, ok := set[t]; !ok {
			return false
		}
	}
	return true
}

func NewWorkerServer(app core.App, workerSvc *Service) *WorkerServer {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	if logger.IsDebug() {
		r.Use(gin.Logger())
	}
	s := &WorkerServer{
		app:       app,
		workerSvc: workerSvc,
		engine:    r,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		connections:  make(map[string]*websocket.Conn),
		connWriteMu:  make(map[string]*sync.Mutex),
		pending:      make(map[string]*pendingResult),
		workerTags:   make(map[string][]string),
		seenMessages: make(map[string]time.Time),
	}

	s.registerRoutes()
	return s
}

func NewMTLSServer(app core.App, _ interface{}, workerSvc *Service) *MTLSServer {
	return NewWorkerServer(app, workerSvc)
}

func (s *WorkerServer) registerRoutes() {
	s.engine.POST("/worker/register", s.handleRegister)
	s.engine.GET("/worker/ws", s.handleWebSocket)
}

// DisconnectWorker forcefully closes the active WebSocket connection for the given workerID, if any.
// Used immediately after revoking a worker so the connection drops without waiting for a heartbeat timeout.
func (s *WorkerServer) DisconnectWorker(workerID string) {
	s.connMu.Lock()
	conn, ok := s.connections[workerID]
	writeMu := s.connWriteMu[workerID]
	if ok {
		if writeMu != nil {
			writeMu.Lock()
		}
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_ = conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "worker revoked"),
		)
		if writeMu != nil {
			writeMu.Unlock()
		}
		conn.Close()
		delete(s.connections, workerID)
		delete(s.connWriteMu, workerID)
	}
	delete(s.workerTags, workerID)
	s.connMu.Unlock()
	if ok {
		logger.SafeLogf("[WORKER] Forcefully disconnected revoked worker: %s", workerID)
		if s.onDisconnect != nil {
			go s.onDisconnect(workerID)
		}
	}
}

// IsConnected reports whether the worker currently has an active WebSocket connection.
func (s *WorkerServer) IsConnected(workerID string) bool {
	s.connMu.RLock()
	_, ok := s.connections[workerID]
	s.connMu.RUnlock()
	return ok
}

// SendMessage sends a one-way message to a connected worker (no response expected).
func (s *WorkerServer) SendMessage(workerID string, msgType protocol.MessageType, payload interface{}) error {
	s.connMu.RLock()
	conn, ok := s.connections[workerID]
	writeMu := s.connWriteMu[workerID]
	s.connMu.RUnlock()

	if !ok {
		return fmt.Errorf("worker %s is not connected", workerID)
	}

	msg, err := json.Marshal(protocol.Envelope{Type: msgType, Payload: payload})
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	writeMu.Lock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = conn.WriteMessage(websocket.TextMessage, msg)
	writeMu.Unlock()
	return err
}

func (s *WorkerServer) SendAck(workerID, messageID string) error {
	if strings.TrimSpace(messageID) == "" {
		return nil
	}
	return s.SendMessage(workerID, protocol.MsgAck, protocol.AckMessage{MessageID: messageID})
}

func (s *WorkerServer) isDuplicateMessage(workerID, messageID string) bool {
	if strings.TrimSpace(messageID) == "" {
		return false
	}

	now := time.Now().UTC()
	key := workerID + ":" + messageID

	s.seenMu.Lock()
	defer s.seenMu.Unlock()

	for k, seenAt := range s.seenMessages {
		if now.Sub(seenAt) > 24*time.Hour {
			delete(s.seenMessages, k)
		}
	}

	if _, ok := s.seenMessages[key]; ok {
		return true
	}
	s.seenMessages[key] = now
	return false
}

// Dispatch sends a command to the connected remote worker and
// blocks until it receives the result (with a 5 minute timeout).
func (s *WorkerServer) Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error) {
	s.connMu.RLock()
	conn, ok := s.connections[workerID]
	writeMu := s.connWriteMu[workerID]
	s.connMu.RUnlock()

	var msgType protocol.MessageType
	var commandID string
	switch v := cmd.(type) {
	case protocol.DeployCommand:
		msgType = protocol.MsgDeploy
		commandID = v.CommandID
	case protocol.RedeployCommand:
		msgType = protocol.MsgRedeploy
		commandID = v.DeployCommand.CommandID
	case protocol.TeardownCommand:
		msgType = protocol.MsgTeardown
		commandID = v.CommandID
	case protocol.ProbeCommand:
		msgType = protocol.MsgProbe
		commandID = v.CommandID
	case protocol.InspectCommand:
		msgType = protocol.MsgInspect
		commandID = v.CommandID
	case protocol.GetStatusCommand:
		msgType = protocol.MsgGetStatus
		commandID = v.CommandID
	case protocol.GetResourcesCommand:
		msgType = protocol.MsgGetResources
		commandID = v.CommandID
	case protocol.ContainerActionCommand:
		commandID = v.CommandID
		if strings.HasPrefix(v.CommandID, "stop-container-") {
			msgType = protocol.MsgStopContainer
		} else if strings.HasPrefix(v.CommandID, "restart-container-") {
			msgType = protocol.MsgRestartContainer
		} else {
			logger.SafeLogf("[WORKER] unknown or malformed container action command ID: %s", v.CommandID)
			return protocol.CommandResult{}, fmt.Errorf("unknown or malformed container action command ID: %s", v.CommandID)
		}
	case protocol.GetContainerStatsCommand:
		msgType = protocol.MsgGetContainerStats
		commandID = v.CommandID
	case protocol.GetContainerLogsCommand:
		msgType = protocol.MsgGetContainerLogs
		commandID = v.CommandID
	case protocol.DiscoverProjectsCommand:
		msgType = protocol.MsgDiscoverProjects
		commandID = v.CommandID
	case protocol.ReadFileCommand:
		msgType = protocol.MsgReadFile
		commandID = v.CommandID
	case protocol.RunJobCommand:
		msgType = protocol.MsgRunJob
		commandID = v.CommandID
	case protocol.KillJobCommand:
		msgType = protocol.MsgKillJob
		commandID = v.CommandID
	case protocol.GetMetricsCommand:
		msgType = protocol.MsgGetMetrics
		commandID = v.CommandID
	default:
		return protocol.CommandResult{}, fmt.Errorf("unknown command type %T", cmd)
	}

	durable := isDurableCommand(msgType)

	// Non-durable commands (probes, log tails, container actions, etc.) keep
	// the original fail-fast behavior: no queueing, no redelivery.
	if !durable && !ok {
		return protocol.CommandResult{}, fmt.Errorf("worker %s is not connected", workerID)
	}

	messageID := newDispatchMessageID()
	cmdWithMessageID := withDispatchMessageID(cmd, messageID)

	envelopeBuilder := func(mid string) protocol.Envelope {
		return protocol.Envelope{Type: msgType, Payload: withDispatchMessageID(cmd, mid)}
	}

	pr := &pendingResult{
		ch:        make(chan protocol.CommandResult, 1),
		workerID:  workerID,
		msgType:   msgType,
		commandID: commandID,
		envelope:  envelopeBuilder,
	}
	s.connMu.Lock()
	s.pending[commandID] = pr
	s.connMu.Unlock()
	defer func() {
		s.connMu.Lock()
		delete(s.pending, commandID)
		s.connMu.Unlock()
	}()

	start := time.Now()

	if !ok {
		// Durable command with the worker currently offline: persist as
		// queued and rely on replayOnReconnect to send it once the worker
		// reconnects (or on the wait loop below timing out/cancelling).
		if _, logErr := s.workerSvc.LogCommandQueued(ctx, workerID, commandID, commandID, string(msgType), cmd, time.Now()); logErr != nil {
			logger.SafeLogf("[WORKER] Failed to log queued command: %v", logErr)
		}
	} else {
		// Log command start in the database
		if _, logErr := s.workerSvc.LogCommandDispatch(ctx, workerID, commandID, commandID, messageID, string(msgType), cmd); logErr != nil {
			logger.SafeLogf("[WORKER] Failed to log command start: %v", logErr)
		}

		msg, err := json.Marshal(protocol.Envelope{Type: msgType, Payload: cmdWithMessageID})
		if err != nil {
			_ = s.workerSvc.LogCommandFinish(commandID, "error", map[string]string{"error": err.Error()}, 0)
			return protocol.CommandResult{}, fmt.Errorf("failed to marshal command: %w", err)
		}

		writeMu.Lock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err = conn.WriteMessage(websocket.TextMessage, msg)
		writeMu.Unlock()
		if err != nil {
			elapsedMs := time.Since(start).Milliseconds()
			if !durable {
				// If conn.WriteMessage fails during Dispatch, we calculate the duration elapsed since start
				// and pass it to s.workerSvc.LogCommandFinish to log the error for commandID and workerID.
				_ = s.workerSvc.LogCommandFinish(commandID, "error", map[string]string{"error": err.Error()}, elapsedMs)
				return protocol.CommandResult{}, fmt.Errorf("failed to send command to worker %s: %w", workerID, err)
			}
			// Durable command: treat a write failure the same as "offline at
			// dispatch time" — fall through to wait for a reconnect replay
			// instead of losing the command.
			logger.SafeLogf("[WORKER] Failed to send durable command %s to %s, will retry on reconnect: %v", commandID, workerID, err)
			if _, logErr := s.workerSvc.LogCommandQueued(ctx, workerID, commandID, commandID, string(msgType), cmd, time.Now()); logErr != nil {
				logger.SafeLogf("[WORKER] Failed to log queued command after send failure: %v", logErr)
			}
		}
	}

	var result protocol.CommandResult
	var dispatchErr error

	select {
	case result = <-pr.ch:
		// Result received
	case <-ctx.Done():
		dispatchErr = ctx.Err()
	case <-time.After(5 * time.Minute):
		dispatchErr = fmt.Errorf("timed out waiting for worker %s response (command %s)", workerID, commandID)
	}

	durationMs := time.Since(start).Milliseconds()

	if dispatchErr != nil {
		status := "error"
		if errors.Is(dispatchErr, context.Canceled) {
			status = "cancelled"
		} else if errors.Is(dispatchErr, context.DeadlineExceeded) || strings.Contains(dispatchErr.Error(), "timed out") {
			status = "timed_out"
		}
		// Send CancelCommand to the worker so it can abort the command.
		_ = s.SendMessage(workerID, protocol.MsgCancelCommand, protocol.CancelCommand{
			CommandID:       "cancel-" + commandID,
			TargetCommandID: commandID,
		})
		_ = s.workerSvc.LogCommandFinish(commandID, status, map[string]string{"error": dispatchErr.Error()}, durationMs)
		return protocol.CommandResult{}, dispatchErr
	}

	status := "success"
	if result.Error != "" {
		status = "error"
	}
	_ = s.workerSvc.LogCommandFinish(commandID, status, result, durationMs)

	return result, nil
}

// replayPendingOnReconnect resends, in original dispatch order, every
// still-outstanding durable command for workerID over its new connection.
// The original caller of Dispatch is still blocked on pr.ch (or has already
// timed out, in which case the resend is harmless — the worker dedupes by
// CommandID), so no new Dispatch call is needed: only the wire message must
// go out again.
func (s *WorkerServer) replayPendingOnReconnect(workerID string, conn *websocket.Conn, writeMu *sync.Mutex) {
	s.connMu.RLock()
	var toReplay []*pendingResult
	for _, pr := range s.pending {
		if pr.workerID == workerID {
			toReplay = append(toReplay, pr)
		}
	}
	s.connMu.RUnlock()

	if len(toReplay) == 0 {
		return
	}

	for _, pr := range toReplay {
		messageID := newDispatchMessageID()
		env := pr.envelope(messageID)

		msg, err := json.Marshal(env)
		if err != nil {
			logger.SafeLogf("[WORKER] Failed to marshal replay envelope for command %s: %v", pr.commandID, err)
			continue
		}

		if _, err := s.workerSvc.LogCommandDispatch(context.Background(), workerID, pr.commandID, pr.commandID, messageID, string(pr.msgType), env.Payload); err != nil {
			logger.SafeLogf("[WORKER] Failed to log replay dispatch for command %s: %v", pr.commandID, err)
		}

		writeMu.Lock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err = conn.WriteMessage(websocket.TextMessage, msg)
		writeMu.Unlock()
		if err != nil {
			logger.SafeLogf("[WORKER] Failed to replay command %s to reconnected worker %s: %v", pr.commandID, workerID, err)
			continue
		}
		logger.SafeLogf("[WORKER] Replayed pending command %s to reconnected worker %s (message %s)", pr.commandID, workerID, messageID)
	}
}

func workerTokenFromRequest(r *http.Request) string {
	if token := strings.TrimSpace(r.Header.Get("X-Wireops-Worker-Token")); token != "" {
		return token
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func (s *WorkerServer) authenticateWorker(c *gin.Context, hostname string) (*core.Record, *core.Record, error) {
	token := workerTokenFromRequest(c.Request)
	workerRecord, tokenRecord, err := s.workerSvc.ActivateToken(token, hostname)
	if err != nil {
		return nil, nil, err
	}
	return workerRecord, tokenRecord, nil
}

func workerAuthStatus(err error) (int, string) {
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.Is(err, ErrTokenExpired):
		return http.StatusUnauthorized, "Worker token expired"
	case errors.Is(err, ErrTokenRevoked):
		return http.StatusForbidden, "Worker token revoked"
	case errors.Is(err, ErrTokenMissing), errors.Is(err, ErrTokenInvalid):
		return http.StatusUnauthorized, "Invalid worker token"
	default:
		return http.StatusInternalServerError, "Worker authentication failed"
	}
}

func (s *WorkerServer) handleRegister(c *gin.Context) {
	var req struct {
		Hostname  string   `json:"hostname"`
		IPAddress string   `json:"ip_address"`
		Version   string   `json:"version"`
		Tags      []string `json:"tags"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON body"})
		return
	}

	record, tokenRecord, err := s.authenticateWorker(c, req.Hostname)
	if err != nil {
		status, message := workerAuthStatus(err)
		c.JSON(status, gin.H{"error": message})
		return
	}

	if record.GetString("status") == "REVOKED" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Worker is revoked"})
		return
	}

	record.Set("hostname", req.Hostname)
	record.Set("fingerprint", "remote:"+tokenRecord.Id)

	if err := s.app.Save(record); err != nil {
		logger.SafeLogf("[WORKER] Failed to update worker registration %s: %v", record.Id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if err := s.workerSvc.UpdateLastSeen(record.Id); err != nil {
		logger.SafeLogf("[WORKER] best_effort last_seen failed worker_id=%s event=register error=%v", record.Id, err)
	}
	s.SetWorkerTags(record.Id, req.Tags)
	logger.SafeLogf("[WORKER] Initial registration completed for Worker: %s (%s) tags=%v", req.Hostname, record.Id, req.Tags)

	c.JSON(http.StatusOK, gin.H{"status": "registered", "worker_id": record.Id})
}

func (s *WorkerServer) handleWebSocket(c *gin.Context) {
	record, _, err := s.authenticateWorker(c, "")
	if err != nil {
		status, message := workerAuthStatus(err)
		c.JSON(status, gin.H{"error": message})
		return
	}
	workerID := record.Id

	if record.GetString("status") == "REVOKED" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Worker is revoked"})
		return
	}

	if err := s.workerSvc.UpdateLastSeen(workerID); err != nil {
		logger.SafeLogf("[WORKER] best_effort last_seen failed worker_id=%s event=websocket_connect error=%v", workerID, err)
	}
	logger.SafeLogf("[WORKER] Worker connected via WebSocket: %s", workerID)

	conn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.SafeLogf("[WORKER] Failed to upgrade websocket for %s: %v", workerID, err)
		return
	}
	defer func() {
		s.connMu.Lock()
		if s.connections[workerID] == conn {
			conn.Close()
			delete(s.connections, workerID)
			delete(s.connWriteMu, workerID)
			delete(s.workerTags, workerID)
			s.connMu.Unlock()
			logger.SafeLogf("[WORKER] Worker %s disconnected", workerID)
			if s.onDisconnect != nil {
				go s.onDisconnect(workerID)
			}
		} else {
			s.connMu.Unlock()
			conn.Close()
			logger.SafeLogf("[WORKER] Ignoring stale connection cleanup for worker %s", workerID)
		}
	}()

	s.connMu.Lock()
	if oldConn, exists := s.connections[workerID]; exists && oldConn != conn {
		oldConn.Close()
	}
	s.connections[workerID] = conn
	s.connWriteMu[workerID] = &sync.Mutex{}
	s.connMu.Unlock()

	if err := s.workerSvc.RecordHealthEvent(workerID, "online"); err != nil {
		logger.SafeLogf("[WORKER] best_effort health failed worker_id=%s event=online error=%v", workerID, err)
	}

	s.replayPendingOnReconnect(workerID, conn, s.connWriteMu[workerID])

	if s.onConnect != nil {
		go s.onConnect(workerID)
	}

	intervalStr := os.Getenv("HEARTBEAT_INTERVAL")
	if intervalStr == "" {
		intervalStr = "30"
	}
	intervalSecs, err := strconv.Atoi(intervalStr)
	if err != nil || intervalSecs <= 0 {
		intervalSecs = 30
	}
	timeoutDuration := time.Duration(intervalSecs*3) * time.Second

	_ = conn.SetReadDeadline(time.Now().Add(timeoutDuration))

	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			logger.SafeLogf("[WORKER] Worker %s disconnected: %v", workerID, err)
			if err := s.workerSvc.RecordHealthEvent(workerID, "offline"); err != nil {
				logger.SafeLogf("[WORKER] best_effort health failed worker_id=%s event=offline error=%v", workerID, err)
			}
			break
		}

		_ = conn.SetReadDeadline(time.Now().Add(timeoutDuration))

		if err := s.workerSvc.UpdateLastSeen(workerID); err != nil {
			logger.SafeLogf("[WORKER] best_effort last_seen failed worker_id=%s event=message error=%v", workerID, err)
		}

		if messageType != websocket.TextMessage {
			continue
		}

		var env protocol.Envelope
		if jsonErr := json.Unmarshal(p, &env); jsonErr != nil {
			logger.SafeLogf("[WORKER] Failed to parse message from %s: %v", workerID, jsonErr)
			continue
		}

		switch env.Type {
		case protocol.MsgAck:
			payloadBytes, _ := json.Marshal(env.Payload)
			var ack protocol.AckMessage
			if jsonErr := json.Unmarshal(payloadBytes, &ack); jsonErr == nil {
				// Ack of a durable command's receipt (worker got it, about to
				// execute). Distinct from the server's own MsgAck acking the
				// worker's result/job_completed messages.
				if err := s.workerSvc.LogCommandAck(ack.MessageID); err != nil {
					logger.SafeLogf("[WORKER] Failed to record command ack message=%s worker=%s: %v", ack.MessageID, workerID, err)
				}
			}

		case protocol.MsgHeartbeat:
			if err := s.workerSvc.RecordHealthEvent(workerID, "online"); err != nil {
				logger.SafeLogf("[WORKER] best_effort health failed worker_id=%s event=heartbeat error=%v", workerID, err)
			}
			payloadBytes, _ := json.Marshal(env.Payload)
			var hb protocol.HeartbeatPayload
			if jsonErr := json.Unmarshal(payloadBytes, &hb); jsonErr == nil {
				if len(hb.ActiveJobRunIDs) > 0 {
					logger.SafeLogf("[worker] heartbeat worker=%s active_jobs=%d runs=%v", workerID, len(hb.ActiveJobRunIDs), hb.ActiveJobRunIDs)
				}
				if hb.WorkerInfo != nil {
					if err := s.workerSvc.UpdateWorkerInfo(workerID, *hb.WorkerInfo); err != nil {
						logger.SafeLogf("[WORKER] Failed to update worker info for %s: %v", workerID, err)
					}
				}
				if hb.Telemetry != nil {
					if err := s.workerSvc.UpdateWorkerTelemetry(workerID, *hb.Telemetry); err != nil {
						logger.SafeLogf("[WORKER] Failed to update worker telemetry for %s: %v", workerID, err)
					}
				}
				if s.onHeartbeat != nil {
					go s.onHeartbeat(workerID, hb.ActiveJobRunIDs)
				}
			}

		case protocol.MsgJobCompleted:
			payloadBytes, _ := json.Marshal(env.Payload)
			var msg protocol.JobCompletedMessage
			if jsonErr := json.Unmarshal(payloadBytes, &msg); jsonErr == nil {
				if msg.MessageID != "" && s.isDuplicateMessage(workerID, msg.MessageID) {
					_ = s.SendAck(workerID, msg.MessageID)
					logger.SafeLogf("[WORKER] Ignoring duplicate job_completed message=%s worker=%s job_run=%s", msg.MessageID, workerID, msg.JobRunID)
					continue
				}
				logger.SafeLogf("[WORKER] job_completed run=%s worker=%s success=%v elapsed=%dms", msg.JobRunID, workerID, msg.Success, msg.DurationMs)
				_ = s.SendAck(workerID, msg.MessageID)
				if s.onJobCompleted != nil {
					go s.onJobCompleted(msg)
				}
			} else {
				logger.SafeLogf("[WORKER] Failed to parse job_completed from %s: %v", workerID, jsonErr)
			}

		case protocol.MsgCommandOutput:
			payloadBytes, _ := json.Marshal(env.Payload)
			var out protocol.CommandOutputMessage
			if jsonErr := json.Unmarshal(payloadBytes, &out); jsonErr == nil {
				if s.onCommandOutput != nil {
					go s.onCommandOutput(out)
				}
			} else {
				logger.SafeLogf("[WORKER] Failed to parse command_output from %s: %v", workerID, jsonErr)
			}

		case protocol.MsgResult:
			payloadBytes, _ := json.Marshal(env.Payload)
			var result protocol.CommandResult
			if jsonErr := json.Unmarshal(payloadBytes, &result); jsonErr == nil {
				if result.MessageID != "" && s.isDuplicateMessage(workerID, result.MessageID) {
					_ = s.SendAck(workerID, result.MessageID)
					logger.SafeLogf("[WORKER] Ignoring duplicate result message=%s worker=%s command=%s", result.MessageID, workerID, result.CommandID)
					continue
				}
				s.connMu.RLock()
				pr, hasPending := s.pending[result.CommandID]
				s.connMu.RUnlock()
				if hasPending {
					select {
					case pr.ch <- result:
					default:
						logger.SafeLogf("[WORKER] Dropped duplicate/late result for command %s from %s", result.CommandID, workerID)
					}
				} else {
					logger.SafeLogf("[WORKER] Received result for unknown command %s from %s", result.CommandID, workerID)
				}
				_ = s.SendAck(workerID, result.MessageID)
			}

		case protocol.MsgGetMetricsResult:
			payloadBytes, _ := json.Marshal(env.Payload)
			var result protocol.GetMetricsResult
			if jsonErr := json.Unmarshal(payloadBytes, &result); jsonErr == nil {
				s.connMu.RLock()
				pr, hasPending := s.pending[result.CommandID]
				s.connMu.RUnlock()
				if hasPending {
					select {
					case pr.ch <- protocol.CommandResult{CommandID: result.CommandID, Output: result.Metrics}:
					default:
					}
				}
			}

		default:
			logger.SafeLogf("[WORKER] Unknown message type '%s' from %s", env.Type, workerID)
		}
	}
}

// Start launches the worker HTTP(S) server on the given address.
// When tlsCfg is non-nil the server uses TLS (HTTPS/WSS); otherwise it
// falls back to plain HTTP/WS for backwards compatibility.
func (s *WorkerServer) Start(addr string, tlsCfg *tls.Config) error {
	server := &http.Server{
		Addr:      addr,
		Handler:   s.engine,
		TLSConfig: tlsCfg,
	}

	if tlsCfg != nil {
		logger.SafeLogf("[WORKER] Starting worker server with TLS on %s", addr)
		// Certificate is already embedded in tlsCfg.Certificates — pass empty
		// paths so the standard library reads from the config directly.
		return server.ListenAndServeTLS("", "")
	}

	logger.SafeLogf("[WORKER] Starting worker server on %s (plain HTTP)", addr)
	return server.ListenAndServe()
}
