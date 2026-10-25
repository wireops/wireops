package tools

import (
	"context"
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
)

// registerExecTools adds the use_terminal-scoped tool.
func registerExecTools(server *mcp.Server, c *client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "exec_in_container",
		Description: "Run a one-shot command inside a container and return cmd, stdout, stderr, exit_code and timed_out. Give command (through a shell) or argv (direct). Aliases: stack for stack_id and container_name for container_id. This never opens a terminal-session fallback: a worker without exec_one_shot_v1 returns 409 with upgrade guidance. The call is audited with its argv; pass a terminating command.",
		Annotations: destructive(false),
	}, execInContainer(c))
}

func execInContainer(c *client.Client) mcp.ToolHandlerFor[models.ExecInContainerInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ExecInContainerInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		stackID, err := resolveStackInput(ctx, c, apiKey, in.StackID, in.Stack)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(in.Command) == "" && len(in.Argv) == 0 {
			return nil, nil, fmt.Errorf("either command or argv is required")
		}
		if strings.TrimSpace(in.Command) != "" && len(in.Argv) > 0 {
			return nil, nil, fmt.Errorf("pass either command or argv, not both")
		}

		containerID, _, err := resolveContainerInput(ctx, c, apiKey, stackID, in.ContainerID, in.ContainerName, in.ServiceName)
		if err != nil {
			return nil, nil, err
		}

		result, err := execViaRoute(ctx, c, apiKey, stackID, containerID, in)
		if err != nil {
			return nil, nil, err
		}
		return nil, result, nil
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
func execViaRoute(ctx context.Context, c *client.Client, apiKey, stackID, containerID string, in models.ExecInContainerInput) (map[string]any, error) {
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
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
		TimedOut bool   `json:"timed_out"`
	}
	path := "/api/custom/stacks/" + url.PathEscape(stackID) + "/exec"
	if err := c.PostWithTimeout(ctx, apiKey, path, nil, body, &out, time.Duration(timeoutSeconds)*time.Second+20*time.Second); err != nil {
		return nil, err
	}

	result := map[string]any{
		"cmd":       execArgv(in),
		"stdout":    out.Stdout,
		"stderr":    out.Stderr,
		"exit_code": out.ExitCode,
		"timed_out": out.TimedOut,
	}
	if out.TimedOut {
		// Null, not 0: a command that was still running has no exit status,
		// and 0 would read as success.
		result["exit_code"] = nil
	}
	return result, nil
}
