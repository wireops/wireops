// Package tools registers the wireops MCP tool set. All tools are
// read-only proxies over the existing REST API — the MCP process has no
// write capability by construction, and the actual permission check is
// left entirely to the wireops server's RBAC on the caller's API key.
package tools

import (
	"context"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wireops/wireops/mcp/auth"
	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

// Register adds every wireops read-only tool to server, calling the
// wireops REST API at c on each invocation.
func Register(server *mcp.Server, c *client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_stacks",
		Description: "List all wireops stacks visible to the caller's API key, with their sync status.",
	}, listStacks(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_status",
		Description: "Get a single wireops stack record by id, including its current sync/deploy status.",
	}, getStackStatus(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_sync_logs",
		Description: "List recent sync log entries for a wireops stack (most recent first).",
	}, getSyncLogs(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_services",
		Description: "List the running containers/services for a wireops stack.",
	}, getStackServices(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_resources",
		Description: "List the Docker volumes and networks associated with a wireops stack.",
	}, getStackResources(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_container_logs",
		Description: "Fetch recent log lines from a specific container within a wireops stack.",
	}, getContainerLogs(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_jobs",
		Description: "List wireops scheduled (cron) jobs, with their status and recent runs.",
	}, listJobs(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_job_definition",
		Description: "Get the parsed job.yaml definition for a wireops scheduled job.",
	}, getJobDefinition(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_repo_commits",
		Description: "List the most recent commits on a wireops repository's tracked branch.",
	}, getRepoCommits(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_integration_actions",
		Description: "List available integration actions (e.g. Traefik/Dozzle links) for a wireops stack.",
	}, getStackIntegrationActions(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_workers",
		Description: "List wireops workers with their health, status, and host info.",
	}, listWorkers(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_worker_metrics",
		Description: "Get current resource metrics (CPU/memory/disk) for a wireops worker.",
	}, getWorkerMetrics(c))
}

func apiKeyFrom(ctx context.Context) (string, error) {
	apiKey, ok := auth.APIKeyFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("no wireops API key on this MCP session — pass one via the %s header when connecting", "X-Wireops-Api-Key")
	}
	return apiKey, nil
}

func listStacks(c *client.Client) mcp.ToolHandlerFor[models.ListStacksInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ListStacksInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 50
		}
		q := url.Values{"perPage": {fmt.Sprint(limit)}, "sort": {"-updated"}}
		var out any
		if err := c.Get(ctx, apiKey, "/api/collections/stacks/records", q, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getStackStatus(c *client.Client) mcp.ToolHandlerFor[models.StackIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/collections/stacks/records/" + url.PathEscape(in.StackID)
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getSyncLogs(c *client.Client) mcp.ToolHandlerFor[models.SyncLogsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.SyncLogsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		q := url.Values{
			"filter":  {fmt.Sprintf("stack='%s'", in.StackID)},
			"sort":    {"-created"},
			"perPage": {fmt.Sprint(limit)},
		}
		var out any
		if err := c.Get(ctx, apiKey, "/api/collections/sync_logs/records", q, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getStackServices(c *client.Client) mcp.ToolHandlerFor[models.StackIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/services"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getStackResources(c *client.Client) mcp.ToolHandlerFor[models.StackIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/resources"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getContainerLogs(c *client.Client) mcp.ToolHandlerFor[models.ContainerLogsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ContainerLogsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var q url.Values
		if in.Tail != "" {
			q = url.Values{"tail": {in.Tail}}
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/container/" + url.PathEscape(in.ContainerID) + "/logs"
		if err := c.Get(ctx, apiKey, path, q, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func listJobs(c *client.Client) mcp.ToolHandlerFor[models.ListJobsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ models.ListJobsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		if err := c.Get(ctx, apiKey, "/api/custom/jobs", nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getJobDefinition(c *client.Client) mcp.ToolHandlerFor[models.JobIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.JobIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/jobs/" + url.PathEscape(in.JobID) + "/definition"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getRepoCommits(c *client.Client) mcp.ToolHandlerFor[models.RepositoryIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.RepositoryIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/repositories/" + url.PathEscape(in.RepositoryID) + "/commits"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getStackIntegrationActions(c *client.Client) mcp.ToolHandlerFor[models.StackIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/integration-actions"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func listWorkers(c *client.Client) mcp.ToolHandlerFor[models.ListWorkersInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ models.ListWorkersInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		if err := c.Get(ctx, apiKey, "/api/custom/workers", nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getWorkerMetrics(c *client.Client) mcp.ToolHandlerFor[models.WorkerIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.WorkerIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/workers/" + url.PathEscape(in.WorkerID) + "/metrics"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}
