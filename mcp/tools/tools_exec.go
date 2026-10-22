package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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
	// execPollInterval is how often the transcript is re-read while waiting
	// for the end-of-command marker.
	execPollInterval = 300 * time.Millisecond
	// execMarkerPrefix starts the unique end-of-command marker. A pty gives
	// no completion signal, so the command is followed by a printf that emits
	// this marker plus the exit status, and that line — not a lull in the
	// output — is what ends the wait.
	execMarkerPrefix = "WIREOPS_EOC_"
)

// registerExecTools adds the use_terminal-scoped tool.
func registerExecTools(server *mcp.Server, c *client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "exec_in_container",
		Description: "Run a one-shot command inside a container of a wireops stack and return its stdout, stderr and exit code — for inspection a log line cannot answer (reading a config file inside the image, a DNS lookup from the container's network, disk usage). Give the command either as command (run through a shell, so pipes work) or as argv (run directly, for a container without a shell). Identify the container with container_id or service_name. This runs ARBITRARY commands as the container's user and can change or break its state, so prefer get_container_logs, get_stack_services and get_container_stats when they can answer the question. The call is recorded in the audit log with its argv. Pass a command that terminates: an interactive or never-ending one (top, tail -f, a shell waiting for input) hits the timeout instead. A _notice in the response explains any caveat that applied to the run.",
		Annotations: destructive(false),
	}, execInContainer(c))
}

func execInContainer(c *client.Client) mcp.ToolHandlerFor[models.ExecInContainerInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ExecInContainerInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(in.Command) == "" && len(in.Argv) == 0 {
			return nil, nil, fmt.Errorf("either command or argv is required")
		}
		if strings.TrimSpace(in.Command) != "" && len(in.Argv) > 0 {
			return nil, nil, fmt.Errorf("pass either command or argv, not both")
		}

		containerID, serviceName, err := resolveContainerTarget(ctx, c, apiKey, stackID, in.ContainerID, in.ServiceName)
		if err != nil {
			return nil, nil, err
		}

		// The server-side one-shot exec is the real thing: the container
		// runtime reports the exit code and keeps stdout and stderr apart.
		// A worker too old to support it answers 409, and only then is the
		// pty session driven instead.
		result, err := execViaRoute(ctx, c, apiKey, stackID, containerID, serviceName, in)
		if err == nil {
			return nil, result, nil
		}
		var apiErr *client.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
			return nil, nil, err
		}

		fallback, err := execViaTerminal(ctx, c, apiKey, stackID, containerID, serviceName, in)
		if err != nil {
			return nil, nil, err
		}
		return nil, fallback, nil
	}
}

// execArgv renders the tool's input as the argv the exec route takes: argv
// as given, or the command line wrapped in the requested shell.
func execArgv(in models.ExecInContainerInput) []string {
	if len(in.Argv) > 0 {
		return in.Argv
	}
	shell := in.Shell
	if len(shell) == 0 {
		shell = []string{"/bin/sh"}
	}
	return append(append([]string{}, shell...), "-c", in.Command)
}

