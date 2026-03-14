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
	"github.com/wireops/wireops/agent/api"
	"github.com/wireops/wireops/agent/executor"
	"github.com/wireops/wireops/agent/pki"
	"github.com/wireops/wireops/agent/sync"
	"github.com/wireops/wireops/internal/protocol"
)

// activeJobs tracks job_run IDs whose containers are still running on this agent.
// Reported in each heartbeat so the server has a liveness view.
var activeJobs gosync.Map
var connWriteMu gosync.Mutex

func main() {
	serverURL := os.Getenv("WIREOPS_SERVER")
	mtlsServerURL := os.Getenv("WIREOPS_MTLS_SERVER")
	bootstrapToken := os.Getenv("WIREOPS_BOOTSTRAP_TOKEN")
	agentPKIDir := os.Getenv("WIREOPS_AGENT_PKI_DIR")
	hostname := os.Getenv("HOSTNAME")

	if serverURL == "" {
		log.Fatal("WIREOPS_SERVER must be set")
	}
	if mtlsServerURL == "" {
		mtlsServerURL = serverURL
	}
	if agentPKIDir == "" {
		agentPKIDir = "./agent_pki"
	}
	if hostname == "" {
		h, err := os.Hostname()
		if err == nil {
			hostname = h
		} else {
			hostname = "unknown-agent"
		}
	}

	// 1. Bootstrap PKI if we don't have certs
	if !pki.HasValidCerts(agentPKIDir) {
		if bootstrapToken == "" {
			log.Fatal("WIREOPS_BOOTSTRAP_TOKEN is required for initial setup but not provided")
		}
		if err := pki.Bootstrap(serverURL, bootstrapToken, agentPKIDir); err != nil {
			log.Fatalf("Fatal: bootstrap failed: %v", err)
		}
	} else {
		log.Println("[AGENT] Certificates found, skipping bootstrap.")
	}

	// 2. Create mTLS client and register
	client, err := api.NewMTLSClient(agentPKIDir)
	if err != nil {
		log.Fatalf("Fatal: could not initialize mTLS client: %v", err)
	}

	tags := parseTags(os.Getenv("WIREOPS_AGENT_TAGS"))

	for i := 1; i <= 5; i++ {
		err = api.Register(client, mtlsServerURL, hostname, "1.0.0", tags)
		if err == nil {
			break
		}
		if errors.Is(err, api.ErrRevoked) {
			pki.PurgeCredentials(agentPKIDir)
			log.Fatal("[AGENT] This agent has been revoked by the server. " +
				"Bootstrap a new agent with a fresh seat token to continue.")
		}
		log.Printf("[AGENT] Registration attempt %d failed: %v. Retrying in 5 seconds...", i, err)
		time.Sleep(5 * time.Second)
		if i == 5 {
			log.Fatalf("Fatal: failed to register agent after 5 attempts")
		}
	}

	// 3. Connect to Sync Loop WebSocket
	conn, err := sync.Connect(mtlsServerURL, agentPKIDir)
	if err != nil {
		log.Fatalf("Fatal: failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	log.Println("[AGENT] Agent is running and connected.")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Background reader: listen for commands from the server
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				// Check if the server explicitly closed the connection due to revocation.
				if closeErr, ok := err.(*websocket.CloseError); ok && closeErr.Code == websocket.ClosePolicyViolation {
					pki.PurgeCredentials(agentPKIDir)
					log.Fatal("[AGENT] This agent has been revoked by the server. " +
						"Bootstrap a new agent with a fresh seat token to continue.")
				}
				log.Printf("[AGENT] WebSocket connection closed by server: %v", err)
				os.Exit(1)
				return
			}

			var env protocol.Envelope
			if err := json.Unmarshal(msg, &env); err != nil {
				log.Printf("[AGENT] Failed to parse message: %v", err)
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
				log.Printf("[AGENT] Unknown message type: %s", env.Type)
			}
		}
	}()

	intervalStr := os.Getenv("WIREOPS_HEARTBEAT_INTERVAL")
	if intervalStr == "" {
		intervalStr = "30"
	}
	intervalSecs, err := strconv.Atoi(intervalStr)
	if err != nil || intervalSecs <= 0 {
		intervalSecs = 30
	}

	ticker := time.NewTicker(time.Duration(intervalSecs) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			log.Println("[AGENT] Shutting down...")
			return
		case <-ticker.C:
			// Collect IDs of containers that are still running so the server has
			// a liveness view without holding a WebSocket channel per job.
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
			err := conn.WriteMessage(websocket.TextMessage, heartbeat)
			connWriteMu.Unlock()
			if err != nil {
				log.Printf("[AGENT] Heartbeat failed: %v", err)
			}
		}
	}
}

func handleDeploy(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.DeployCommand](payload)
	if err != nil {
		log.Printf("[AGENT] Invalid deploy payload: %v", err)
		return
	}
	trigger := cmd.Trigger
	if trigger == "" {
		trigger = "unknown"
	}
	log.Printf("[AGENT] deploy (trigger: %s) stack: %s, commit: %s (command: %s)", trigger, cmd.StackID, cmd.CommitSHA, cmd.CommandID)
	result := executor.Deploy(context.Background(), cmd)
	sendResult(conn, result)
}

