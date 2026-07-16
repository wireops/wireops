// Package resources registers the wireops MCP resource templates —
// URI-addressable read-only content, discoverable via resources/list and
// resources/templates/list without a dedicated listing tool. Like
// mcp/tools, every handler proxies to the existing REST API using the
// caller's pass-through API key from context.
package resources

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wireops/wireops/mcp/auth"
	"github.com/wireops/wireops/mcp/client"
)

const (
	stackComposeURITemplate  = "wireops://stacks/{id}/compose"
	jobRawURITemplate        = "wireops://jobs/{id}/raw"
	stackLiveLogsURITemplate = "wireops://stacks/{id}/logs/live"
)

// Register adds every wireops resource template to server, calling the
// wireops REST API at c on each read.
func Register(server *mcp.Server, c *client.Client) {
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "stack_compose",
		Description: "The rendered docker-compose file for a wireops stack.",
		URITemplate: stackComposeURITemplate,
		MIMEType:    "text/plain",
	}, readStackCompose(c))

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "job_raw",
		Description: "The raw job.yaml file backing a wireops scheduled job.",
		URITemplate: jobRawURITemplate,
		MIMEType:    "text/plain",
	}, readJobRaw(c))

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		Name:        "stack_logs_live",
		Description: "The most recent sync_logs output for a wireops stack. Supports resources/subscribe for live updates as new sync output is written.",
		URITemplate: stackLiveLogsURITemplate,
		MIMEType:    "text/plain",
	}, readStackLiveLogs(c))
}

// rawFileResponse matches the {"content", "filename"} shape returned by
// GET /api/custom/stacks/{id}/compose and GET /api/custom/jobs/{id}/raw.
type rawFileResponse struct {
	Content  string `json:"content"`
	Filename string `json:"filename"`
}

func apiKeyFrom(ctx context.Context) (string, error) {
	apiKey, ok := auth.APIKeyFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("no wireops API key on this MCP session — pass one via the %s header when connecting", "X-Wireops-Api-Key")
	}
	return apiKey, nil
}

// extractID pulls the {id} path segment out of a resource URI matching
// prefix + "{id}" + suffix. The SDK does not bind template variables for
// us — see mcp/resource.go ResourceHandler docs — so callers must do it.
func extractID(uri, prefix, suffix string) (string, bool) {
	if !strings.HasPrefix(uri, prefix) || !strings.HasSuffix(uri, suffix) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(uri, prefix), suffix)
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func readStackCompose(c *client.Client) mcp.ResourceHandler {
	prefix, suffix, _ := strings.Cut(stackComposeURITemplate, "{id}")
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		id, ok := extractID(req.Params.URI, prefix, suffix)
		if !ok {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, err
		}
		var out rawFileResponse
		path := "/api/custom/stacks/" + id + "/compose"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: "text/plain", Text: out.Content}},
		}, nil
	}
}

func readJobRaw(c *client.Client) mcp.ResourceHandler {
	prefix, suffix, _ := strings.Cut(jobRawURITemplate, "{id}")
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		id, ok := extractID(req.Params.URI, prefix, suffix)
		if !ok {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, err
		}
		var out rawFileResponse
		path := "/api/custom/jobs/" + id + "/raw"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: "text/plain", Text: out.Content}},
		}, nil
	}
}

// syncLogRecord is the subset of a sync_logs collection record we need.
type syncLogRecord struct {
	Output string `json:"output"`
	Status string `json:"status"`
}

type syncLogsListResponse struct {
	Items []syncLogRecord `json:"items"`
}

func readStackLiveLogs(c *client.Client) mcp.ResourceHandler {
	prefix, suffix, _ := strings.Cut(stackLiveLogsURITemplate, "{id}")
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		id, ok := extractID(req.Params.URI, prefix, suffix)
		if !ok {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, err
		}
		text, err := latestSyncLogOutput(ctx, c, apiKey, id)
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{URI: req.Params.URI, MIMEType: "text/plain", Text: text}},
		}, nil
	}
}

func latestSyncLogOutput(ctx context.Context, c *client.Client, apiKey, stackID string) (string, error) {
	var out syncLogsListResponse
	q := url.Values{
		"filter":  {fmt.Sprintf("stack='%s'", stackID)},
		"sort":    {"-created"},
		"perPage": {"1"},
	}
	if err := c.Get(ctx, apiKey, "/api/collections/sync_logs/records", q, &out); err != nil {
		return "", err
	}
	if len(out.Items) == 0 {
		return "", nil
	}
	return out.Items[0].Output, nil
}
