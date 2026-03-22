package worker

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

// MTLSServer handles mTLS connections from remote workers.
type MTLSServer struct {
	app       core.App
	pkiSvc    *pki.Service
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

// SetOnConnect allows registering a callback to be notified when a worker successfully connects.
func (s *MTLSServer) SetOnConnect(f func(workerID string)) {
	s.onConnect = f
}

// SetOnJobCompleted registers a callback invoked whenever a remote worker reports
// that a job container has exited. The callback is called in a new goroutine.
func (s *MTLSServer) SetOnJobCompleted(f func(protocol.JobCompletedMessage)) {
	s.onJobCompleted = f
}

// SetWorkerTags stores the tags reported by the worker at registration time.
// For the embedded worker this is called directly from the server bootstrap.
func (s *MTLSServer) SetWorkerTags(workerID string, tags []string) {
	s.connMu.Lock()
	s.workerTags[workerID] = tags
	s.connMu.Unlock()
}

// GetWorkerTags returns the tags currently associated with the given worker.
// Returns an empty slice if the worker has no tags or is not registered.
func (s *MTLSServer) GetWorkerTags(workerID string) []string {
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
func (s *MTLSServer) ClearWorkerTags(workerID string) {
	s.connMu.Lock()
	delete(s.workerTags, workerID)
	s.connMu.Unlock()
}

// GetWorkersByTags returns the IDs of all workers that are currently connected
// and whose tag set is a superset of the required tags. Empty required tags
// returns nil so callers can distinguish "no filter" from "no match".
func (s *MTLSServer) GetWorkersByTags(tags []string) []string {
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

func NewMTLSServer(app core.App, pkiSvc *pki.Service, workerSvc *Service) *MTLSServer {
	r := gin.Default()
	s := &MTLSServer{
		app:       app,
		pkiSvc:    pkiSvc,
		workerSvc: workerSvc,
		engine:    r,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // mTLS already enforces security
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

func (s *MTLSServer) registerRoutes() {
	s.engine.POST("/worker/register", s.handleRegister)
	s.engine.POST("/worker/renew", s.handleRenew)
	s.engine.GET("/worker/ws", s.handleWebSocket)
}

// IsEmbedded reports whether the given workerID corresponds to the embedded worker.
func (s *MTLSServer) IsEmbedded(workerID string) bool {
	if workerID == "" {
		return true
	}
	worker, err := s.app.FindRecordById("workers", workerID)
	if err != nil {
		logger.SafeLogf("[WORKER] IsEmbedded: failed to look up worker %s: %v", workerID, err)
		return false
	}
	return worker != nil && worker.GetString("fingerprint") == "embedded"
}

// DisconnectWorker forcefully closes the active WebSocket connection for the given workerID, if any.
// Used immediately after revoking a worker so the connection drops without waiting for a heartbeat timeout.
func (s *MTLSServer) DisconnectWorker(workerID string) {
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

// IsConnected reports whether the worker currently has an active mTLS WebSocket connection.
// Embedded workers are always considered connected since they run in-process.
func (s *MTLSServer) IsConnected(workerID string) bool {
	if s.IsEmbedded(workerID) {
		return true
	}
	s.connMu.RLock()
	_, ok := s.connections[workerID]
	s.connMu.RUnlock()
	return ok
}

// SendMessage sends a one-way message to a connected worker (no response expected).
// Used for control messages like MsgRequestRenewal and MsgForceRebootstrap.
func (s *MTLSServer) SendMessage(workerID string, msgType protocol.MessageType, payload interface{}) error {
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
func (s *MTLSServer) Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error) {
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

// getWorkerID extracts the common name from the verified client certificate.
func getWorkerID(c *gin.Context) (string, *x509.Certificate, error) {
	if c.Request.TLS == nil || len(c.Request.TLS.PeerCertificates) == 0 {
		return "", nil, fmt.Errorf("missing client certificate")
	}
	cert := c.Request.TLS.PeerCertificates[0]
	return cert.Subject.CommonName, cert, nil
}

func (s *MTLSServer) handleRegister(c *gin.Context) {
	workerID, clientCert, err := getWorkerID(c)
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

	record, err := s.app.FindRecordById("workers", workerID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unknown worker ID"})
		return
	}

	if record.GetString("status") == "REVOKED" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Worker is revoked"})
		return
	}

	certHash := fmt.Sprintf("%x", sha256.Sum256(clientCert.Raw))
	record.Set("hostname", req.Hostname)
	record.Set("fingerprint", certHash)

	if err := s.app.Save(record); err != nil {
		logger.SafeLogf("[WORKER] Failed to update worker registration %s: %v", workerID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	s.workerSvc.UpdateLastSeen(workerID)
	s.SetWorkerTags(workerID, req.Tags)
	logger.SafeLogf("[WORKER] Initial registration completed for Worker: %s (%s) tags=%v", req.Hostname, workerID, req.Tags)

	c.JSON(http.StatusOK, gin.H{"status": "registered"})
}

func (s *MTLSServer) handleRenew(c *gin.Context) {
	workerID, _, err := getWorkerID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	record, err := s.app.FindRecordById("workers", workerID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unknown worker ID"})
		return
	}

	if record.GetString("status") == "REVOKED" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Worker is revoked"})
		return
	}

	var req struct {
		CSR string `json:"csr"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON body"})
		return
	}

	signed, err := s.pkiSvc.SignCSR([]byte(req.CSR), workerID)
	if err != nil {
		logger.SafeLogf("[WORKER] Failed to sign renewal CSR for worker %s: %v", workerID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to sign CSR"})
		return
	}

	record.Set("cert_not_after", signed.NotAfter)
	record.Set("cert_serial", signed.Serial)
	if saveErr := s.app.Save(record); saveErr != nil {
		logger.SafeLogf("[WORKER] Failed to save cert metadata for worker %s: %v", workerID, saveErr)
	}

	caCertPEM, err := s.pkiSvc.GetCACertPEM()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve CA cert"})
		return
	}

	logger.SafeLogf("[WORKER] Certificate renewed for worker %s (serial: %s, expires: %s)", workerID, signed.Serial, signed.NotAfter.Format(time.RFC3339))
	c.JSON(http.StatusOK, gin.H{
		"worker_cert": string(signed.CertPEM),
		"ca_cert":     string(caCertPEM),
	})
}

func (s *MTLSServer) handleWebSocket(c *gin.Context) {
	workerID, _, err := getWorkerID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	record, err := s.app.FindRecordById("workers", workerID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unknown worker ID"})
		return
	}

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
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			cert, err := s.pkiSvc.GetServerTLSCert()
			if err != nil {
				return nil, err
			}
			return &cert, nil
		},
	}

	server := &http.Server{
		Addr:      addr,
		Handler:   s.engine,
		TLSConfig: tlsConfig,
	}

	logger.SafeLogf("[WORKER] Starting mTLS server on %s", addr)
	return server.ListenAndServeTLS("", "")
}
