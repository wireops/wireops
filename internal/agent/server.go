package agent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/pocketbase/pocketbase/core"
	"github.com/wireops/wireops/internal/pki"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/pkg/logger"
)

// pendingResult holds the channel to send a CommandResult back to the waiting caller.
type pendingResult struct {
	ch chan protocol.CommandResult
}

// MTLSServer handles mTLS connections from remote agents.
type MTLSServer struct {
	app      core.App
	pkiSvc   *pki.Service
	agentSvc *Service
	engine   *gin.Engine
	upgrader websocket.Upgrader

	// connMu protects connections, connWriteMu, pending, and agentTags maps.
	connMu      sync.RWMutex
	connections map[string]*websocket.Conn // agentID → conn
	connWriteMu map[string]*sync.Mutex     // agentID → write mutex
	pending     map[string]*pendingResult  // commandID → pending
	agentTags   map[string][]string        // agentID → tags declared via WIREOPS_AGENT_TAGS

	onConnect      func(agentID string)
	onJobCompleted func(protocol.JobCompletedMessage)
}

// SetOnConnect allows registering a callback to be notified when an agent successfully connects.
func (s *MTLSServer) SetOnConnect(f func(agentID string)) {
	s.onConnect = f
}

// SetOnJobCompleted registers a callback invoked whenever a remote agent reports
// that a job container has exited. The callback is called in a new goroutine.
func (s *MTLSServer) SetOnJobCompleted(f func(protocol.JobCompletedMessage)) {
	s.onJobCompleted = f
}

// SetAgentTags stores the tags reported by the agent at registration time.
// For the embedded agent this is called directly from the server bootstrap.
func (s *MTLSServer) SetAgentTags(agentID string, tags []string) {
	s.connMu.Lock()
	s.agentTags[agentID] = tags
	s.connMu.Unlock()
}

// GetAgentTags returns the tags currently associated with the given agent.
// Returns an empty slice if the agent has no tags or is not registered.
func (s *MTLSServer) GetAgentTags(agentID string) []string {
	s.connMu.RLock()
	tags, ok := s.agentTags[agentID]
	s.connMu.RUnlock()
	if !ok {
		return []string{}
	}
	return tags
}

// ClearAgentTags removes the in-memory tags for the given agent.
// Called on disconnect and revocation so stale tags are not served.
func (s *MTLSServer) ClearAgentTags(agentID string) {
	s.connMu.Lock()
	delete(s.agentTags, agentID)
	s.connMu.Unlock()
}

// GetAgentsByTags returns the IDs of all agents that are currently connected
// and whose tag set is a superset of the required tags. Empty required tags
// returns nil so callers can distinguish "no filter" from "no match".
func (s *MTLSServer) GetAgentsByTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}

	// Copy the tag snapshot under the read lock to minimise lock duration.
	s.connMu.RLock()
	snapshot := make(map[string][]string, len(s.agentTags))
	for id, t := range s.agentTags {
		snapshot[id] = t
	}
	s.connMu.RUnlock()

	var result []string
	for agentID, agentTagList := range snapshot {
		if !s.IsConnected(agentID) {
			continue
		}
		if agentHasAllTags(agentTagList, tags) {
			result = append(result, agentID)
		}
	}
	return result
}

// agentHasAllTags reports whether agentTags contains every tag in required.
func agentHasAllTags(agentTags, required []string) bool {
	set := make(map[string]struct{}, len(agentTags))
	for _, t := range agentTags {
		set[t] = struct{}{}
	}
	for _, t := range required {
		if _, ok := set[t]; !ok {
			return false
		}
	}
	return true
}

func NewMTLSServer(app core.App, pkiSvc *pki.Service, agentSvc *Service) *MTLSServer {
	r := gin.Default()
	s := &MTLSServer{
		app:      app,
		pkiSvc:   pkiSvc,
		agentSvc: agentSvc,
		engine:   r,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // mTLS already enforces security
			},
		},
		connections: make(map[string]*websocket.Conn),
		connWriteMu: make(map[string]*sync.Mutex),
		pending:     make(map[string]*pendingResult),
		agentTags:   make(map[string][]string),
	}

	s.registerRoutes()
	return s
}

func (s *MTLSServer) registerRoutes() {
	s.engine.POST("/agent/register", s.handleRegister)
	s.engine.GET("/agent/ws", s.handleWebSocket)
}

// IsEmbedded reports whether the given agentID corresponds to the embedded agent.
// This satisfies the AgentDispatcher interface used by the Reconciler.
func (s *MTLSServer) IsEmbedded(agentID string) bool {
	if agentID == "" {
		return true
	}
	agent, err := s.app.FindRecordById("agents", agentID)
	if err != nil {
		logger.SafeLogf("[AGENT] IsEmbedded: failed to look up agent %s: %v", agentID, err)
		return false // fail-safe: not embedded on lookup failure
	}
	return agent != nil && agent.GetString("fingerprint") == "embedded"
}

