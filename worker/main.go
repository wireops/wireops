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

	"net/http"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/worker/api"
	"github.com/wireops/wireops/worker/executor"
	"github.com/wireops/wireops/worker/pki"
	wsync "github.com/wireops/wireops/worker/sync"
)

var activeJobs gosync.Map
var connWriteMu gosync.Mutex

// renewInProgress is set to 1 while a cert renewal is closing the WebSocket.
// The readLoop checks this to suppress the expected "use of closed connection" error.
var renewInProgress atomic.Int32

const (
	maxBackoff     = 5 * time.Minute
	initialBackoff = 5 * time.Second
)

type disconnectReason int

const (
	reasonUnknown       disconnectReason = iota
	reasonRevoked                        // server revoked this worker
	reasonRebootstrap                    // admin requested re-bootstrap
	reasonCertRenewed                    // cert was swapped, reconnect immediately
	reasonShutdown                       // graceful shutdown
)

func main() {
	serverURL := os.Getenv("WIREOPS_SERVER")
	mtlsServerURL := os.Getenv("WIREOPS_MTLS_SERVER")
	bootstrapToken := os.Getenv("WIREOPS_BOOTSTRAP_TOKEN")
	workerPKIDir := os.Getenv("WIREOPS_WORKER_PKI_DIR")
	hostname := os.Getenv("HOSTNAME")

	renewalDays := 30
	if v := os.Getenv("WIREOPS_CERT_RENEWAL_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			renewalDays = n
		}
	}

	if serverURL == "" {
		log.Fatal("WIREOPS_SERVER must be set")
	}
	if mtlsServerURL == "" {
		mtlsServerURL = serverURL
	}
	if workerPKIDir == "" {
		workerPKIDir = "./worker_pki"
	}
	if hostname == "" {
		h, err := os.Hostname()
		if err == nil {
			hostname = h
		} else {
			hostname = "unknown-worker"
		}
	}

	if !pki.HasValidCerts(workerPKIDir) {
		if bootstrapToken == "" {
			log.Fatal("WIREOPS_BOOTSTRAP_TOKEN is required for initial setup but not provided")
		}
		if err := pki.Bootstrap(serverURL, bootstrapToken, workerPKIDir); err != nil {
			log.Fatalf("Fatal: bootstrap failed: %v", err)
		}
	} else {
		log.Println("[WORKER] Certificates found, skipping bootstrap.")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	tags := parseTags(os.Getenv("WIREOPS_WORKER_TAGS"))
	backoff := initialBackoff

	for {
		reason := runSession(mtlsServerURL, workerPKIDir, hostname, tags, renewalDays, sigChan)

		switch reason {
		case reasonRevoked:
			pki.PurgeCredentials(workerPKIDir)
			log.Fatal("[WORKER] This worker has been revoked by the server. " +
				"Bootstrap a new worker with a fresh seat token to continue.")

		case reasonRebootstrap:
			pki.PurgeCredentials(workerPKIDir)
			log.Fatal("[WORKER] Server requested re-bootstrap. " +
				"Restart the worker with a new WIREOPS_BOOTSTRAP_TOKEN to continue.")

		case reasonCertRenewed:
			log.Println("[WORKER] Certificate renewed. Reconnecting immediately...")
			backoff = initialBackoff
			continue

		case reasonShutdown:
			log.Println("[WORKER] Shutting down...")
			return

		default:
			log.Printf("[WORKER] Disconnected. Reconnecting in %v...", backoff)
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
		}
	}
}

// runSession handles one full connect-register-websocket cycle.
// Returns the reason the session ended so the caller can decide what to do.
func runSession(mtlsServerURL, workerPKIDir, hostname string, tags []string, renewalDays int, sigChan <-chan os.Signal) disconnectReason {
	renewInProgress.Store(0)

	client, err := api.NewMTLSClient(workerPKIDir)
	if err != nil {
		log.Printf("[WORKER] Failed to create mTLS client: %v", err)
		return reasonUnknown
	}

	for i := 1; i <= 5; i++ {
		err = api.Register(client, mtlsServerURL, hostname, "1.0.0", tags)
		if err == nil {
			break
		}
		if errors.Is(err, api.ErrRevoked) {
			return reasonRevoked
		}
		log.Printf("[WORKER] Registration attempt %d failed: %v. Retrying in 5s...", i, err)
		time.Sleep(5 * time.Second)
		if i == 5 {
			log.Printf("[WORKER] Failed to register after 5 attempts")
			return reasonUnknown
		}
	}

	conn, err := wsync.Connect(mtlsServerURL, workerPKIDir)
	if err != nil {
		log.Printf("[WORKER] Failed to connect WebSocket: %v", err)
		return reasonUnknown
	}
	defer conn.Close()

	log.Println("[WORKER] Worker is running and connected.")

	disconnectCh := make(chan disconnectReason, 1)

	go readLoop(conn, workerPKIDir, mtlsServerURL, client, renewalDays, disconnectCh)

	intervalStr := os.Getenv("WIREOPS_HEARTBEAT_INTERVAL")
	if intervalStr == "" {
		intervalStr = "30"
	}
	intervalSecs, parseErr := strconv.Atoi(intervalStr)
	if parseErr != nil || intervalSecs <= 0 {
		intervalSecs = 30
	}

	ticker := time.NewTicker(time.Duration(intervalSecs) * time.Second)
	defer ticker.Stop()

	renewalChecked := false

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
			connWriteMu.Lock()
			writeErr := conn.WriteMessage(websocket.TextMessage, heartbeat)
			connWriteMu.Unlock()
			if writeErr != nil {
				log.Printf("[WORKER] Heartbeat failed: %v", writeErr)
			}

			if !renewalChecked {
				renewalChecked = true
				go checkAndRenew(conn, workerPKIDir, mtlsServerURL, client, renewalDays, disconnectCh)
			}
		}
	}
}

