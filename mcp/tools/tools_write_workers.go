package tools

import (
	"context"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

// registerWorkerWriteTools adds the manage_workers-scoped tools. That
// capability gates exactly one route — every other worker mutation (policy,
// enrollment tokens) is admin-only and therefore unreachable with an API key,
// since a service account can only be viewer or operator.
func registerWorkerWriteTools(server *mcp.Server, c *client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "revoke_worker",
		Description: "Revoke a wireops worker: its token is invalidated and its WebSocket connection dropped, so it can no longer receive deploys until it is re-enrolled with a new token (an admin-only action, not available over MCP). Stacks assigned to it stop reconciling and their containers are left running on the host. confirm must be the worker's exact hostname, as shown by list_workers.",
		Annotations: destructive(true),
	}, revokeWorker(c))
}

func revokeWorker(c *client.Client) mcp.ToolHandlerFor[models.RevokeWorkerInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.RevokeWorkerInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if err := requireConfirm(ctx, c, apiKey, confirmWorkers, in.WorkerID, in.Confirm); err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/workers/" + url.PathEscape(in.WorkerID) + "/revoke"
		if err := c.Post(ctx, apiKey, path, nil, struct{}{}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}
