// Package tools registers the wireops MCP tool set. All tools are
// read-only proxies over the existing REST API — the MCP process has no
// write capability by construction, and the actual permission check is
// left entirely to the wireops server's RBAC on the caller's API key.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"github.com/wireops/wireops/internal/job"
	"github.com/wireops/wireops/internal/manifest"
	"github.com/wireops/wireops/internal/policy"
	"github.com/wireops/wireops/mcp/auth"
	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
	"github.com/wireops/wireops/mcp/utils"
)

// Register adds every wireops read-only tool to server, calling the
// wireops REST API at c on each invocation.
func Register(server *mcp.Server, c *client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_stacks",
		Description: "List all wireops stacks visible to the caller's API key, with their sync status. Records with active render-time overrides carry a _notice field pointing at get_stack_render_overrides. A status of \"paused\" means git sync/reconcile is disabled for that stack — it says nothing about whether the compose containers are running; check get_stack_services for that.",
	}, listStacks(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_status",
		Description: "Get a single wireops stack record by id, including its current sync/deploy status. If the stack has active render-time overrides, the response carries a _notice field pointing at get_stack_render_overrides. A status of \"paused\" means git sync/reconcile is disabled for that stack — it says nothing about whether the compose containers are running; check get_stack_services for that.",
	}, getStackStatus(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_sync_logs",
		Description: "List recent sync log entries for a wireops stack (most recent first).",
	}, getSyncLogs(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_services",
		Description: "List the running containers/services for a wireops stack. This reflects actual container/compose state, independent of the stack's sync status — a \"paused\" stack (git sync disabled) can still have running containers, and a stack that is actively syncing can have stopped containers.",
	}, getStackServices(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_resources",
		Description: "List the Docker volumes and networks associated with a wireops stack.",
	}, getStackResources(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_compose",
		Description: "Get the raw compose file content wireops currently considers 'current' for a stack — the rendered content of current_version if the stack has synced at least once, else the compose file read straight from the repo (or from the host for a locally-imported stack). For comparing two past revisions instead, use diff_stack_version.",
	}, getStackCompose(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_revision",
		Description: "Get the raw rendered docker-compose content of a single past revision of a wireops stack by version number. For comparing two revisions side by side, use diff_stack_version instead.",
	}, getStackRevision(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_container_stats",
		Description: "Get live CPU/memory/network/block-IO usage for a single container in a wireops stack. Requires the stack's worker to be online. Pairs with get_container_logs and get_stack_services (which lists the container ids to pass here).",
	}, getContainerStats(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_audit_logs",
		Description: "List wireops audit log entries (who did what, when, and the outcome) — covers actions like stack pause/resume/deploy, worker token issuance, settings changes, etc. Supports filtering by actor, action, resource, origin, status, and time range. Most recent first.",
	}, listAuditLogs(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_render_overrides",
		Description: "Get the render-time overrides (image/ports/networks) currently active on a wireops stack, plus what those same services resolve to from Git alone, for comparison. Render overrides are ephemeral (not committed to git) and are reapplied automatically on every reconcile until cleared. Empty if the stack has no active overrides.",
	}, getStackRenderOverrides(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "diff_stack_version",
		Description: "Get the raw rendered docker-compose content of two past revisions of a wireops stack (e.g. version 33 vs 34), for comparison before deciding on a rollback. Returns both revisions' content as-is — does not compute a diff itself, the caller compares them. Revision numbers can be found via the stack's stack_revisions collection records (sorted by version).",
	}, diffStackVersion(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_secrets",
		Description: "List which secret env vars are configured for a wireops stack — key names and provider only, never values, regardless of whether the secret is stored internally or via an external provider (Vault/Infisical/SOPS). Covers stack-level env vars marked secret, secret global env vars bound to the stack, and SOPS-managed keys from the repo's secrets file. Use this to check whether a secret exists/is configured, not to read it.",
	}, listSecrets(c))

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
		Name:        "list_repos",
		Description: "List wireops repositories (name, git_url, branch, status, last_commit_sha) visible to the caller's API key. Never includes credentials — SSH keys and git passwords live in a separate collection this tool does not touch.",
	}, listRepos(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_repo_commits",
		Description: "List the most recent commits on a wireops repository's tracked branch.",
	}, getRepoCommits(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_repo_stack_files",
		Description: "List compose file paths (docker-compose.yml/yaml, compose.yml/yaml) found in a wireops repository — useful for finding candidate compose files before scaffolding or importing a stack. Triggers a git fetch of the repository first, so it reflects the latest commit on the tracked branch.",
	}, listRepoStackFiles(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_repo_job_files",
		Description: "List job.yaml file paths found in a wireops repository — useful for finding candidate job definitions before scheduling a job. Triggers a git fetch of the repository first, so it reflects the latest commit on the tracked branch.",
	}, listRepoJobFiles(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_integration_actions",
		Description: "List available integration actions (e.g. Traefik/Dozzle links) for a wireops stack.",
	}, getStackIntegrationActions(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_workers",
		Description: "List wireops workers, including their live status (ACTIVE/OFFLINE, computed against the current connection — not just the stored DB flag), last_seen heartbeat timestamp, and docker_online, plus token and host info. This already covers worker online/offline diagnosis — there is no separate worker-status tool.",
	}, listWorkers(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_worker_metrics",
		Description: "Get current resource metrics (CPU/memory/disk) for a wireops worker.",
	}, getWorkerMetrics(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_integrations",
		Description: "List all wireops integration plugins (Traefik, Dozzle, notification providers, etc.), whether each is enabled, and its (secret-masked) config. Requires manage-settings capability on the caller's API key. For per-stack resolved container actions instead, use get_stack_integration_actions.",
	}, listIntegrations(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_orphans",
		Description: "List directories in the repo workspace that aren't tracked by any wireops repository record — leftovers from deleted repos, failed imports, etc. Requires manage-settings capability on the caller's API key.",
	}, listOrphans(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_system_info",
		Description: "Get wireops server build/version info and repo workspace disk usage. Requires manage-settings capability on the caller's API key.",
	}, getSystemInfo(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "generate_wireops_yaml",
		Description: "Generate a valid wireops.yaml file from structured fields, ready to commit to a repository. Does not create or modify any stack — the caller commits the returned content itself.",
	}, generateWireopsYAML())

	mcp.AddTool(server, &mcp.Tool{
		Name:        "generate_job_yaml",
		Description: "Generate a valid job.yaml file from structured fields, ready to commit to a repository. Does not create or schedule any job — the caller commits the returned content itself.",
	}, generateJobYAML())

	mcp.AddTool(server, &mcp.Tool{
		Name:        "scaffold_stack",
		Description: "Generate a docker-compose.yml and matching wireops.yaml for a new stack from structured service definitions, ready to commit to a repository. If worker_id is given, validates the compose file against that worker's effective deploy security policy and reports violations instead of silently returning a file that would be rejected at deploy time. Does not create or import any stack. Before calling this tool, look up the current image tag, ports, volumes, and required environment variables for each service using a web search against the application's official site, official image registry page, or official GitHub repository — do not rely on memorized/training-data knowledge, which may be outdated.",
	}, scaffoldStack(c))
}