func readLoop(conn *websocket.Conn, workerPKIDir, mtlsServerURL string, client *http.Client, renewalDays int, disconnectCh chan<- disconnectReason) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if closeErr, ok := err.(*websocket.CloseError); ok && closeErr.Code == websocket.ClosePolicyViolation {
				disconnectCh <- reasonRevoked
				return
			}
			if renewInProgress.Load() == 1 {
				return
			}
			log.Printf("[WORKER] WebSocket read error: %v", err)
			disconnectCh <- reasonUnknown
			return
		}

		var env protocol.Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			log.Printf("[WORKER] Failed to parse message: %v", err)
			continue
		}

		switch env.Type {
		case protocol.MsgDeploy:
			go handleDeploy(conn, env.Payload)
		case protocol.MsgRedeploy:
			go handleRedeploy(conn, env.Payload)
		case protocol.MsgTeardown:
			go handleTeardown(conn, env.Payload)
		case protocol.MsgProbe:
			go handleProbe(conn, env.Payload)
		case protocol.MsgInspect:
			go handleInspect(conn, env.Payload)
		case protocol.MsgGetStatus:
			go handleGetStatus(conn, env.Payload)
		case protocol.MsgGetResources:
			go handleGetResources(conn, env.Payload)
		case protocol.MsgDiscoverProjects:
			go handleDiscoverProjects(conn, env.Payload)
		case protocol.MsgReadFile:
			go handleReadFile(conn, env.Payload)
		case protocol.MsgRunJob:
			go handleRunJob(conn, env.Payload)
		case protocol.MsgKillJob:
			go handleKillJob(conn, env.Payload)

		case protocol.MsgRequestRenewal:
			log.Println("[WORKER] Server requested certificate renewal.")
			go performRenewal(conn, workerPKIDir, mtlsServerURL, client, disconnectCh)

		case protocol.MsgForceRebootstrap:
			log.Println("[WORKER] Server requested force re-bootstrap.")
			disconnectCh <- reasonRebootstrap

		default:
			log.Printf("[WORKER] Unknown message type: %s", env.Type)
		}
	}
}

func checkAndRenew(conn *websocket.Conn, workerPKIDir, mtlsServerURL string, client *http.Client, renewalDays int, disconnectCh chan<- disconnectReason) {
	if !pki.NeedsRenewal(workerPKIDir, renewalDays) {
		return
	}
	log.Printf("[WORKER] Certificate expires within %d days. Starting renewal...", renewalDays)
	performRenewal(conn, workerPKIDir, mtlsServerURL, client, disconnectCh)
}

func performRenewal(conn *websocket.Conn, workerPKIDir, mtlsServerURL string, client *http.Client, disconnectCh chan<- disconnectReason) {
	csrPEM, keyPEM, err := pki.GenerateCSR()
	if err != nil {
		log.Printf("[WORKER] Failed to generate CSR for renewal: %v", err)
		return
	}

	result, err := api.Renew(client, mtlsServerURL, csrPEM)
	if err != nil {
		if errors.Is(err, api.ErrRevoked) {
			disconnectCh <- reasonRevoked
			return
		}
		log.Printf("[WORKER] Renewal request failed: %v", err)
		return
	}

	if err := pki.WriteCertificates(workerPKIDir, []byte(result.WorkerCert), keyPEM, []byte(result.CACert)); err != nil {
		log.Printf("[WORKER] Failed to write renewed certificates: %v", err)
		return
	}

	log.Println("[WORKER] Certificate files updated. Closing connection to reconnect with new credentials.")

	renewInProgress.Store(1)

	connWriteMu.Lock()
	_ = conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "certificate renewed"),
	)
	connWriteMu.Unlock()

	disconnectCh <- reasonCertRenewed
}

func handleDeploy(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.DeployCommand](payload)
	if err != nil {
		log.Printf("[WORKER] Invalid deploy payload: %v", err)
		return
	}
	trigger := cmd.Trigger
	if trigger == "" {
		trigger = "unknown"
	}
	log.Printf("[WORKER] deploy (trigger: %s) stack: %s, commit: %s (command: %s)", trigger, cmd.StackID, cmd.CommitSHA, cmd.CommandID)
	result := executor.Deploy(context.Background(), cmd)
	sendResult(conn, result)
}

