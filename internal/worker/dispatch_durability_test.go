package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wireops/wireops/internal/protocol"
)

// dialWorker connects a real websocket client to the given httptest server
// using workerToken for auth, mirroring what the worker binary does.
func dialWorker(t *testing.T, httpServerURL, workerToken string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(httpServerURL, "http") + "/worker/ws"
	headers := http.Header{}
	headers.Set("X-Wireops-Worker-Token", workerToken)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		body := ""
		if resp != nil {
			body = resp.Status
			if resp.Body != nil {
				resp.Body.Close()
			}
		}
		t.Fatalf("failed to dial worker ws: %v (resp=%s)", err, body)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func readEnvelope(t *testing.T, conn *websocket.Conn) protocol.Envelope {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read envelope: %v", err)
	}
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}
	return env
}

func sendResult(t *testing.T, conn *websocket.Conn, result protocol.CommandResult) {
	t.Helper()
	env := protocol.Envelope{Type: protocol.MsgResult, Payload: result}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to write result: %v", err)
	}
}

func TestDispatchConnectedWorkerSetsMessageIDAndSucceeds(t *testing.T) {
	app := newWorkerTestApp(t)
	svc := NewService(app)
	server := NewWorkerServer(app, svc)
	httpServer := httptest.NewServer(server.engine)
	defer httpServer.Close()

	token, _, err := svc.IssueToken("admin-1")
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	conn := dialWorker(t, httpServer.URL, token)

	resultCh := make(chan protocol.CommandResult, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := server.Dispatch(context.Background(), workerIDFromConnection(t, server), protocol.DeployCommand{
			CommandID: "cmd-connected-1",
			StackID:   "stack-1",
		})
		resultCh <- res
		errCh <- err
	}()

	env := readEnvelope(t, conn)
	if env.Type != protocol.MsgDeploy {
		t.Fatalf("expected deploy envelope, got %v", env.Type)
	}
	payloadBytes, _ := json.Marshal(env.Payload)
	var cmd protocol.DeployCommand
	_ = json.Unmarshal(payloadBytes, &cmd)
	if cmd.CommandID != "cmd-connected-1" {
		t.Fatalf("command_id = %q, want cmd-connected-1", cmd.CommandID)
	}
	if cmd.MessageID == "" {
		t.Fatal("expected a non-empty message_id on the dispatched deploy command")
	}

	sendResult(t, conn, protocol.CommandResult{CommandID: "cmd-connected-1", Output: "ok"})

	select {
	case res := <-resultCh:
		if res.Output != "ok" {
			t.Fatalf("expected output=ok, got %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Dispatch to return")
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
}

func TestDispatchQueuesWhenWorkerOfflineThenReplaysOnReconnect(t *testing.T) {
	app := newWorkerTestApp(t)
	svc := NewService(app)
	server := NewWorkerServer(app, svc)
	httpServer := httptest.NewServer(server.engine)
	defer httpServer.Close()

	token, _, err := svc.IssueToken("admin-1")
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	// Resolve the workerID that this token will bind to without connecting yet,
	// by activating and immediately discarding a throwaway connection.
	workerID := workerIDFromToken(t, svc, token)

	resultCh := make(chan protocol.CommandResult, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := server.Dispatch(context.Background(), workerID, protocol.TeardownCommand{
			CommandID: "cmd-offline-1",
			StackID:   "stack-1",
		})
		resultCh <- res
		errCh <- err
	}()

	// Give Dispatch time to persist the queued row before the worker connects.
	deadline := time.Now().Add(2 * time.Second)
	for {
		records, _ := app.FindAllRecords("worker_commands", nil)
		found := false
		for _, r := range records {
			if r.GetString("command_id") == "cmd-offline-1" && r.GetString("status") == "queued" {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for command to be persisted as queued")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Now the worker comes online: replayPendingOnReconnect should resend it.
	conn := dialWorker(t, httpServer.URL, token)

	env := readEnvelope(t, conn)
	if env.Type != protocol.MsgTeardown {
		t.Fatalf("expected teardown envelope replayed on reconnect, got %v", env.Type)
	}
	payloadBytes, _ := json.Marshal(env.Payload)
	var cmd protocol.TeardownCommand
	_ = json.Unmarshal(payloadBytes, &cmd)
	if cmd.CommandID != "cmd-offline-1" {
		t.Fatalf("replayed command_id = %q, want cmd-offline-1", cmd.CommandID)
	}
	if cmd.MessageID == "" {
		t.Fatal("expected replayed command to carry a message_id")
	}

	sendResult(t, conn, protocol.CommandResult{CommandID: "cmd-offline-1", Output: "torn-down"})

	select {
	case res := <-resultCh:
		if res.Output != "torn-down" {
			t.Fatalf("expected output=torn-down, got %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Dispatch to return after replay")
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch returned error after replay: %v", err)
	}

	records, err := app.FindAllRecords("worker_commands", nil)
	if err != nil {
		t.Fatalf("FindAllRecords failed: %v", err)
	}
	for _, r := range records {
		if r.GetString("command_id") == "cmd-offline-1" {
			if got := r.GetString("status"); got != "success" {
				t.Fatalf("final status = %q, want success", got)
			}
		}
	}
}

// workerIDFromToken activates the token via a short-lived connection (so the
// worker record is created and the token becomes bound/active), then closes
// it immediately so the worker is "offline" for the actual test.
func workerIDFromToken(t *testing.T, svc *Service, token string) string {
	t.Helper()
	workerRecord, _, err := svc.ActivateToken(token, "offline-worker")
	if err != nil {
		t.Fatalf("ActivateToken failed: %v", err)
	}
	return workerRecord.Id
}

// workerIDFromConnection returns the worker ID bound to the currently (only)
// connected worker, for tests that dial before calling Dispatch.
func workerIDFromConnection(t *testing.T, server *WorkerServer) string {
	t.Helper()
	server.connMu.RLock()
	defer server.connMu.RUnlock()
	for id := range server.connections {
		return id
	}
	t.Fatal("no connected worker found")
	return ""
}