func apiKeyFrom(ctx context.Context) (string, error) {
	apiKey, ok := auth.APIKeyFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("no wireops API key on this MCP session — pass one via the %s header when connecting", "X-Wireops-Api-Key")
	}
	return apiKey, nil
}

// renderOverridesNotice is injected into a stack record's response so the
// agent can't miss that render_overrides (ephemeral, not committed to git,
// reapplied on every reconcile) is active without having to notice an
// otherwise-opaque JSON field on its own.
const renderOverridesNotice = "This stack has active render-time overrides (render_overrides) that differ from what's committed in Git and are reapplied on every reconcile. Call get_stack_render_overrides for the resolved details."

// hasRenderOverrides reports whether a decoded stacks collection record has
// a non-empty render_overrides field.
func hasRenderOverrides(rec map[string]interface{}) bool {
	overrides, ok := rec["render_overrides"].(map[string]interface{})
	return ok && len(overrides) > 0
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
		var out map[string]interface{}
		if err := c.Get(ctx, apiKey, "/api/collections/stacks/records", q, &out); err != nil {
			return nil, nil, err
		}
		if items, ok := out["items"].([]interface{}); ok {
			for _, item := range items {
				if rec, ok := item.(map[string]interface{}); ok && hasRenderOverrides(rec) {
					rec["_notice"] = renderOverridesNotice
				}
			}
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
		var out map[string]interface{}
		path := "/api/collections/stacks/records/" + url.PathEscape(in.StackID)
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		if hasRenderOverrides(out) {
			out["_notice"] = renderOverridesNotice
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
		var out []interface{}
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/services"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, map[string]interface{}{"services": out}, nil
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

func getStackCompose(c *client.Client) mcp.ToolHandlerFor[models.StackIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/compose"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getStackRevision(c *client.Client) mcp.ToolHandlerFor[models.StackRevisionInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackRevisionInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/revisions/" + fmt.Sprint(in.Version)
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getContainerStats(c *client.Client) mcp.ToolHandlerFor[models.ContainerStatsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ContainerStatsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/container/" + url.PathEscape(in.ContainerID) + "/stats"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func listAuditLogs(c *client.Client) mcp.ToolHandlerFor[models.AuditLogsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.AuditLogsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		page := in.Page
		if page <= 0 {
			page = 1
		}
		perPage := in.PerPage
		if perPage <= 0 {
			perPage = 25
		}
		q := url.Values{"page": {fmt.Sprint(page)}, "perPage": {fmt.Sprint(perPage)}}
		setIfNonEmpty := func(key, val string) {
			if val != "" {
				q.Set(key, val)
			}
		}
		setIfNonEmpty("from", in.From)
		setIfNonEmpty("to", in.To)
		setIfNonEmpty("actor_type", in.ActorType)
		setIfNonEmpty("actor_id", in.ActorID)
		setIfNonEmpty("action", in.Action)
		setIfNonEmpty("resource_type", in.ResourceType)
		setIfNonEmpty("resource_id", in.ResourceID)
		setIfNonEmpty("origin", in.Origin)
		setIfNonEmpty("status", in.Status)
		var out any
		if err := c.Get(ctx, apiKey, "/api/custom/audit-logs", q, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getStackRenderOverrides(c *client.Client) mcp.ToolHandlerFor[models.StackIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/render-overrides"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func diffStackVersion(c *client.Client) mcp.ToolHandlerFor[models.DiffStackVersionInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.DiffStackVersionInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var a, b any
		pathA := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/revisions/" + fmt.Sprint(in.VersionA)
		pathB := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/revisions/" + fmt.Sprint(in.VersionB)
		if err := c.Get(ctx, apiKey, pathA, nil, &a); err != nil {
			return nil, nil, fmt.Errorf("fetching version %d: %w", in.VersionA, err)
		}
		if err := c.Get(ctx, apiKey, pathB, nil, &b); err != nil {
			return nil, nil, fmt.Errorf("fetching version %d: %w", in.VersionB, err)
		}
		return nil, map[string]any{"version_a": a, "version_b": b}, nil
	}
}

// secretSummary is the masked shape returned by list_secrets — a key and
// where it's sourced from, deliberately never a value.
type secretSummary struct {
	Key            string `json:"key"`
	SecretProvider string `json:"secret_provider,omitempty"`
}

// fetchStackEnvVarSecrets lists a stack's own secret env vars. The "fields"
// query param explicitly excludes "value" from the response on the wire —
// belt-and-suspenders on top of the server's OnRecordEnrich masking, which
// only blanks the value for the internal secret provider and leaves
// external-provider reference strings (Vault/Infisical paths) unmasked.
func fetchStackEnvVarSecrets(ctx context.Context, c *client.Client, apiKey, stackID string) ([]secretSummary, error) {
	var out struct {
		Items []struct {
			Key            string `json:"key"`
			SecretProvider string `json:"secret_provider"`
		} `json:"items"`
	}
	q := url.Values{
		"filter":  {fmt.Sprintf("stack='%s' && secret=true", client.EscapeFilterValue(stackID))},
		"perPage": {"200"},
		"fields":  {"key,secret_provider"},
	}
	if err := c.Get(ctx, apiKey, "/api/collections/stack_env_vars/records", q, &out); err != nil {
		return nil, fmt.Errorf("fetching stack env var secrets: %w", err)
	}
	result := make([]secretSummary, 0, len(out.Items))
	for _, item := range out.Items {
		result = append(result, secretSummary{Key: item.Key, SecretProvider: item.SecretProvider})
	}
	return result, nil
}

// fetchStackGlobalEnvVarSecrets lists secret global env vars bound to the
// stack via the stack_global_env_vars join collection. Same "fields"
// restriction as fetchStackEnvVarSecrets, scoped through the expanded
// relation so the joined global_env_var's value never reaches the response.
func fetchStackGlobalEnvVarSecrets(ctx context.Context, c *client.Client, apiKey, stackID string) ([]secretSummary, error) {
	var out struct {
		Items []struct {
			Expand struct {
				GlobalEnvVar struct {
					Key            string `json:"key"`
					Secret         bool   `json:"secret"`
					SecretProvider string `json:"secret_provider"`
				} `json:"global_env_var"`
			} `json:"expand"`
		} `json:"items"`
	}
	q := url.Values{
		"filter":  {fmt.Sprintf("stack='%s'", client.EscapeFilterValue(stackID))},
		"expand":  {"global_env_var"},
		"perPage": {"200"},
		"fields":  {"expand.global_env_var.key,expand.global_env_var.secret,expand.global_env_var.secret_provider"},
	}
	if err := c.Get(ctx, apiKey, "/api/collections/stack_global_env_vars/records", q, &out); err != nil {
		return nil, fmt.Errorf("fetching global env var secrets: %w", err)
	}
	result := make([]secretSummary, 0, len(out.Items))
	for _, item := range out.Items {
		if item.Expand.GlobalEnvVar.Secret {
			result = append(result, secretSummary{Key: item.Expand.GlobalEnvVar.Key, SecretProvider: item.Expand.GlobalEnvVar.SecretProvider})
		}
	}
	return result, nil
}

func listSecrets(c *client.Client) mcp.ToolHandlerFor[models.StackIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}

		stackSecrets, err := fetchStackEnvVarSecrets(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		globalSecrets, err := fetchStackGlobalEnvVarSecrets(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		var sopsSecrets any
		sopsPath := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/sops-env-vars"
		if err := c.Get(ctx, apiKey, sopsPath, nil, &sopsSecrets); err != nil {
			return nil, nil, fmt.Errorf("fetching sops env var secrets: %w", err)
		}

		return nil, map[string]any{
			"stack_secrets":  stackSecrets,
			"global_secrets": globalSecrets,
			"sops_secrets":   sopsSecrets,
		}, nil
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
		var out []interface{}
		if err := c.Get(ctx, apiKey, "/api/custom/jobs", nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, map[string]interface{}{"jobs": out}, nil
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

func listRepos(c *client.Client) mcp.ToolHandlerFor[models.ListReposInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ListReposInput) (*mcp.CallToolResult, any, error) {
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
		if err := c.Get(ctx, apiKey, "/api/collections/repositories/records", q, &out); err != nil {
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
		var out []interface{}
		path := "/api/custom/repositories/" + url.PathEscape(in.RepositoryID) + "/commits"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, map[string]interface{}{"commits": out}, nil
	}
}

func listRepoStackFiles(c *client.Client) mcp.ToolHandlerFor[models.RepositoryIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.RepositoryIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out []interface{}
		path := "/api/custom/repositories/" + url.PathEscape(in.RepositoryID) + "/stack-files"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, map[string]interface{}{"files": out}, nil
	}
}

func listRepoJobFiles(c *client.Client) mcp.ToolHandlerFor[models.RepositoryIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.RepositoryIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out []interface{}
		path := "/api/custom/repositories/" + url.PathEscape(in.RepositoryID) + "/job-files"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, map[string]interface{}{"files": out}, nil
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
		var out []interface{}
		if err := c.Get(ctx, apiKey, "/api/custom/workers", nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, map[string]interface{}{"workers": out}, nil
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

func listIntegrations(c *client.Client) mcp.ToolHandlerFor[models.ListIntegrationsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ models.ListIntegrationsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out []interface{}
		if err := c.Get(ctx, apiKey, "/api/custom/integrations", nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, map[string]interface{}{"integrations": out}, nil
	}
}

func listOrphans(c *client.Client) mcp.ToolHandlerFor[models.ListOrphansInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ models.ListOrphansInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out []interface{}
		if err := c.Get(ctx, apiKey, "/api/custom/orphans", nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, map[string]interface{}{"orphans": out}, nil
	}
}

func getSystemInfo(c *client.Client) mcp.ToolHandlerFor[models.GetSystemInfoInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ models.GetSystemInfoInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		if err := c.Get(ctx, apiKey, "/api/custom/system/info", nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

// generatedFile is the {filename, content} shape returned by the
// file-scaffolding tools below.
type generatedFile struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// wireopsDefinitionFrom builds an internal/manifest.Definition from the
// tool's flat input fields, defaulting name to fallbackName when the input's
// own Name is empty (used by scaffold_stack, where the wireops name is
// optional and defaults to the stack name).
func wireopsDefinitionFrom(in models.GenerateWireopsYAMLInput, fallbackName string) *manifest.Definition {
	name := in.Name
	if name == "" {
		name = fallbackName
	}
	def := &manifest.Definition{
		Version: "wireops.v1",
		Name:    name,
		Timeout: in.Timeout,
	}
	if in.RemoveOrphans != nil || in.ForcePull != nil {
		def.Compose = &manifest.ComposeConfig{RemoveOrphans: in.RemoveOrphans, ForcePull: in.ForcePull}
	}
	if in.WaitRunningJobs != nil {
		def.Jobs = &manifest.JobsConfig{WaitRunning: in.WaitRunningJobs}
	}
	if len(in.WorkerTags) > 0 {
		def.Worker = &manifest.WorkerConfig{Tags: in.WorkerTags}
	}
	if in.SyncIntervalGo != "" {
		def.Sync = &manifest.SyncConfig{Interval: in.SyncIntervalGo}
	}
	return def
}

func generateWireopsYAML() mcp.ToolHandlerFor[models.GenerateWireopsYAMLInput, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, in models.GenerateWireopsYAMLInput) (*mcp.CallToolResult, any, error) {
		def := wireopsDefinitionFrom(in, "")
		if err := def.Validate(); err != nil {
			return nil, nil, err
		}
		content, err := yaml.Marshal(def)
		if err != nil {
			return nil, nil, fmt.Errorf("marshaling wireops.yaml: %w", err)
		}
		return nil, generatedFile{Filename: "wireops.yaml", Content: string(content)}, nil
	}
}

func jobDefinitionFrom(in models.GenerateJobYAMLInput) *job.Definition {
	return &job.Definition{
		Name:        in.Name,
		Description: in.Description,
		Cron:        in.Cron,
		Tags:        in.Tags,
		Mode:        job.Mode(in.Mode),
		Image:       in.Image,
		Command:     job.Command(in.Command),
		Volumes:     in.Volumes,
		Network:     in.Network,
		Resources: job.Resources{
			CPU:     in.CPU,
			Memory:  in.Memory,
			Timeout: in.ResTimeout,
		},
	}
}

func generateJobYAML() mcp.ToolHandlerFor[models.GenerateJobYAMLInput, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, in models.GenerateJobYAMLInput) (*mcp.CallToolResult, any, error) {
		def := jobDefinitionFrom(in)
		if err := def.Validate(); err != nil {
			return nil, nil, err
		}
		if def.Mode == "" {
			def.Mode = job.ModeOnce
		}
		content, err := yaml.Marshal(def)
		if err != nil {
			return nil, nil, fmt.Errorf("marshaling job.yaml: %w", err)
		}
		return nil, generatedFile{Filename: "job.yaml", Content: string(content)}, nil
	}
}

// composeConfigFrom builds a docker-compose config map (in the shape
// internal/policy.ValidateComposeConfig expects) from the tool's structured
// service inputs.
func composeConfigFrom(services []models.ComposeServiceInput) (map[string]interface{}, error) {
	if len(services) == 0 {
		return nil, fmt.Errorf("at least one service is required")
	}
	svcMap := map[string]interface{}{}
	namedVolumes := map[string]struct{}{}
	networks := map[string]struct{}{}
	for _, svc := range services {
		if svc.Name == "" {
			return nil, fmt.Errorf("every service requires a name")
		}
		if svc.Image == "" {
			return nil, fmt.Errorf("service %q requires an image", svc.Name)
		}
		entry := map[string]interface{}{"image": svc.Image}
		if len(svc.Command) > 0 {
			entry["command"] = svc.Command
		}
		if len(svc.Environment) > 0 {
			entry["environment"] = svc.Environment
		}
		if len(svc.Ports) > 0 {
			entry["ports"] = svc.Ports
		}
		if len(svc.Volumes) > 0 {
			entry["volumes"] = utils.ToInterfaceSlice(svc.Volumes)
			for _, v := range svc.Volumes {
				if src := utils.VolumeSource(v); src != "" && !utils.IsHostPath(src) {
					namedVolumes[src] = struct{}{}
				}
			}
		}
		if len(svc.Networks) > 0 {
			entry["networks"] = utils.ToInterfaceSlice(svc.Networks)
			for _, n := range svc.Networks {
				networks[n] = struct{}{}
			}
		}
		if len(svc.DependsOn) > 0 {
			entry["depends_on"] = svc.DependsOn
		}
		svcMap[svc.Name] = entry
	}

	config := map[string]interface{}{"services": svcMap}
	if len(namedVolumes) > 0 {
		topVolumes := map[string]interface{}{}
		for name := range namedVolumes {
			topVolumes[name] = map[string]interface{}{}
		}
		config["volumes"] = topVolumes
	}
	if len(networks) > 0 {
		topNetworks := map[string]interface{}{}
		for name := range networks {
			topNetworks[name] = map[string]interface{}{}
		}
		config["networks"] = topNetworks
	}
	return config, nil
}

// workerPolicyFromEffective converts the "effective" PolicyJSON object
// embedded in a GET /api/custom/workers/{id}/policy response into a
// policy.WorkerPolicy usable with ValidateComposeConfig.
func workerPolicyFromEffective(pj policy.PolicyJSON) *policy.WorkerPolicy {
	return &policy.WorkerPolicy{
		Disabled:             !pj.Enabled,
		AllowedVolumes:       pj.AllowedVolumes,
		AllowedNetworks:      pj.AllowedNetworks,
		AllowedImages:        pj.AllowedImages,
		AllowedCapAdd:        pj.AllowedCapAdd,
		AllowedDevices:       pj.AllowedDevices,
		AllowedSecurityOpt:   pj.AllowedSecurityOpt,
		PreventLatestImages:  pj.PreventLatestImages,
		BlockHostVolumes:     pj.BlockHostVolumes,
		BlockPrivileged:      pj.BlockPrivileged,
		BlockHostNetwork:     pj.BlockHostNetwork,
		BlockHostPID:         pj.BlockHostPID,
		BlockHostIPC:         pj.BlockHostIPC,
		BlockDockerSocket:    pj.BlockDockerSocket,
		AllowRenderOverrides: pj.AllowRenderOverrides,
	}
}

func fetchEffectiveWorkerPolicy(ctx context.Context, c *client.Client, apiKey, workerID string) (*policy.WorkerPolicy, error) {
	var raw map[string]json.RawMessage
	path := "/api/custom/workers/" + url.PathEscape(workerID) + "/policy"
	if err := c.Get(ctx, apiKey, path, nil, &raw); err != nil {
		return nil, fmt.Errorf("fetching worker policy: %w", err)
	}
	effRaw, ok := raw["effective"]
	if !ok {
		return nil, fmt.Errorf("worker policy response missing 'effective' field")
	}
	var pj policy.PolicyJSON
	if err := json.Unmarshal(effRaw, &pj); err != nil {
		return nil, fmt.Errorf("decoding effective worker policy: %w", err)
	}
	return workerPolicyFromEffective(pj), nil
}

// scaffoldStackOutput is the result of the scaffold_stack tool.
type scaffoldStackOutput struct {
	Wireops generatedFile `json:"wireops"`
	Compose generatedFile `json:"compose"`
}

func scaffoldStack(c *client.Client) mcp.ToolHandlerFor[models.ScaffoldStackInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ScaffoldStackInput) (*mcp.CallToolResult, any, error) {
		if in.Name == "" {
			return nil, nil, fmt.Errorf("name is required")
		}

		wireopsDef := wireopsDefinitionFrom(in.Wireops, in.Name)
		if err := wireopsDef.Validate(); err != nil {
			return nil, nil, err
		}
		wireopsContent, err := yaml.Marshal(wireopsDef)
		if err != nil {
			return nil, nil, fmt.Errorf("marshaling wireops.yaml: %w", err)
		}

		composeMap, err := composeConfigFrom(in.Services)
		if err != nil {
			return nil, nil, err
		}

		if in.WorkerID != "" {
			apiKey, err := apiKeyFrom(ctx)
			if err != nil {
				return nil, nil, err
			}
			workerPolicy, err := fetchEffectiveWorkerPolicy(ctx, c, apiKey, in.WorkerID)
			if err != nil {
				return nil, nil, err
			}
			if err := workerPolicy.ValidateComposeConfig(composeMap); err != nil {
				return nil, nil, fmt.Errorf("generated compose file violates worker %q's deploy security policy: %w", in.WorkerID, err)
			}
		}

		composeContent, err := yaml.Marshal(composeMap)
		if err != nil {
			return nil, nil, fmt.Errorf("marshaling docker-compose.yml: %w", err)
		}

		return nil, scaffoldStackOutput{
			Wireops: generatedFile{Filename: "wireops.yaml", Content: string(wireopsContent)},
			Compose: generatedFile{Filename: "docker-compose.yml", Content: string(composeContent)},
		}, nil
	}
}