func handleRedeploy(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.RedeployCommand](payload)
	if err != nil {
		log.Printf("[WORKER] Invalid redeploy payload: %v", err)
		return
	}
	trigger := cmd.Trigger
	if trigger == "" {
		trigger = "force-redeploy"
	}
	log.Printf("[WORKER] redeploy (trigger: %s) stack: %s, commit: %s (command: %s)", trigger, cmd.StackID, cmd.CommitSHA, cmd.CommandID)
	result := executor.Redeploy(context.Background(), cmd)
	sendResult(conn, result)
}

func handleTeardown(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.TeardownCommand](payload)
	if err != nil {
		log.Printf("[WORKER] Invalid teardown payload: %v", err)
		return
	}
	log.Printf("[WORKER] teardown stack: %s (command: %s)", cmd.StackID, cmd.CommandID)
	result := executor.Teardown(context.Background(), cmd)
	sendResult(conn, result)
}

func handleProbe(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.ProbeCommand](payload)
	if err != nil {
		log.Printf("[WORKER] Invalid probe payload: %v", err)
		return
	}
	log.Printf("[WORKER] probe stack: %s (command: %s)", cmd.StackID, cmd.CommandID)
	result := executor.Probe(context.Background(), cmd)
	sendResult(conn, result)
}

func handleInspect(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.InspectCommand](payload)
	if err != nil {
		log.Printf("[WORKER] Invalid inspect payload: %v", err)
		return
	}
	log.Printf("[WORKER] inspect stack: %s (command: %s)", cmd.StackID, cmd.CommandID)
	result := executor.Inspect(context.Background(), cmd)
	sendResult(conn, result)
}

func handleGetResources(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.GetResourcesCommand](payload)
	if err != nil {
		log.Printf("[WORKER] Invalid get_resources payload: %v", err)
		return
	}
	log.Printf("[WORKER] get_resources stack: %s project: %s (command: %s)", cmd.StackID, cmd.ProjectName, cmd.CommandID)
	result := executor.GetResources(context.Background(), cmd)
	sendResult(conn, result)
}

func handleGetStatus(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.GetStatusCommand](payload)
	if err != nil {
		log.Printf("[WORKER] Invalid get_status payload: %v", err)
		return
	}
	log.Printf("[WORKER] get_status project: %s (command: %s)", cmd.ProjectName, cmd.CommandID)
	result := executor.GetStatus(context.Background(), cmd)
	sendResult(conn, result)
}

func handleDiscoverProjects(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.DiscoverProjectsCommand](payload)
	if err != nil {
		log.Printf("[WORKER] Invalid discover_projects payload: %v", err)
		return
	}
	log.Printf("[WORKER] discover_projects (command: %s)", cmd.CommandID)
	result := executor.DiscoverProjects(context.Background(), cmd)
	sendResult(conn, result)
}

func handleReadFile(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.ReadFileCommand](payload)
	if err != nil {
		log.Printf("[WORKER] Invalid read_file payload: %v", err)
		return
	}
	log.Printf("[WORKER] read_file path=%s (command: %s)", cmd.Path, cmd.CommandID)
	result := executor.ReadFile(context.Background(), cmd)
	sendResult(conn, result)
}

func handleRunJob(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.RunJobCommand](payload)
	if err != nil {
		log.Printf("[WORKER] Invalid run_job payload: %v", err)
		return
	}
	log.Printf("[WORKER] run_job job_run=%s image=%s (command: %s)", cmd.JobRunID, cmd.Image, cmd.CommandID)

	activeJobs.Store(cmd.JobRunID, struct{}{})

	result := executor.RunJob(cmd, func(msgType protocol.MessageType, p interface{}) {
		activeJobs.Delete(cmd.JobRunID)
		msg, marshalErr := json.Marshal(protocol.Envelope{Type: msgType, Payload: p})
		if marshalErr != nil {
			log.Printf("[WORKER] Failed to marshal job completion: %v", marshalErr)
			return
		}
		connWriteMu.Lock()
		writeErr := conn.WriteMessage(websocket.TextMessage, msg)
		connWriteMu.Unlock()
		if writeErr != nil {
			log.Printf("[WORKER] Failed to send job completion for run %s: %v", cmd.JobRunID, writeErr)
		}
	})

	sendResult(conn, result)
}

func handleKillJob(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.KillJobCommand](payload)
	if err != nil {
		log.Printf("[WORKER] Invalid kill_job payload: %v", err)
		return
	}
	log.Printf("[WORKER] kill_job job_run=%s (command: %s)", cmd.JobRunID, cmd.CommandID)
	result := executor.KillJob(cmd)
	sendResult(conn, result)
}

func sendResult(conn *websocket.Conn, result protocol.CommandResult) {
	msg, err := json.Marshal(protocol.Envelope{Type: protocol.MsgResult, Payload: result})
	if err != nil {
		log.Printf("[WORKER] Failed to marshal result: %v", err)
		return
	}
	connWriteMu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, msg)
	connWriteMu.Unlock()
	if err != nil {
		log.Printf("[WORKER] Failed to send result: %v", err)
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
