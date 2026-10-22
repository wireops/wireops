package worker

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wireops/wireops/internal/protocol"
)

func TestDispatchExecRoundTrip(t *testing.T) {
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
	workerID := workerIDFromConnection(t, server)

	resultCh := make(chan protocol.CommandResult, 1)
	errCh := make(chan error, 1)
	go func() {
		res, err := server.Dispatch(context.Background(), workerID, protocol.ExecCommand{
			CommandID:   "exec-1",
			StackID:     "stack-1",
			ContainerID: "c-web",
			Cmd:         []string{"/bin/sh", "-c", "echo hi"},
		})
		resultCh <- res
		errCh <- err
	}()

	env := readEnvelope(t, conn)
	if env.Type != protocol.MsgExec {
		t.Fatalf("expected an exec envelope, got %v", env.Type)
	}
	payloadBytes, _ := json.Marshal(env.Payload)
	var cmd protocol.ExecCommand
	_ = json.Unmarshal(payloadBytes, &cmd)
	if cmd.CommandID != "exec-1" {
		t.Fatalf("command_id = %q, want exec-1", cmd.CommandID)
	}
	if len(cmd.Cmd) != 3 || cmd.Cmd[2] != "echo hi" {
		t.Fatalf("argv did not survive the round trip: %#v", cmd.Cmd)
	}
	// An exec is not durable: replaying one on reconnect could run a
	// mutating command twice. ExecCommand carries no MessageID field at all,
	// so assert on the wire form — nothing stamped it on the way out.
	var raw map[string]any
	if err := json.Unmarshal(payloadBytes, &raw); err != nil {
		t.Fatalf("decoding payload: %v", err)
	}
	if _, present := raw["message_id"]; present {
		t.Errorf("expected no message_id on a non-durable exec, got %v", raw["message_id"])
	}

	execResult, _ := json.Marshal(protocol.ExecResult{Stdout: "hi\n", ExitCode: 0})
	sendResult(t, conn, protocol.CommandResult{CommandID: "exec-1", Output: string(execResult)})

	select {
	case res := <-resultCh:
		var decoded protocol.ExecResult
		if err := json.Unmarshal([]byte(res.Output), &decoded); err != nil {
			t.Fatalf("decoding exec result: %v", err)
		}
		if decoded.Stdout != "hi\n" || decoded.ExitCode != 0 {
			t.Fatalf("unexpected exec result %+v", decoded)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Dispatch to return")
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Dispatch returned error: %v", err)
	}
}

func TestExecIsNotADurableCommand(t *testing.T) {
	// Durability means replay on reconnect, which for an exec would mean
	// running someone's command a second time, unasked.
	if isDurableCommand(protocol.MsgExec) {
		t.Fatal("MsgExec must not be durable")
	}
}
