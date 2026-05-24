package worker

import (
	"context"
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

// pendingResult holds the channel to send a CommandResult back to the waiting caller.
type pendingResult struct {
	ch chan protocol.CommandResult
}

// WorkerServer handles authenticated connections from remote workers.
type WorkerServer struct {
	app       core.App
	workerSvc *Service
	engine    *gin.Engine
	upgrader  websocket.Upgrader

	// connMu protects connections, connWriteMu, pending, and workerTags maps.
	connMu      sync.RWMutex
	connections map[string]*websocket.Conn // workerID → conn
	connWriteMu map[string]*sync.Mutex     // workerID → write mutex
	pending     map[string]*pendingResult  // commandID → pending
	workerTags  map[string][]string        // workerID → tags declared via WIREOPS_WORKER_TAGS

	onConnect      func(workerID string)
	onJobCompleted func(protocol.JobCompletedMessage)
}

// MTLSServer is kept as an internal alias while the codebase finishes moving
// away from the old naming.
type MTLSServer = WorkerServer

// SetOnConnect allows registering a callback to be notified when a worker successfully connects.
func (s *WorkerServer) SetOnConnect(f func(workerID string)) {
	s.onConnect = f
}

// SetOnJobCompleted registers a callback invoked whenever a remote worker reports
// that a job container has exited. The callback is called in a new goroutine.
func (s *WorkerServer) SetOnJobCompleted(f func(protocol.JobCompletedMessage)) {
	s.onJobCompleted = f
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
	r := gin.Default()
	s := &WorkerServer{
		app:       app,
		workerSvc: workerSvc,
		engine:    r,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		connections: make(map[string]*websocket.Conn),
		connWriteMu: make(map[string]*sync.Mutex),
		pending:     make(map[string]*pendingResult),
		workerTags:  make(map[string][]string),
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
	err = conn.WriteMessage(websocket.TextMessage, msg)
	writeMu.Unlock()
	return err
}

// Dispatch sends a command to the connected remote worker and
// blocks until it receives the result (with a 5 minute timeout).
func (s *WorkerServer) Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error) {
	s.connMu.RLock()
	conn, ok := s.connections[workerID]
	writeMu := s.connWriteMu[workerID]
	s.connMu.RUnlock()

	if !ok {
		return protocol.CommandResult{}, fmt.Errorf("worker %s is not connected", workerID)
	}

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
		} else {
			msgType = protocol.MsgRestartContainer
		}
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
	default:
		return protocol.CommandResult{}, fmt.Errorf("unknown command type %T", cmd)
	}

	pr := &pendingResult{ch: make(chan protocol.CommandResult, 1)}
	s.connMu.Lock()
	s.pending[commandID] = pr
	s.connMu.Unlock()
	defer func() {
		s.connMu.Lock()
		delete(s.pending, commandID)
		s.connMu.Unlock()
	}()

	msg, err := json.Marshal(protocol.Envelope{Type: msgType, Payload: cmd})
	if err != nil {
		return protocol.CommandResult{}, fmt.Errorf("failed to marshal command: %w", err)
	}

	writeMu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, msg)
	writeMu.Unlock()
	if err != nil {
		return protocol.CommandResult{}, fmt.Errorf("failed to send command to worker %s: %w", workerID, err)
	}

	select {
	case result := <-pr.ch:
		return result, nil
	case <-ctx.Done():
		return protocol.CommandResult{}, ctx.Err()
	case <-time.After(5 * time.Minute):
		return protocol.CommandResult{}, fmt.Errorf("timed out waiting for worker %s response (command %s)", workerID, commandID)
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

	s.workerSvc.UpdateLastSeen(record.Id)
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

	s.workerSvc.UpdateLastSeen(workerID)
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

	_ = s.workerSvc.RecordHealthEvent(workerID, "online")

	if s.onConnect != nil {
		go s.onConnect(workerID)
	}

	intervalStr := os.Getenv("WIREOPS_HEARTBEAT_INTERVAL")
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
			_ = s.workerSvc.RecordHealthEvent(workerID, "offline")
			break
		}

		_ = conn.SetReadDeadline(time.Now().Add(timeoutDuration))

		s.workerSvc.UpdateLastSeen(workerID)

		if messageType != websocket.TextMessage {
			continue
		}

		var env protocol.Envelope
		if jsonErr := json.Unmarshal(p, &env); jsonErr != nil {
			logger.SafeLogf("[WORKER] Failed to parse message from %s: %v", workerID, jsonErr)
			continue
		}

		switch env.Type {
		case protocol.MsgHeartbeat:
			_ = s.workerSvc.RecordHealthEvent(workerID, "online")
			payloadBytes, _ := json.Marshal(env.Payload)
			var hb protocol.HeartbeatPayload
			if jsonErr := json.Unmarshal(payloadBytes, &hb); jsonErr == nil && len(hb.ActiveJobRunIDs) > 0 {
				logger.SafeLogf("[WORKER] %s heartbeat: %d active job(s) %v", workerID, len(hb.ActiveJobRunIDs), hb.ActiveJobRunIDs)
			}

		case protocol.MsgJobCompleted:
			payloadBytes, _ := json.Marshal(env.Payload)
			var msg protocol.JobCompletedMessage
			if jsonErr := json.Unmarshal(payloadBytes, &msg); jsonErr == nil {
				logger.SafeLogf("[WORKER] job_completed from %s run=%s success=%v elapsed=%dms", workerID, msg.JobRunID, msg.Success, msg.DurationMs)
				if s.onJobCompleted != nil {
					go s.onJobCompleted(msg)
				}
			} else {
				logger.SafeLogf("[WORKER] Failed to parse job_completed from %s: %v", workerID, jsonErr)
			}

		case protocol.MsgResult:
			payloadBytes, _ := json.Marshal(env.Payload)
			var result protocol.CommandResult
			if jsonErr := json.Unmarshal(payloadBytes, &result); jsonErr == nil {
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
			}

		default:
			logger.SafeLogf("[WORKER] Unknown message type '%s' from %s", env.Type, workerID)
		}
	}
}

func (s *WorkerServer) Start(addr string) error {
	server := &http.Server{
		Addr:    addr,
		Handler: s.engine,
	}

	logger.SafeLogf("[WORKER] Starting worker server on %s", addr)
	return server.ListenAndServe()
}