// DisconnectAgent forcefully closes the active WebSocket connection for the given agentID, if any.
// This is used immediately after revoking an agent so the connection drops without waiting for a heartbeat timeout.
func (s *MTLSServer) DisconnectAgent(agentID string) {
	s.connMu.Lock()
	conn, ok := s.connections[agentID]
	writeMu := s.connWriteMu[agentID]
	if ok {
		if writeMu != nil {
			writeMu.Lock()
		}
		// Send a close frame so the agent knows why it is being disconnected.
		_ = conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "agent revoked"),
		)
		if writeMu != nil {
			writeMu.Unlock()
		}
		conn.Close()
		delete(s.connections, agentID)
		delete(s.connWriteMu, agentID)
	}
	delete(s.agentTags, agentID)
	s.connMu.Unlock()
	if ok {
		logger.SafeLogf("[AGENT] Forcefully disconnected revoked agent: %s", agentID)
	}
}

// IsConnected reports whether the agent currently has an active mTLS WebSocket connection.
// Embedded agents are always considered connected since they run in-process.
func (s *MTLSServer) IsConnected(agentID string) bool {
	if s.IsEmbedded(agentID) {
		return true
	}
	s.connMu.RLock()
	_, ok := s.connections[agentID]
	s.connMu.RUnlock()
	return ok
}

// Dispatch sends a deploy or redeploy command to the connected remote agent and
// blocks until it receives the result (with a 5 minute timeout).
func (s *MTLSServer) Dispatch(ctx context.Context, agentID string, cmd interface{}) (protocol.CommandResult, error) {
	s.connMu.RLock()
	conn, ok := s.connections[agentID]
	writeMu := s.connWriteMu[agentID]
	s.connMu.RUnlock()

	if !ok {
		return protocol.CommandResult{}, fmt.Errorf("agent %s is not connected", agentID)
	}

	// Determine command type and command ID
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
	case protocol.GetResourcesCommand:
		msgType = protocol.MsgGetResources
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
	default:
		return protocol.CommandResult{}, fmt.Errorf("unknown command type %T", cmd)
	}

	// Register a pending slot so the read loop can deliver the result.
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
		return protocol.CommandResult{}, fmt.Errorf("failed to send command to agent %s: %w", agentID, err)
	}

	select {
	case result := <-pr.ch:
		return result, nil
	case <-ctx.Done():
		return protocol.CommandResult{}, ctx.Err()
	case <-time.After(5 * time.Minute):
		return protocol.CommandResult{}, fmt.Errorf("timed out waiting for agent %s response (command %s)", agentID, commandID)
	}
}

// getAgentID extracts the common name from the verified client certificate.
func getAgentID(c *gin.Context) (string, *x509.Certificate, error) {
	if c.Request.TLS == nil || len(c.Request.TLS.PeerCertificates) == 0 {
		return "", nil, fmt.Errorf("missing client certificate")
	}
	cert := c.Request.TLS.PeerCertificates[0]
	return cert.Subject.CommonName, cert, nil
}

