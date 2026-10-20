package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

const (
	// execDefaultTimeout is how long we wait for a command's output before
	// giving up; execMaxTimeout bounds what a caller may ask for, so a tool
	// call can never outlive the MCP client's own patience by much.
	execDefaultTimeout = 30 * time.Second
	execMaxTimeout     = 120 * time.Second
	// execPollInterval is how often the transcript is re-read, and
	// execQuietPeriod how long the output has to stop growing before the
	// command is considered finished. A pty gives no completion signal, so
	// "nothing new for a while" is the only end-of-output we get.
	execPollInterval = 300 * time.Millisecond
	execQuietPeriod  = time.Second
)

// registerExecTools adds the use_terminal-scoped tool.
func registerExecTools(server *mcp.Server, c *client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "exec_in_container",
		Description: "Run a one-shot shell command inside a container of a wireops stack and return its output — the MCP equivalent of the panel's terminal, for inspection a log line can't answer (checking a config file inside the image, a DNS lookup from the container's network, disk usage). It opens a real pty session, sends the command, waits for the output to settle, then closes the session; everything typed and printed is recorded in that session's auditable transcript under the calling API key. This runs ARBITRARY commands as the container's user, so it can change or break the container's state — prefer get_container_logs, get_stack_services and get_container_stats when they can answer the question. Identify the container with container_id or service_name. Output detection is quiet-period based, so a command that prints nothing returns empty and a long-running or interactive command (top, tail -f, a shell waiting for input) will simply hit the timeout — pass a command that terminates.",
		Annotations: destructive(false),
	}, execInContainer(c))
}

func execInContainer(c *client.Client) mcp.ToolHandlerFor[models.ExecInContainerInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ExecInContainerInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(in.Command) == "" {
			return nil, nil, fmt.Errorf("command is required")
		}

		containerID, serviceName, err := resolveContainerTarget(ctx, c, apiKey, in.StackID, in.ContainerID, in.ServiceName)
		if err != nil {
			return nil, nil, err
		}

		timeout := execDefaultTimeout
		if in.TimeoutSeconds > 0 {
			timeout = time.Duration(in.TimeoutSeconds) * time.Second
			if timeout > execMaxTimeout {
				timeout = execMaxTimeout
			}
		}

		shell := in.Shell
		if len(shell) == 0 {
			shell = []string{"/bin/sh"}
		}

		var opened struct {
			SessionID string `json:"session_id"`
		}
		openPath := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/terminal/sessions"
		openBody := map[string]any{"container_id": containerID, "shell": shell, "rows": 24, "cols": 120}
		if err := c.Post(ctx, apiKey, openPath, nil, openBody, &opened); err != nil {
			return nil, nil, fmt.Errorf("opening terminal session: %w", err)
		}
		if opened.SessionID == "" {
			return nil, nil, fmt.Errorf("terminal session response carried no session_id")
		}
		// Close on every path: an abandoned pty stays open until the
		// server's 15-minute idle sweeper notices it, holding a shell in
		// the container the whole time.
		defer closeTerminalSession(c, apiKey, opened.SessionID)

		// Whatever the shell prints on startup (banner, prompt) is not this
		// command's output, so capture it as a baseline to subtract.
		baseline, err := readTranscript(ctx, c, apiKey, opened.SessionID)
		if err != nil {
			return nil, nil, err
		}

		inputPath := "/api/custom/terminal/sessions/" + url.PathEscape(opened.SessionID) + "/input"
		if err := c.Post(ctx, apiKey, inputPath, nil, map[string]any{"data": in.Command + "\n"}, nil); err != nil {
			return nil, nil, fmt.Errorf("sending command: %w", err)
		}

		output, timedOut, err := awaitTranscriptOutput(ctx, c, apiKey, opened.SessionID, len(baseline), timeout)
		if err != nil {
			return nil, nil, err
		}

		return nil, map[string]any{
			"stack_id":     in.StackID,
			"container_id": containerID,
			"service_name": serviceName,
			"session_id":   opened.SessionID,
			"command":      in.Command,
			"output":       output,
			"timed_out":    timedOut,
		}, nil
	}
}

// closeTerminalSession tears the pty down, ignoring errors — the caller is
// already returning, and a failed close is the idle sweeper's problem.
func closeTerminalSession(c *client.Client, apiKey, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := "/api/custom/terminal/sessions/" + url.PathEscape(sessionID) + "/close"
	_ = c.Post(ctx, apiKey, path, nil, struct{}{}, nil)
}

// awaitTranscriptOutput polls the session transcript until it stops growing
// for execQuietPeriod, or the deadline passes, and returns everything written
// after baseLen. A pty has no end-of-command marker, so settling is the only
// available signal; timedOut tells the caller the output may be truncated.
func awaitTranscriptOutput(ctx context.Context, c *client.Client, apiKey, sessionID string, baseLen int, timeout time.Duration) (string, bool, error) {
	deadline := time.Now().Add(timeout)
	lastLen := baseLen
	lastGrowth := time.Now()
	var decoded string

	for {
		select {
		case <-ctx.Done():
			return decoded, false, ctx.Err()
		case <-time.After(execPollInterval):
		}

		full, err := readTranscript(ctx, c, apiKey, sessionID)
		if err != nil {
			return "", false, err
		}
		if len(full) > lastLen {
			lastLen = len(full)
			lastGrowth = time.Now()
		}
		if len(full) > baseLen {
			decoded = full[baseLen:]
		}

		// Only settle once something actually arrived; otherwise a command
		// whose first byte is slow would return empty after one quiet
		// period.
		if len(full) > baseLen && time.Since(lastGrowth) >= execQuietPeriod {
			return decoded, false, nil
		}
		if time.Now().After(deadline) {
			return decoded, true, nil
		}
	}
}

// readTranscript fetches a session's asciicast-v2 transcript and returns the
// concatenated pty output. A transcript that does not exist yet (the file is
// written asynchronously as output arrives) reads as empty, not as an error.
func readTranscript(ctx context.Context, c *client.Client, apiKey, sessionID string) (string, error) {
	path := "/api/custom/terminal/sessions/" + url.PathEscape(sessionID) + "/transcript"
	raw, err := c.GetRaw(ctx, apiKey, path, nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			return "", nil
		}
		return "", fmt.Errorf("reading terminal transcript: %w", err)
	}
	return decodeAsciicast(raw), nil
}

// decodeAsciicast turns an asciicast-v2 NDJSON transcript into plain text.
// The first line is the header object; every later line is
// [elapsed, "o", base64(bytes)] — base64 because pty output is not
// guaranteed to be valid UTF-8 (see internal/termsession.AppendTranscript).
// Malformed lines are skipped rather than failing the whole read: a
// transcript can legitimately be read mid-write, with a torn final line.
func decodeAsciicast(raw []byte) string {
	var sb strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "[") {
			continue
		}
		var event []json.RawMessage
		if err := json.Unmarshal([]byte(line), &event); err != nil || len(event) < 3 {
			continue
		}
		var kind string
		if err := json.Unmarshal(event[1], &kind); err != nil || kind != "o" {
			continue
		}
		var payload string
		if err := json.Unmarshal(event[2], &payload); err != nil {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			continue
		}
		sb.Write(data)
	}
	return sb.String()
}
