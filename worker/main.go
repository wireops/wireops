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
)

var activeJobs gosync.Map
var connWriteMu gosync.Mutex

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
	serverURL := os.Getenv("WIREOPS_SERVER")
	workerToken := os.Getenv("WIREOPS_WORKER_TOKEN")
	hostname := os.Getenv("HOSTNAME")

	if serverURL == "" {
		log.Fatal("WIREOPS_SERVER must be set")
	}
	if workerToken == "" {
		log.Fatal("WIREOPS_WORKER_TOKEN must be set")
	}
	if hostname == "" {
		h, err := os.Hostname()
		if err == nil {
			hostname = h
		} else {
			hostname = "unknown-worker"
		}
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	tags := parseTags(os.Getenv("WIREOPS_WORKER_TAGS"))
	backoff := initialBackoff

	for {
		reason := runSession(serverURL, workerToken, hostname, tags, sigChan)

		switch reason {
		case reasonRevoked:
			log.Fatal("[WORKER] This worker token has been rejected by the server. Issue a new token to continue.")

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

		log.Printf("[WORKER] Registration attempt %d failed: %v. Retrying in 5s...", i, err)
		if i == 5 {
			log.Printf("[WORKER] Failed to register after 5 attempts")
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
		log.Printf("[WORKER] Failed to connect WebSocket: %v", err)
		return reasonUnknown
	}
	defer conn.Close()

	log.Println("[WORKER] Worker is running and connected.")

	disconnectCh := make(chan disconnectReason, 1)

	go readLoop(conn, disconnectCh)

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
		}
	}
}

func readLoop(conn *websocket.Conn, disconnectCh chan<- disconnectReason) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if closeErr, ok := err.(*websocket.CloseError); ok && closeErr.Code == websocket.ClosePolicyViolation {
				disconnectCh <- reasonRevoked
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

		default:
			log.Printf("[WORKER] Unknown message type: %s", env.Type)
		}
	}
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