func (s *MTLSServer) handleRegister(c *gin.Context) {
	agentID, clientCert, err := getAgentID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

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

	record, err := s.app.FindRecordById("agents", agentID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unknown agent ID"})
		return
	}

	if record.GetString("status") == "REVOKED" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Agent is revoked"})
		return
	}

	certHash := fmt.Sprintf("%x", sha256.Sum256(clientCert.Raw))
	record.Set("hostname", req.Hostname)
	record.Set("fingerprint", certHash)

	if err := s.app.Save(record); err != nil {
		logger.SafeLogf("[AGENT] Failed to update agent registration %s: %v", agentID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	s.agentSvc.UpdateLastSeen(agentID)
	s.SetAgentTags(agentID, req.Tags)
	logger.SafeLogf("[AGENT] Initial registration completed for Agent: %s (%s) tags=%v", req.Hostname, agentID, req.Tags)

	c.JSON(http.StatusOK, gin.H{"status": "registered"})
}

func (s *MTLSServer) handleWebSocket(c *gin.Context) {
	agentID, _, err := getAgentID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	record, err := s.app.FindRecordById("agents", agentID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unknown agent ID"})
		return
	}

	if record.GetString("status") == "REVOKED" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Agent is revoked"})
		return
	}

	s.agentSvc.UpdateLastSeen(agentID)
	logger.SafeLogf("[AGENT] Agent connected via WebSocket: %s", agentID)

	conn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.SafeLogf("[AGENT] Failed to upgrade websocket for %s: %v", agentID, err)
		return
	}
	defer func() {
		s.connMu.Lock()
		if s.connections[agentID] == conn {
			conn.Close()
			delete(s.connections, agentID)
			delete(s.connWriteMu, agentID)
			delete(s.agentTags, agentID)
			s.connMu.Unlock()
			logger.SafeLogf("[AGENT] Agent %s disconnected", agentID)
		} else {
			s.connMu.Unlock()
			conn.Close() // Always close the local connection to avoid FD leaks
			logger.SafeLogf("[AGENT] Ignoring stale connection cleanup for agent %s", agentID)
		}
	}()

	// Register connection
	s.connMu.Lock()
	if oldConn, exists := s.connections[agentID]; exists && oldConn != conn {
		oldConn.Close()
	}
	s.connections[agentID] = conn
	s.connWriteMu[agentID] = &sync.Mutex{}
	s.connMu.Unlock()

	// Initial online event
	_ = s.agentSvc.RecordHealthEvent(agentID, "online")

	if s.onConnect != nil {
		go s.onConnect(agentID)
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

	// Set initial read deadline
	_ = conn.SetReadDeadline(time.Now().Add(timeoutDuration))

	// Read loop: handle heartbeats and command results from agent
	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			logger.SafeLogf("[AGENT] Agent %s disconnected: %v", agentID, err)
			_ = s.agentSvc.RecordHealthEvent(agentID, "offline")
			break
		}

		// Reset deadline on success
		_ = conn.SetReadDeadline(time.Now().Add(timeoutDuration))

		s.agentSvc.UpdateLastSeen(agentID)

		if messageType != websocket.TextMessage {
			continue
		}

		var env protocol.Envelope
		if jsonErr := json.Unmarshal(p, &env); jsonErr != nil {
			logger.SafeLogf("[AGENT] Failed to parse message from %s: %v", agentID, jsonErr)
			continue
		}

		switch env.Type {
		case protocol.MsgHeartbeat:
			_ = s.agentSvc.RecordHealthEvent(agentID, "online")
			// The heartbeat payload optionally carries the list of job_run IDs
			// whose containers are still running. Log at trace level for now;
			// the scheduler uses reconnect events rather than per-heartbeat checks.
			payloadBytes, _ := json.Marshal(env.Payload)
			var hb protocol.HeartbeatPayload
			if jsonErr := json.Unmarshal(payloadBytes, &hb); jsonErr == nil && len(hb.ActiveJobRunIDs) > 0 {
				logger.SafeLogf("[AGENT] %s heartbeat: %d active job(s) %v", agentID, len(hb.ActiveJobRunIDs), hb.ActiveJobRunIDs)
			}

		case protocol.MsgJobCompleted:
			payloadBytes, _ := json.Marshal(env.Payload)
			var msg protocol.JobCompletedMessage
			if jsonErr := json.Unmarshal(payloadBytes, &msg); jsonErr == nil {
				logger.SafeLogf("[AGENT] job_completed from %s run=%s success=%v elapsed=%dms", agentID, msg.JobRunID, msg.Success, msg.DurationMs)
				if s.onJobCompleted != nil {
					go s.onJobCompleted(msg)
				}
			} else {
				logger.SafeLogf("[AGENT] Failed to parse job_completed from %s: %v", agentID, jsonErr)
			}

		case protocol.MsgResult:
			// Deliver result to waiting Dispatch() call
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
						logger.SafeLogf("[AGENT] Dropped duplicate/late result for command %s from %s", result.CommandID, agentID)
					}
				} else {
					logger.SafeLogf("[AGENT] Received result for unknown command %s from %s", result.CommandID, agentID)
				}
			}

		default:
			logger.SafeLogf("[AGENT] Unknown message type '%s' from %s", env.Type, agentID)
		}
	}
}

func (s *MTLSServer) Start(addr string) error {
	caCertPEM, err := s.pkiSvc.GetCACertPEM()
	if err != nil {
		return fmt.Errorf("failed to get CA cert: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCertPEM) {
		return fmt.Errorf("failed to parse CA certs from PEM (length: %d)", len(caCertPEM))
	}

	tlsConfig := &tls.Config{
		ClientCAs:  caCertPool,
		ClientAuth: tls.RequireAndVerifyClientCert,
		MinVersion: tls.VersionTLS13,
	}

	serverCert, err := s.pkiSvc.GetServerTLSCert()
	if err != nil {
		return fmt.Errorf("failed to get server TLS cert: %w", err)
	}

	tlsConfig.Certificates = []tls.Certificate{serverCert}

	server := &http.Server{
		Addr:      addr,
		Handler:   s.engine,
		TLSConfig: tlsConfig,
	}

	logger.SafeLogf("[AGENT] Starting mTLS server on %s", addr)
	return server.ListenAndServeTLS("", "")
}

