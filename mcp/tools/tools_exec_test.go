package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

// execRouteServer is a minimal one-shot-exec worker façade. In particular it
// has no terminal endpoint: tests prove the MCP tool never needs one.
func execRouteServer(t *testing.T, result string, status int) (*httptest.Server, *[]string, *[]map[string]any) {
	t.Helper()
	var calls []string
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api/custom/stacks/stack0000000001/services":
			writeFixture(w, `[{"service_name":"web","container_id":"c-web","container_name":"web.1"}]`)
		case strings.HasSuffix(r.URL.Path, "/exec"):
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			bodies = append(bodies, body)
			if status != 0 {
				w.WriteHeader(status)
			}
			writeFixture(w, result)
		default:
			w.WriteHeader(http.StatusNotFound)
			writeFixture(w, `{"error":"unexpected route"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &bodies
}

func TestExecInContainerUsesOneShotRouteAndSeparatesStreams(t *testing.T) {
	srv, calls, bodies := execRouteServer(t, `{"stdout":"ok\n","stderr":"warn\n","exit_code":3,"timed_out":false}`, 0)
	_, out, err := execInContainer(client.New(srv.URL))(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID: "stack0000000001", ServiceName: "web", Command: "cat /etc/hosts",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := out.(map[string]any)
	if len(result) != 5 {
		t.Fatalf("exec response must have the one stable shape, got %#v", result)
	}
	if result["stdout"] != "ok\n" || result["stderr"] != "warn\n" || result["exit_code"] != 3 {
		t.Fatalf("expected separated streams and exit code, got %#v", result)
	}
	if len(*bodies) != 1 {
		t.Fatalf("expected one exec body, got %#v", bodies)
	}
	cmd, _ := (*bodies)[0]["cmd"].([]any)
	if len(cmd) != 3 || cmd[0] != "/bin/sh" || cmd[1] != "-c" || cmd[2] != "cat /etc/hosts" {
		t.Fatalf("unexpected argv %#v", cmd)
	}
	for _, call := range *calls {
		if strings.Contains(call, "/terminal/") {
			t.Fatalf("one-shot exec must not open a terminal: %#v", calls)
		}
	}
}

func TestExecInContainerAcceptsAliasesAndArgv(t *testing.T) {
	srv, _, bodies := execRouteServer(t, `{"stdout":"","stderr":"","exit_code":0}`, 0)
	_, _, err := execInContainer(client.New(srv.URL))(ctxWithKey(), nil, models.ExecInContainerInput{
		Stack: "stack0000000001", ContainerName: "web.1", Argv: []string{"cat", "/etc/hosts"},
	})
	if err != nil {
		t.Fatalf("aliases should resolve: %v", err)
	}
	cmd, _ := (*bodies)[0]["cmd"].([]any)
	if len(cmd) != 2 || cmd[0] != "cat" || cmd[1] != "/etc/hosts" {
		t.Fatalf("argv should pass through unchanged: %#v", cmd)
	}
}

func TestExecInContainerRejectsConflictingAliases(t *testing.T) {
	srv, calls, _ := execRouteServer(t, `{}`, 0)
	_, _, err := execInContainer(client.New(srv.URL))(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID: "stack0000000001", Stack: "another-stack", ContainerID: "c-web", Command: "ls",
	})
	if err == nil || !strings.Contains(err.Error(), "different values") {
		t.Fatalf("expected alias conflict, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("alias conflict must be local validation, got calls %#v", calls)
	}
}

func TestExecInContainerKeepsPartialOutputOnTimeout(t *testing.T) {
	srv, _, _ := execRouteServer(t, `{"stdout":"partial","stderr":"","exit_code":-1,"timed_out":true}`, 0)
	_, out, err := execInContainer(client.New(srv.URL))(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID: "stack0000000001", ServiceName: "web", Command: "sleep 600",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := out.(map[string]any)
	if result["stdout"] != "partial" || result["timed_out"] != true || result["exit_code"] != nil {
		t.Fatalf("expected partial timeout result, got %#v", result)
	}
}

func TestExecInContainerPropagatesCapabilityConflictWithoutTerminal(t *testing.T) {
	srv, calls, _ := execRouteServer(t, `{"error":"the assigned worker does not support one-shot exec","required_capability":"exec_one_shot_v1"}`, http.StatusConflict)
	_, _, err := execInContainer(client.New(srv.URL))(ctxWithKey(), nil, models.ExecInContainerInput{
		StackID: "stack0000000001", ServiceName: "web", Command: "echo hello",
	})
	if err == nil || !strings.Contains(err.Error(), "exec_one_shot_v1") {
		t.Fatalf("expected capability refusal, got %v", err)
	}
	for _, call := range *calls {
		if strings.Contains(call, "/terminal/") {
			t.Fatalf("missing capability must not open a terminal: %#v", calls)
		}
	}
}

func TestExecArgvRendering(t *testing.T) {
	if got := execArgv(models.ExecInContainerInput{Command: "ls -la"}); strings.Join(got, "|") != "/bin/sh|-c|ls -la" {
		t.Fatalf("default shell argv = %#v", got)
	}
	if got := execArgv(models.ExecInContainerInput{Argv: []string{"ps", "aux"}}); strings.Join(got, "|") != "ps|aux" {
		t.Fatalf("direct argv = %#v", got)
	}
}