func handleRedeploy(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.RedeployCommand](payload)
	if err != nil {
		log.Printf("[AGENT] Invalid redeploy payload: %v", err)
		return
	}
	trigger := cmd.Trigger
	if trigger == "" {
		trigger = "force-redeploy"
	}
	log.Printf("[AGENT] redeploy (trigger: %s) stack: %s, commit: %s (command: %s)", trigger, cmd.StackID, cmd.CommitSHA, cmd.CommandID)
	result := executor.Redeploy(context.Background(), cmd)
	sendResult(conn, result)
}

func handleTeardown(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.TeardownCommand](payload)
	if err != nil {
		log.Printf("[AGENT] Invalid teardown payload: %v", err)
		return
	}
	log.Printf("[AGENT] teardown stack: %s (command: %s)", cmd.StackID, cmd.CommandID)
	result := executor.Teardown(context.Background(), cmd)
	sendResult(conn, result)
}

func handleProbe(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.ProbeCommand](payload)
	if err != nil {
		log.Printf("[AGENT] Invalid probe payload: %v", err)
		return
	}
	log.Printf("[AGENT] probe stack: %s (command: %s)", cmd.StackID, cmd.CommandID)
	result := executor.Probe(context.Background(), cmd)
	sendResult(conn, result)
}

func handleInspect(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.InspectCommand](payload)
	if err != nil {
		log.Printf("[AGENT] Invalid inspect payload: %v", err)
		return
	}
	log.Printf("[AGENT] inspect stack: %s (command: %s)", cmd.StackID, cmd.CommandID)
	result := executor.Inspect(context.Background(), cmd)
	sendResult(conn, result)
}

func handleGetResources(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.GetResourcesCommand](payload)
	if err != nil {
		log.Printf("[AGENT] Invalid get_resources payload: %v", err)
		return
	}
	log.Printf("[AGENT] get_resources stack: %s project: %s (command: %s)", cmd.StackID, cmd.ProjectName, cmd.CommandID)
	result := executor.GetResources(context.Background(), cmd)
	sendResult(conn, result)
}

func handleGetStatus(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.GetStatusCommand](payload)
	if err != nil {
		log.Printf("[AGENT] Invalid get_status payload: %v", err)
		return
	}
	log.Printf("[AGENT] get_status project: %s (command: %s)", cmd.ProjectName, cmd.CommandID)
	result := executor.GetStatus(context.Background(), cmd)
	sendResult(conn, result)
}

func handleDiscoverProjects(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.DiscoverProjectsCommand](payload)
	if err != nil {
		log.Printf("[AGENT] Invalid discover_projects payload: %v", err)
		return
	}
	log.Printf("[AGENT] discover_projects (command: %s)", cmd.CommandID)
	result := executor.DiscoverProjects(context.Background(), cmd)
	sendResult(conn, result)
}

func handleReadFile(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.ReadFileCommand](payload)
	if err != nil {
		log.Printf("[AGENT] Invalid read_file payload: %v", err)
		return
	}
	log.Printf("[AGENT] read_file path=%s (command: %s)", cmd.Path, cmd.CommandID)
	result := executor.ReadFile(context.Background(), cmd)
	sendResult(conn, result)
}

func handleRunJob(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.RunJobCommand](payload)
	if err != nil {
		log.Printf("[AGENT] Invalid run_job payload: %v", err)
		return
	}
	log.Printf("[AGENT] run_job job_run=%s image=%s (command: %s)", cmd.JobRunID, cmd.Image, cmd.CommandID)

	// Mark the job as active before starting so the next heartbeat includes it.
	activeJobs.Store(cmd.JobRunID, struct{}{})

	result := executor.RunJob(cmd, func(msgType protocol.MessageType, p interface{}) {
		// Container has exited — remove from the liveness map and push the result.
		activeJobs.Delete(cmd.JobRunID)
		msg, marshalErr := json.Marshal(protocol.Envelope{Type: msgType, Payload: p})
		if marshalErr != nil {
			log.Printf("[AGENT] Failed to marshal job completion: %v", marshalErr)
			return
		}
		connWriteMu.Lock()
		writeErr := conn.WriteMessage(websocket.TextMessage, msg)
		connWriteMu.Unlock()
		if writeErr != nil {
			log.Printf("[AGENT] Failed to send job completion for run %s: %v", cmd.JobRunID, writeErr)
		}
	})

	// Send the immediate ack — this unblocks the server's Dispatch call.
	sendResult(conn, result)
}

func handleKillJob(conn *websocket.Conn, payload interface{}) {
	cmd, err := unmarshalPayload[protocol.KillJobCommand](payload)
	if err != nil {
		log.Printf("[AGENT] Invalid kill_job payload: %v", err)
		return
	}
	log.Printf("[AGENT] kill_job job_run=%s (command: %s)", cmd.JobRunID, cmd.CommandID)
	result := executor.KillJob(cmd)
	sendResult(conn, result)
}

func sendResult(conn *websocket.Conn, result protocol.CommandResult) {
	msg, err := json.Marshal(protocol.Envelope{Type: protocol.MsgResult, Payload: result})
	if err != nil {
		log.Printf("[AGENT] Failed to marshal result: %v", err)
		return
	}
	connWriteMu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, msg)
	connWriteMu.Unlock()
	if err != nil {
		log.Printf("[AGENT] Failed to send result: %v", err)
	}
}

// parseTags splits a comma-separated tag string into a trimmed, non-empty slice.
func parseTags(raw string) []string {
	var tags []string
	for _, t := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(t); trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	return tags
}

// unmarshalPayload re-marshals the interface{} payload into T via JSON round-trip.
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