// execViaRoute runs the command through POST /api/custom/stacks/{id}/exec.
func execViaRoute(ctx context.Context, c *client.Client, apiKey, stackID, containerID, serviceName string, in models.ExecInContainerInput) (map[string]any, error) {
	timeoutSeconds := int(execDefaultTimeout.Seconds())
	if in.TimeoutSeconds > 0 {
		timeoutSeconds = in.TimeoutSeconds
		if max := int(execMaxTimeout.Seconds()); timeoutSeconds > max {
			timeoutSeconds = max
		}
	}

	body := map[string]any{
		"container_id":    containerID,
		"cmd":             execArgv(in),
		"timeout_seconds": timeoutSeconds,
	}
	var out struct {
		Stdout          string `json:"stdout"`
		Stderr          string `json:"stderr"`
		ExitCode        int    `json:"exit_code"`
		TimedOut        bool   `json:"timed_out"`
		StdoutTruncated bool   `json:"stdout_truncated"`
		StderrTruncated bool   `json:"stderr_truncated"`
	}
	path := "/api/custom/stacks/" + url.PathEscape(stackID) + "/exec"
	if err := c.Post(ctx, apiKey, path, nil, body, &out); err != nil {
		return nil, err
	}

	result := map[string]any{
		"stack_id":     stackID,
		"container_id": containerID,
		"service_name": serviceName,
		"cmd":          execArgv(in),
		"stdout":       out.Stdout,
		"stderr":       out.Stderr,
		"exit_code":    out.ExitCode,
		"timed_out":    out.TimedOut,
	}
	if out.TimedOut {
		// Null, not 0: a command that was still running has no exit status,
		// and 0 would read as success.
		result["exit_code"] = nil
		result["_notice"] = "The command did not finish within the timeout, so exit_code is unknown and the output may be cut short. The worker kills the exec at its own deadline."
	}
	if out.StdoutTruncated || out.StderrTruncated {
		appendNotice(result, "Output hit the worker's per-stream size cap and was truncated. Narrow the command (head, grep, --tail) rather than raising the cap.")
	}
	return result, nil
}

// execViaTerminal is the pre-exec-command path: open a pty session, send the
// command followed by a marker that prints $?, read the transcript back. Kept
// only for workers that do not advertise CapabilityExecOneShotV1 — it cannot
// separate stdout from stderr (a pty is one stream) and needs a POSIX shell
// for the marker.
func execViaTerminal(ctx context.Context, c *client.Client, apiKey, stackID, containerID, serviceName string, in models.ExecInContainerInput) (map[string]any, error) {
	command := in.Command
	if command == "" {
		// The pty path can only send a line of shell, so an argv has to be
		// quoted back into one.
		quoted := make([]string, 0, len(in.Argv))
		for _, arg := range in.Argv {
			quoted = append(quoted, shellQuote(arg))
		}
		command = strings.Join(quoted, " ")
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
	openPath := "/api/custom/stacks/" + url.PathEscape(stackID) + "/terminal/sessions"
	openBody := map[string]any{"container_id": containerID, "shell": shell, "rows": 24, "cols": 120}
	if err := c.Post(ctx, apiKey, openPath, nil, openBody, &opened); err != nil {
		return nil, fmt.Errorf("opening terminal session: %w", err)
	}
	if opened.SessionID == "" {
		return nil, fmt.Errorf("terminal session response carried no session_id")
	}
	// Close on every path: an abandoned pty stays open until the
	// server's 15-minute idle sweeper notices it, holding a shell in
	// the container the whole time.
	defer closeTerminalSession(c, apiKey, opened.SessionID)

	// Whatever the shell prints on startup (banner, prompt) is not this
	// command's output, so capture it as a baseline to subtract.
	baseline, err := readTranscript(ctx, c, apiKey, opened.SessionID)
	if err != nil {
		return nil, err
	}

	// The marker is passed to printf as an argument, so the line the pty
	// echoes back carries it next to a quote while the real end of output
	// carries it next to ":<exit status>" — which is how
	// awaitTranscriptOutput tells the two apart. On its own line (leading
	// \n) so a command whose output has no trailing newline can't run into
	// it.
	marker := execMarkerPrefix + opened.SessionID
	inputPath := "/api/custom/terminal/sessions/" + url.PathEscape(opened.SessionID) + "/input"
	data := command + "\nprintf '\\n%s:%d\\n' '" + marker + "' $?\n"
	if err := c.Post(ctx, apiKey, inputPath, nil, map[string]any{"data": data}, nil); err != nil {
		return nil, fmt.Errorf("sending command: %w", err)
	}

	output, exitCode, timedOut, err := awaitTranscriptOutput(ctx, c, apiKey, opened.SessionID, len(baseline), marker, timeout)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"stack_id":     stackID,
		"container_id": containerID,
		"service_name": serviceName,
		"session_id":   opened.SessionID,
		"command":      command,
		"output":       output,
		"timed_out":    timedOut,
		// Null rather than 0 when the marker never arrived: a timed-out
		// command has no exit status, and reporting 0 would read as success.
		"exit_code": nil,
	}
	if exitCode != nil {
		result["exit_code"] = *exitCode
	}
	appendNotice(result, "This worker does not support one-shot exec, so the command ran through a pty session: stdout and stderr are interleaved in output (not separate fields), the shell must be POSIX, and completion is detected by a marker command appended after yours. Upgrade the worker for exact results.")
	if timedOut {
		appendNotice(result, "The command did not finish within the timeout, so exit_code is null (unknown, not 0) and the output above may be cut short.")
	}
	return result, nil
}

