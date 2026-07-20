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

	server.AddPrompt(&mcp.Prompt{
		Name:        "scaffold_new_stack",
		Description: "Research and scaffold a new wireops stack (docker-compose.yml + wireops.yaml) for a described application.",
		Arguments: []*mcp.PromptArgument{
			{Name: "app_description", Description: "What the stack should run, e.g. 'a Postgres database with pgAdmin' or 'Ghost blog behind Traefik'.", Required: true},
			{Name: "image", Description: "A specific Docker image to use, if already known. Optional — leave empty to have the model research one.", Required: false},
		},
	}, scaffoldNewStack())
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

func scaffoldNewStack() mcp.PromptHandler {
	return func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		appDescription := req.Params.Arguments["app_description"]
		if appDescription == "" {
			return nil, fmt.Errorf("app_description argument is required")
		}
		image := req.Params.Arguments["image"]

		imageHint := "You do not have a specific image yet — use your own web search tool to find the official/most appropriate Docker image and its required configuration (ports, volumes, required environment variables) before proceeding."
		if image != "" {
			imageHint = fmt.Sprintf("A candidate image was given: %q. Use your own web search tool to confirm its required configuration (ports, volumes, required environment variables) before proceeding.", image)
		}

		text := fmt.Sprintf(`Scaffold a new wireops stack for: %s

%s

Once you have the image(s) and their required ports/volumes/environment variables, call the scaffold_stack tool with a service definition for each container. Do not invent image names or configuration you have not verified — if unsure, search for the image's official documentation or Docker Hub page first.

If you know which wireops worker this stack will run on, pass its worker_id to scaffold_stack so the generated compose file is checked against that worker's deploy security policy before you present it.`, appDescription, imageHint)

		return &mcp.GetPromptResult{
			Description: "Research-grounded scaffolding for a new wireops stack from a natural-language description.",
			Messages: []*mcp.PromptMessage{
				{
					Role:    "user",
					Content: &mcp.TextContent{Text: text},
				},
			},
		}, nil
	}
}
