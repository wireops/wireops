// Package prompts registers the wireops MCP prompt templates — canned,
// discoverable (prompts/list) message sequences that operationalize a
// specific use case rather than leaving the client to chain tool calls
// itself. The prompt handler pre-fetches real data through the existing
// REST API (same pass-through auth as mcp/tools) and embeds it directly
// in the returned message, so the model gets grounded context instead of
// free-form tool-calling authority.
package prompts

import (
	"context"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wireops/wireops/mcp/auth"
	"github.com/wireops/wireops/mcp/client"
)

// Register adds every wireops prompt template to server.
func Register(server *mcp.Server, c *client.Client) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "diagnose_stack_failure",
		Description: "Diagnose why a wireops stack's most recent sync/deploy failed, using its current status and recent sync log history.",
		Arguments: []*mcp.PromptArgument{
			{Name: "stack_id", Description: "The wireops stack record id to diagnose.", Required: true},
		},
	}, diagnoseStackFailure(c))
}

func apiKeyFrom(ctx context.Context) (string, error) {
	apiKey, ok := auth.APIKeyFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("no wireops API key on this MCP session — pass one via the %s header when connecting", "X-Wireops-Api-Key")
	}
	return apiKey, nil
}

func diagnoseStackFailure(c *client.Client) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		stackID := req.Params.Arguments["stack_id"]
		if stackID == "" {
			return nil, fmt.Errorf("stack_id argument is required")
		}

		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, err
		}

		var status any
		statusPath := "/api/collections/stacks/records/" + url.PathEscape(stackID)
		if err := c.Get(ctx, apiKey, statusPath, nil, &status); err != nil {
			return nil, fmt.Errorf("fetching stack status: %w", err)
		}

		var logs any
		q := url.Values{
			"filter":  {fmt.Sprintf("stack='%s'", client.EscapeFilterValue(stackID))},
			"sort":    {"-created"},
			"perPage": {"5"},
		}
		if err := c.Get(ctx, apiKey, "/api/collections/sync_logs/records", q, &logs); err != nil {
			return nil, fmt.Errorf("fetching sync logs: %w", err)
		}

		text := fmt.Sprintf(`Diagnose why wireops stack %s is failing to deploy/sync.

Current stack status:
%v

Last 5 sync log entries (most recent first):
%v

Identify the most likely root cause (e.g. missing/invalid env var, port conflict, image not found, compose syntax error, worker offline). If the sync log output references a specific container, call get_container_logs for that container to confirm before concluding. State your hypothesis and the evidence for it; do not take any write action.`, stackID, status, logs)

		return &mcp.GetPromptResult{
			Description: "Root-cause diagnosis for a failing wireops stack, grounded in its current status and recent sync history.",
			Messages: []*mcp.PromptMessage{
				{
					Role:    "user",
					Content: &mcp.TextContent{Text: text},
				},
			},
		}, nil
	}
}