// shellQuote wraps an argv element in single quotes for the pty fallback,
// escaping any single quote it contains.
func shellQuote(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// closeTerminalSession tears the pty down, ignoring errors — the caller is
// already returning, and a failed close is the idle sweeper's problem.
func closeTerminalSession(c *client.Client, apiKey, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := "/api/custom/terminal/sessions/" + url.PathEscape(sessionID) + "/close"
	_ = c.Post(ctx, apiKey, path, nil, struct{}{}, nil)
}

// awaitTranscriptOutput polls the session transcript until the end-of-command
// marker appears, or the deadline passes, and returns everything written
// after baseLen and before the marker, plus the command's exit status.
//
// Waiting for a lull in the output instead would be wrong: the pty echoes the
// command back as soon as it is sent, so the transcript grows before the
// command has printed anything, and a command that is quiet for a moment
// before its first byte (du -sh /, a slow DNS lookup) would be reported as
// finished with no output. exitCode is nil and timedOut true when the marker
// never arrived, in which case the output may be truncated.
func awaitTranscriptOutput(ctx context.Context, c *client.Client, apiKey, sessionID string, baseLen int, marker string, timeout time.Duration) (string, *int, bool, error) {
	deadline := time.Now().Add(timeout)
	markerRe := regexp.MustCompile(regexp.QuoteMeta(marker) + `:(\d+)`)
	var decoded string

	for {
		select {
		case <-ctx.Done():
			return decoded, nil, false, ctx.Err()
		case <-time.After(execPollInterval):
		}

		full, err := readTranscript(ctx, c, apiKey, sessionID)
		if err != nil {
			return "", nil, false, err
		}
		if len(full) > baseLen {
			decoded = full[baseLen:]
		}

		if loc := markerRe.FindStringSubmatchIndex(decoded); loc != nil {
			output := stripEchoedInput(decoded[:loc[0]], marker)
			// The newline the marker command prints ahead of the marker is
			// ours, not something the command itself printed.
			output = strings.TrimSuffix(output, "\n")
			output = strings.TrimSuffix(output, "\r")
			if code, convErr := strconv.Atoi(decoded[loc[2]:loc[3]]); convErr == nil {
				return output, &code, false, nil
			}
			return output, nil, false, nil
		}
		if time.Now().After(deadline) {
			return stripEchoedInput(decoded, marker), nil, true, nil
		}
	}
}

// stripEchoedInput drops the input line the pty echoed back. The session is a
// real pty with ECHO on, so everything sent to it reappears in the transcript
// ahead of any output; the echo of the trailing marker command is the last
// thing echoed, so cutting past the newline that ends it leaves exactly what
// the command itself printed. A session that does not echo has nothing to cut
// and is returned unchanged.
func stripEchoedInput(output, marker string) string {
	i := strings.Index(output, marker)
	if i < 0 {
		return output
	}
	nl := strings.IndexByte(output[i:], '\n')
	if nl < 0 {
		return output
	}
	return output[i+nl+1:]
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
