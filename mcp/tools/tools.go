// Package tools registers the wireops MCP tool set: read tools plus the
// write tools an operator-scoped API key can use, all of them thin proxies
// over the existing REST API. The MCP process makes no authorization
// decision of its own — every call carries the caller's own API key and the
// wireops server's RBAC decides, so a viewer-scoped key gets a 403 back for
// everything in the write half. On top of that, the tools whose effect
// cannot be undone require the caller to echo the resource's name back in a
// confirm field (see confirm.go), and every tool is annotated with its
// read-only/destructive hints so clients know what to prompt a human about.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

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

// ToolSet selects which groups of tools Register adds. It is a deployment
// choice, not an authorization one — the server's RBAC still decides every
// call, and a key that cannot write already gets a 403. What it buys is
// context: the full set is ~60 tools whose schemas and descriptions every
// session pays for before any work starts, and an instance used only for
// diagnosis has no use for the write half.
type ToolSet struct {
	Read  bool
	Write bool
	Exec  bool
}

// AllTools is the default: every group.
func AllTools() ToolSet {
	return ToolSet{Read: true, Write: true, Exec: true}
}

// ParseToolSet reads a comma-separated group list ("read", "write", "exec",
// or "all"). An empty value means all of them. An unknown group is an error
// rather than a silent omission: a typo that quietly produced a read-only
// server would be found only when a write failed.
func ParseToolSet(raw string) (ToolSet, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AllTools(), nil
	}
	var set ToolSet
	for _, group := range strings.Split(raw, ",") {
		switch strings.ToLower(strings.TrimSpace(group)) {
		case "":
			continue
		case "all":
			set = AllTools()
		case "read":
			set.Read = true
		case "write":
			set.Write = true
		case "exec":
			set.Exec = true
		default:
			return ToolSet{}, fmt.Errorf("unknown tool group %q — valid groups are read, write, exec, all", strings.TrimSpace(group))
		}
	}
	if !set.Read && !set.Write && !set.Exec {
		return ToolSet{}, fmt.Errorf("no tool groups selected")
	}
	return set, nil
}

// Register adds the selected wireops tools to server, calling the wireops
// REST API at c on each invocation. Within a selected group the write tools
// are registered unconditionally: gating them on the caller's role here would
// be a second, weaker copy of the server's RBAC, and a viewer-scoped key
// already cannot use them.
func Register(server *mcp.Server, c *client.Client, set ToolSet) {
	if set.Read {
		registerReadTools(server, c)
		registerStackReadTools(server, c)
		registerRepoReadTools(server, c)
		registerJobReadTools(server, c)
	}
	if set.Write {
		registerStackWriteTools(server, c)
		registerRepoWriteTools(server, c)
		registerJobWriteTools(server, c)
		registerWorkerWriteTools(server, c)
	}
	if set.Exec {
		registerExecTools(server, c)
	}
}

// registerReadTools adds the tools that never change wireops state, plus the
// pure local generators (generate_*/scaffold_stack).
func registerReadTools(server *mcp.Server, c *client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "whoami",
		Description: "Report what the caller's own wireops API key is: its role (viewer or operator), the capabilities that role grants, the service account it belongs to, and the server's version plus its default git poll interval (scan_period_seconds, which is what a stack's sync_interval_seconds=0 resolves to). Call this before concluding that something is broken when a tool returns a permission error — a viewer-scoped key cannot deploy, and no API key can hold an admin capability at all.",
		Annotations: readOnly(),
	}, whoami(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_stacks",
		Description: "List the wireops stacks visible to the caller's API key, newest first, with their sync status and deployed version. Filterable by status, name substring, repository and worker, and paged; returns a compact projection by default (pass fields [\"*\"] for whole records). Relations come back expanded, so the repository name and worker hostname are in the response rather than two more ids to look up. A _notice on a record explains anything about it that the fields alone do not.",
		Annotations: readOnly(),
	}, listStacks(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_status",
		Description: "Get one wireops stack record, including its current sync/deploy status. A _notice in the response explains the status when it is one that is easy to misread (paused, degraded) and flags active render-time overrides.",
		Annotations: readOnly(),
	}, getStackStatus(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_sync_logs",
		Description: "List recent sync log entries for a wireops stack (most recent first).",
		Annotations: readOnly(),
	}, getSyncLogs(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_services",
		Description: "List the containers/services of a wireops stack with their live state, which is independent of the stack's sync status. Each entry carries is_init and exit_code; a _notice in the response explains those two whenever the stack actually has a case where they matter.",
		Annotations: readOnly(),
	}, getStackServices(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_resources",
		Description: "List the Docker volumes and networks associated with a wireops stack.",
		Annotations: readOnly(),
	}, getStackResources(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_compose",
		Description: "Get the raw compose file content wireops currently considers 'current' for a stack — the rendered content of current_version if the stack has synced at least once, else the compose file read straight from the repo (or from the host for a locally-imported stack). For comparing two past revisions instead, use diff_stack_version.",
		Annotations: readOnly(),
	}, getStackCompose(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_revision",
		Description: "Get the raw rendered docker-compose content of a single past revision of a wireops stack by version number. For comparing two revisions side by side, use diff_stack_version instead.",
		Annotations: readOnly(),
	}, getStackRevision(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_container_stats",
		Description: "Get live CPU/memory/network/block-IO usage for a single container in a wireops stack. Requires the stack's worker to be online. Pairs with get_container_logs and get_stack_services (which lists the container ids to pass here).",
		Annotations: readOnly(),
	}, getContainerStats(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_render_overrides",
		Description: "Get the render-time overrides (image/ports/networks/scale) currently active on a wireops stack, plus what those same services resolve to from Git alone, for comparison. Render overrides are ephemeral (not committed to git) and are reapplied automatically on every reconcile until cleared. Empty if the stack has no active overrides.",
		Annotations: readOnly(),
	}, getStackRenderOverrides(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "diff_stack_version",
		Description: "Get the raw rendered docker-compose content of two past revisions of a wireops stack (e.g. version 33 vs 34), for comparison before deciding on a rollback. Returns both revisions' content as-is — does not compute a diff itself, the caller compares them. Revision numbers can be found via the stack's stack_revisions collection records (sorted by version).",
		Annotations: readOnly(),
	}, diffStackVersion(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_secrets",
		Description: "List which secret env vars are configured for a wireops stack — key names and provider only, never values, regardless of whether the secret is stored internally or via an external provider (Vault/Infisical/SOPS). Covers stack-level env vars marked secret, secret global env vars bound to the stack, and SOPS-managed keys from the repo's secrets file. Use this to check whether a secret exists/is configured, not to read it.",
		Annotations: readOnly(),
	}, listSecrets(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_container_logs",
		Description: "Fetch recent log lines from a specific container within a wireops stack. Identify the container with container_id or service_name (either works — service_name is resolved internally, saving a round trip through get_stack_services). Use pattern to filter to lines matching a regex, which cuts noise in crash-loop logs.",
		Annotations: readOnly(),
	}, getContainerLogs(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_jobs",
		Description: "List wireops scheduled (cron) jobs, with their status and recent runs.",
		Annotations: readOnly(),
	}, listJobs(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_job_definition",
		Description: "Get the parsed job.yaml definition for a wireops scheduled job.",
		Annotations: readOnly(),
	}, getJobDefinition(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_repos",
		Description: "List wireops repositories (name, git_url, branch, status, last_commit_sha) visible to the caller's API key. Never includes credentials — SSH keys and git passwords live in a separate collection this tool does not touch.",
		Annotations: readOnly(),
	}, listRepos(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_repo_commits",
		Description: "List the most recent commits on a wireops repository's tracked branch.",
		Annotations: readOnly(),
	}, getRepoCommits(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_repo_stack_files",
		Description: "List compose file paths (docker-compose.yml/yaml, compose.yml/yaml) found in a wireops repository — useful for finding candidate compose files before scaffolding or importing a stack. Triggers a git fetch of the repository first, so it reflects the latest commit on the tracked branch.",
		Annotations: readOnly(),
	}, listRepoStackFiles(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_repo_job_files",
		Description: "List job.yaml file paths found in a wireops repository — useful for finding candidate job definitions before scheduling a job. Triggers a git fetch of the repository first, so it reflects the latest commit on the tracked branch.",
		Annotations: readOnly(),
	}, listRepoJobFiles(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stack_integration_actions",
		Description: "List available integration actions (e.g. Traefik/Dozzle links) for a wireops stack.",
		Annotations: readOnly(),
	}, getStackIntegrationActions(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_workers",
		Description: "List wireops workers, including their live status (ACTIVE/OFFLINE, computed against the current connection — not just the stored DB flag), last_seen heartbeat timestamp, and docker_online, plus token and host info. This already covers worker online/offline diagnosis — there is no separate worker-status tool.",
		Annotations: readOnly(),
	}, listWorkers(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_worker_metrics",
		Description: "Get current resource metrics (CPU/memory/disk) for a wireops worker.",
		Annotations: readOnly(),
	}, getWorkerMetrics(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_worker_policy",
		Description: "Get a wireops worker's deploy security policy: its local override fields, whether it inherits the global policy (inherit=true), and the fully resolved 'effective' policy actually enforced on deploy (allowed images/volumes/networks/capabilities/devices, and the block_* flags for privileged/host-network/host-PID/host-IPC/docker-socket). Use this to explain why a compose file would be accepted or rejected by this worker, or before calling scaffold_stack with worker_id. Requires manage-settings capability on the caller's API key.",
		Annotations: readOnly(),
	}, getWorkerPolicy(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_global_worker_policy",
		Description: "Get the instance-wide default deploy security policy — the same shape as get_worker_policy's 'effective' field, but the fallback every worker inherits from unless it sets its own local override (inherit=false in get_worker_policy). Requires manage-settings capability on the caller's API key.",
		Annotations: readOnly(),
	}, getGlobalWorkerPolicy(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "generate_wireops_yaml",
		Description: "DEPRECATED: Generate a valid standalone wireops.yaml file from structured fields, ready to commit to a repository. The standalone two-file layout is deprecated — for a new stack, use scaffold_stack instead, which by default embeds an equivalent 'x-wireops' block directly in the compose file (no separate wireops.yaml needed). Use this tool directly only to add/edit a standalone wireops.yaml for a repository that already uses the deprecated two-file layout. Does not create or modify any stack — the caller commits the returned content itself.",
		Annotations: readOnly(),
	}, generateWireopsYAML())

	mcp.AddTool(server, &mcp.Tool{
		Name:        "generate_job_yaml",
		Description: "Generate a valid job.yaml file from structured fields, ready to commit to a repository. Does not create or schedule any job — the caller commits the returned content itself.",
		Annotations: readOnly(),
	}, generateJobYAML())

	mcp.AddTool(server, &mcp.Tool{
		Name:        "scaffold_stack",
		Description: "Generate a docker-compose.yml for a new stack from structured service definitions — the recommended way to define a wireops stack. By default the wireops settings are embedded as a top-level x-wireops block in that one file, which POST /api/custom/stacks/from-compose (create_stack_from_compose) then creates the stack from; two_file true instead emits the deprecated separate wireops.yaml + docker-compose.yml pair. Pass worker_id to validate the result against that worker's effective deploy security policy instead of returning a file that would be rejected at deploy time. Creates nothing itself. Look up each image's current tag, ports, volumes and required environment variables from the project's official site, registry page or GitHub repository first — not from memory, which goes stale. The optional label/annotation conventions (service icon, git-tracked config mounts, run-to-completion services) are not generated by this tool: read the wireops://conventions/compose resource and hand-add the ones you want.",
		Annotations: readOnly(),
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

// Status explanations, emitted only for the status actually returned. These
// used to live in the tool descriptions, where every session paid for them
// whether or not the stack was in that state — the description is read once
// per session, the notice exactly when it applies.
const (
	pausedStatusNotice = "Status \"paused\" means wireops will not act on new commits for this stack. It says nothing about the containers, which may well be running — get_stack_services reports those. Resume with set_stack_paused."

	degradedStatusNotice = "Status \"degraded\" means at least one non-init service is unhealthy, OR an init service (labeled customization.init) exited non-zero or is restart-looping. An init service that exited 0 is healthy and does not degrade a stack by itself. get_stack_services shows which service it is."

	initServiceNotice = "This stack has one or more init services (labeled customization.init, is_init true): run-to-completion containers such as a migration or seed job. For those, status \"exited\" with exit_code 0 is the expected healthy end state, not a failure — only a non-zero exit_code or repeated restarts are a problem."

	unknownServiceStateNotice = "Some entries carry null for is_init/exit_code, which means unknown rather than false/0: the assigned worker is offline and this response fell back to last-known database state, which does not track them. Check the worker with list_workers before drawing conclusions from the container states here."
)

// statusNotice returns the explanation for a stack status, "" for the ones
// that mean what they say.
func statusNotice(status string) string {
	switch status {
	case "paused":
		return pausedStatusNotice
	case "degraded":
		return degradedStatusNotice
	default:
		return ""
	}
}

// appendNotice adds text to a record's _notice, keeping any notice already
// there — a stack can be both paused and carrying render overrides.
func appendNotice(rec map[string]interface{}, text string) {
	if text == "" {
		return
	}
	if existing, ok := rec["_notice"].(string); ok && existing != "" {
		rec["_notice"] = existing + " " + text
		return
	}
	rec["_notice"] = text
}

// hasRenderOverrides reports whether a decoded stacks collection record has
// a non-empty render_overrides field.
func hasRenderOverrides(rec map[string]interface{}) bool {
	overrides, ok := rec["render_overrides"].(map[string]interface{})
	return ok && len(overrides) > 0
}

// whoamiCache memoizes GET /api/custom/me per API key. The identity half
// changes only when the key is reconfigured, and the instance half
// (version, scan period) not until a restart — but every stack record needs
// scan_period_seconds to resolve sync_interval_seconds=0, so without a cache
// that would be one extra request per list_stacks call.
var whoamiCache = newTTLCache(5 * time.Minute)

// fetchWhoami returns the caller's own identity/capability record, cached.
func fetchWhoami(ctx context.Context, c *client.Client, apiKey string) (map[string]interface{}, error) {
	if cached, ok := whoamiCache.get(apiKey); ok {
		return cached, nil
	}
	var out map[string]interface{}
	if err := c.Get(ctx, apiKey, "/api/custom/me", nil, &out); err != nil {
		return nil, err
	}
	whoamiCache.put(apiKey, out)
	return out, nil
}

func whoami(c *client.Client) mcp.ToolHandlerFor[models.WhoamiInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ models.WhoamiInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		out, err := fetchWhoami(ctx, c, apiKey)
		if err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func listStacks(c *client.Client) mcp.ToolHandlerFor[models.ListStacksInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ListStacksInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		repositoryID, err := resolveRepositoryID(ctx, c, apiKey, in.Repository)
		if err != nil {
			return nil, nil, err
		}
		workerID, err := resolveWorkerID(ctx, c, apiKey, in.Worker)
		if err != nil {
			return nil, nil, err
		}

		q := paging(in.Limit, in.Page)
		q.Set("sort", "-updated")
		q.Set("expand", "repository,worker")
		if fields := fieldsParam(in.Fields, stackListFields); fields != "" {
			q.Set("fields", fields)
		}
		if filter := buildFilter([]filterClause{
			{field: "status", op: "=", value: in.Status},
			{field: "name", op: "~", value: in.NameContains},
			{field: "repository", op: "=", value: repositoryID},
			{field: "worker", op: "=", value: workerID},
		}); filter != "" {
			q.Set("filter", filter)
		}

		var out map[string]interface{}
		if err := c.Get(ctx, apiKey, "/api/collections/stacks/records", q, &out); err != nil {
			return nil, nil, err
		}
		globalSeconds := scanPeriodSeconds(ctx, c, apiKey)
		if items, ok := out["items"].([]interface{}); ok {
			for _, item := range items {
				rec, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				annotateSyncInterval(rec, globalSeconds)
				if hasRenderOverrides(rec) {
					appendNotice(rec, renderOverridesNotice)
				}
				if status, ok := rec["status"].(string); ok {
					appendNotice(rec, statusNotice(status))
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
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		var out map[string]interface{}
		path := "/api/collections/stacks/records/" + url.PathEscape(stackID)
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		if hasRenderOverrides(out) {
			appendNotice(out, renderOverridesNotice)
		}
		if status, ok := out["status"].(string); ok {
			appendNotice(out, statusNotice(status))
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
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		q := url.Values{
			"filter":  {fmt.Sprintf("stack='%s'", stackID)},
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

// fetchStackServices lists a stack's services, as returned by
// GET /api/custom/stacks/{id}/services — each entry carries at least
// service_name and container_id.
func fetchStackServices(ctx context.Context, c *client.Client, apiKey, stackID string) ([]interface{}, error) {
	var out []interface{}
	path := "/api/custom/stacks/" + url.PathEscape(stackID) + "/services"
	if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// serviceStateNotices reports which of the is_init/exit_code explanations
// actually apply to a service listing: the init-service one when the stack has
// such a service, and the unknown-state one when a field came back null
// because the worker is offline.
func serviceStateNotices(services []interface{}) []string {
	var hasInit, hasUnknown bool
	for _, svc := range services {
		rec, ok := svc.(map[string]interface{})
		if !ok {
			continue
		}
		if isInit, present := rec["is_init"]; present {
			if isInit == nil {
				hasUnknown = true
			} else if flag, ok := isInit.(bool); ok && flag {
				hasInit = true
			}
		}
		if exitCode, present := rec["exit_code"]; present && exitCode == nil {
			hasUnknown = true
		}
	}
	var notices []string
	if hasInit {
		notices = append(notices, initServiceNotice)
	}
	if hasUnknown {
		notices = append(notices, unknownServiceStateNotice)
	}
	return notices
}

// resolveContainerIDByServiceName looks up a service's container id from the
// stack's service list, for callers that only know the compose service name.
func resolveContainerIDByServiceName(ctx context.Context, c *client.Client, apiKey, stackID, serviceName string) (string, error) {
	services, err := fetchStackServices(ctx, c, apiKey, stackID)
	if err != nil {
		return "", err
	}
	for _, svc := range services {
		m, ok := svc.(map[string]interface{})
		if !ok {
			continue
		}
		if name, _ := m["service_name"].(string); name == serviceName {
			containerID, _ := m["container_id"].(string)
			if containerID == "" {
				return "", fmt.Errorf("service %q has no container_id yet (not running?)", serviceName)
			}
			return containerID, nil
		}
	}
	return "", fmt.Errorf("no service named %q found for stack %q", serviceName, stackID)
}

// resolveContainerTarget turns the (container_id | service_name) pair the
// container-action tools accept into both halves: the id the API needs, and
// the service name the confirmation prompts quote back. Either input is
// enough — whichever is missing is looked up from the stack's service list.
func resolveContainerTarget(ctx context.Context, c *client.Client, apiKey, stackID, containerID, serviceName string) (string, string, error) {
	if containerID == "" && serviceName == "" {
		return "", "", fmt.Errorf("either container_id or service_name is required")
	}
	services, err := fetchStackServices(ctx, c, apiKey, stackID)
	if err != nil {
		return "", "", err
	}
	for _, svc := range services {
		m, ok := svc.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["service_name"].(string)
		id, _ := m["container_id"].(string)
		if (serviceName != "" && name == serviceName) || (serviceName == "" && id == containerID) {
			if id == "" {
				return "", "", fmt.Errorf("service %q has no container_id yet (not running?)", name)
			}
			return id, name, nil
		}
	}
	if serviceName != "" {
		return "", "", fmt.Errorf("no service named %q found for stack %q", serviceName, stackID)
	}
	return "", "", fmt.Errorf("no container %q found for stack %q — call get_stack_services for the current ids", containerID, stackID)
}

// Modes accepted by the env-var bulk routes.
const (
	envVarModeAppend  = "append"
	envVarModeReplace = "replace"
)

func validateEnvVarMode(mode string) error {
	if mode == envVarModeAppend || mode == envVarModeReplace {
		return nil
	}
	return fmt.Errorf("mode must be %q or %q, got %q", envVarModeAppend, envVarModeReplace, mode)
}

func getStackServices(c *client.Client) mcp.ToolHandlerFor[models.StackIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		out, err := fetchStackServices(ctx, c, apiKey, stackID)
		if err != nil {
			return nil, nil, err
		}
		result := map[string]interface{}{"services": out}
		for _, notice := range serviceStateNotices(out) {
			appendNotice(result, notice)
		}
		return nil, result, nil
	}
}

func getStackResources(c *client.Client) mcp.ToolHandlerFor[models.StackIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(stackID) + "/resources"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

// interpolationPattern matches a compose variable reference: ${NAME},
// ${NAME:-default} and ${NAME?err} all capture NAME. $$NAME is a compose
// escape and deliberately not matched.
var interpolationPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)[^}]*\}`)

// unresolvedInterpolations returns the variable names referenced in content
// that are not in defined, in first-appearance order and without duplicates.
func unresolvedInterpolations(content string, defined map[string]bool) []string {
	seen := map[string]bool{}
	var missing []string
	for _, match := range interpolationPattern.FindAllStringSubmatch(content, -1) {
		name := match[1]
		if defined[name] || seen[name] {
			continue
		}
		seen[name] = true
		missing = append(missing, name)
	}
	return missing
}

func getStackCompose(c *client.Client) mcp.ToolHandlerFor[models.StackIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		var out map[string]interface{}
		path := "/api/custom/stacks/" + url.PathEscape(stackID) + "/compose"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}

		// wireops renders with --no-interpolate, so a ${VAR} survives into the
		// revision and is substituted by the worker at `compose up`. One that
		// nothing defines becomes an empty string there — no error, no log
		// line, just a service that behaves as if the value were "". Naming
		// them here is the cheapest place to catch that.
		if content, ok := out["content"].(string); ok && content != "" {
			if missing := unresolvedVariablesForStack(ctx, c, apiKey, stackID, content); len(missing) > 0 {
				out["_notice"] = fmt.Sprintf(
					"These variables are referenced by the compose file but defined by neither the stack's env vars nor a global env var bound to it: %s. "+
						"wireops deploys with --no-interpolate, so the worker substitutes them at `compose up` and an undefined one becomes an empty string silently. "+
						"Run lint_compose with this stack's id to confirm against the full resolved env (it also sees the repository's own .env and SOPS keys), or set them with set_stack_env_vars.",
					strings.Join(missing, ", "))
			}
		}
		return nil, out, nil
	}
}

// unresolvedVariablesForStack lists the compose variable references in content
// that the stack's own and bound-global env vars do not define. A lookup
// failure yields no names rather than a wrong warning — this is an annotation,
// not a gate, and lint_compose is the authoritative check.
func unresolvedVariablesForStack(ctx context.Context, c *client.Client, apiKey, stackID, content string) []string {
	defined := map[string]bool{}

	secretKeys, nonSecretKeys, err := fetchStackEnvVarKeys(ctx, c, apiKey, stackID)
	if err != nil {
		return nil
	}
	for _, key := range append(secretKeys, nonSecretKeys...) {
		defined[key] = true
	}

	globals, err := fetchStackGlobalEnvVarKeys(ctx, c, apiKey, stackID)
	if err != nil {
		return nil
	}
	for _, key := range globals {
		defined[key] = true
	}
	return unresolvedInterpolations(content, defined)
}

// fetchStackGlobalEnvVarKeys lists the names of every global env var bound to
// the stack, secret or not — names only, through the expanded relation.
func fetchStackGlobalEnvVarKeys(ctx context.Context, c *client.Client, apiKey, stackID string) ([]string, error) {
	var out struct {
		Items []struct {
			Expand struct {
				GlobalEnvVar struct {
					Key string `json:"key"`
				} `json:"global_env_var"`
			} `json:"expand"`
		} `json:"items"`
	}
	q := url.Values{
		"filter":  {fmt.Sprintf("stack='%s'", client.EscapeFilterValue(stackID))},
		"expand":  {"global_env_var"},
		"perPage": {"400"},
		"fields":  {"expand.global_env_var.key"},
	}
	if err := c.Get(ctx, apiKey, "/api/collections/stack_global_env_vars/records", q, &out); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(out.Items))
	for _, item := range out.Items {
		if key := item.Expand.GlobalEnvVar.Key; key != "" {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func getStackRevision(c *client.Client) mcp.ToolHandlerFor[models.StackRevisionInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackRevisionInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(stackID) + "/revisions/" + fmt.Sprint(in.Version)
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
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(stackID) + "/container/" + url.PathEscape(in.ContainerID) + "/stats"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
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
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(stackID) + "/render-overrides"
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
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		var a, b any
		pathA := "/api/custom/stacks/" + url.PathEscape(stackID) + "/revisions/" + fmt.Sprint(in.VersionA)
		pathB := "/api/custom/stacks/" + url.PathEscape(stackID) + "/revisions/" + fmt.Sprint(in.VersionB)
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

// fetchStackEnvVarKeys lists a stack's env var *names*, split by whether each
// is marked secret. Only "key" and "secret" are requested, so no value — plain
// or reference — is on the wire.
//
// list_secrets reported secrets only, which reads as "this stack has no env
// vars configured" for a stack whose compose file interpolates plain ones:
// wireops renders with --no-interpolate, so an undefined ${VAR} silently
// becomes an empty string at deploy time instead of failing.
func fetchStackEnvVarKeys(ctx context.Context, c *client.Client, apiKey, stackID string) (secret, nonSecret []string, err error) {
	var out struct {
		Items []struct {
			Key    string `json:"key"`
			Secret bool   `json:"secret"`
		} `json:"items"`
	}
	q := url.Values{
		"filter":  {fmt.Sprintf("stack='%s'", client.EscapeFilterValue(stackID))},
		"perPage": {"400"},
		"fields":  {"key,secret"},
		"sort":    {"key"},
	}
	if err := c.Get(ctx, apiKey, "/api/collections/stack_env_vars/records", q, &out); err != nil {
		return nil, nil, fmt.Errorf("fetching stack env var keys: %w", err)
	}
	for _, item := range out.Items {
		if item.Secret {
			secret = append(secret, item.Key)
			continue
		}
		nonSecret = append(nonSecret, item.Key)
	}
	return secret, nonSecret, nil
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
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}

		stackSecrets, err := fetchStackEnvVarSecrets(ctx, c, apiKey, stackID)
		if err != nil {
			return nil, nil, err
		}
		globalSecrets, err := fetchStackGlobalEnvVarSecrets(ctx, c, apiKey, stackID)
		if err != nil {
			return nil, nil, err
		}
		var sopsSecrets any
		sopsPath := "/api/custom/stacks/" + url.PathEscape(stackID) + "/sops-env-vars"
		if err := c.Get(ctx, apiKey, sopsPath, nil, &sopsSecrets); err != nil {
			return nil, nil, fmt.Errorf("fetching sops env var secrets: %w", err)
		}
		_, nonSecretKeys, err := fetchStackEnvVarKeys(ctx, c, apiKey, stackID)
		if err != nil {
			return nil, nil, err
		}

		result := map[string]any{
			"stack_secrets":    stackSecrets,
			"global_secrets":   globalSecrets,
			"sops_secrets":     sopsSecrets,
			"non_secret_keys":  nonSecretKeys,
			"non_secret_count": len(nonSecretKeys),
			"secret_count":     len(stackSecrets) + len(globalSecrets),
		}
		if len(stackSecrets) == 0 && len(globalSecrets) == 0 && len(nonSecretKeys) > 0 {
			result["_notice"] = "This stack has no secret env vars, but it does have the plain env vars listed in non_secret_keys. An empty secret list does not mean the stack is unconfigured."
		}
		return nil, result, nil
	}
}

func getContainerLogs(c *client.Client) mcp.ToolHandlerFor[models.ContainerLogsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ContainerLogsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}

		var re *regexp.Regexp
		if in.Pattern != "" {
			re, err = regexp.Compile(in.Pattern)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid pattern: %w", err)
			}
		}

		containerID := in.ContainerID
		if containerID == "" {
			if in.ServiceName == "" {
				return nil, nil, fmt.Errorf("either container_id or service_name is required")
			}
			containerID, err = resolveContainerIDByServiceName(ctx, c, apiKey, stackID, in.ServiceName)
			if err != nil {
				return nil, nil, err
			}
		}

		var q url.Values
		if in.Tail != "" {
			q = url.Values{"tail": {in.Tail}}
		}
		var out map[string]interface{}
		path := "/api/custom/stacks/" + url.PathEscape(stackID) + "/container/" + url.PathEscape(containerID) + "/logs"
		if err := c.Get(ctx, apiKey, path, q, &out); err != nil {
			return nil, nil, err
		}

		if re != nil {
			if logs, ok := out["logs"].(string); ok {
				out["logs"] = filterLines(logs, re)
			}
		}
		return nil, out, nil
	}
}

// filterLines keeps only the lines of logs matching re, joined back with "\n".
func filterLines(logs string, re *regexp.Regexp) string {
	lines := strings.Split(logs, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if re.MatchString(line) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func listJobs(c *client.Client) mcp.ToolHandlerFor[models.ListJobsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ListJobsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out []interface{}
		if err := c.Get(ctx, apiKey, "/api/custom/jobs", nil, &out); err != nil {
			return nil, nil, err
		}
		total := len(out)
		filtered := filterRecords(out, in.Limit, func(rec map[string]interface{}) bool {
			return matchesSubstring(recordString(rec, "name"), in.NameContains) &&
				(in.Status == "" || recordString(rec, "status") == in.Status)
		})
		result := map[string]interface{}{"jobs": filtered, "total_before_filter": total}
		if len(filtered) < total {
			result["_notice"] = "The server returns every scheduled job in one response; name_contains/status/limit were applied by the MCP server after fetching, so total_before_filter is the real total."
		}
		return nil, result, nil
	}
}

func getJobDefinition(c *client.Client) mcp.ToolHandlerFor[models.JobIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.JobIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		jobID, err := resolveJobID(ctx, c, apiKey, in.JobID)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/jobs/" + url.PathEscape(jobID) + "/definition"
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
		q := paging(in.Limit, in.Page)
		q.Set("sort", "-updated")
		if fields := fieldsParam(in.Fields, repoListFields); fields != "" {
			q.Set("fields", fields)
		}
		if filter := buildFilter([]filterClause{
			{field: "status", op: "=", value: in.Status},
			{field: "name", op: "~", value: in.NameContains},
		}); filter != "" {
			q.Set("filter", filter)
		}

		var out map[string]interface{}
		if err := c.Get(ctx, apiKey, "/api/collections/repositories/records", q, &out); err != nil {
			return nil, nil, err
		}
		globalSeconds := scanPeriodSeconds(ctx, c, apiKey)
		if items, ok := out["items"].([]interface{}); ok {
			for _, item := range items {
				if rec, ok := item.(map[string]interface{}); ok {
					annotateSyncInterval(rec, globalSeconds)
				}
			}
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
		repositoryID, err := resolveRepositoryID(ctx, c, apiKey, in.RepositoryID)
		if err != nil {
			return nil, nil, err
		}
		var out []interface{}
		path := "/api/custom/repositories/" + url.PathEscape(repositoryID) + "/commits"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, map[string]interface{}{"commits": out}, nil
	}
}

// registeredStack is one stack of a repository, reduced to what identifies the
// compose file it was created from.
type registeredStack struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ComposePath string `json:"compose_path"`
	ComposeFile string `json:"compose_file"`
}

// composeFilePath rebuilds the repository-relative path of a stack's compose
// file, matching how the stack-files route reports one ("stacks/web/
// docker-compose.yml"). compose_file defaults to docker-compose.yml, the same
// default the create routes apply.
func (s registeredStack) composeFilePath() string {
	file := s.ComposeFile
	if file == "" {
		file = "docker-compose.yml"
	}
	dir := strings.Trim(s.ComposePath, "/")
	if dir == "" || dir == "." {
		return file
	}
	return dir + "/" + file
}

// fetchRegisteredStacks lists the stacks a repository already has, so a file
// listing can say which of its compose files are taken.
func fetchRegisteredStacks(ctx context.Context, c *client.Client, apiKey, repositoryID string) ([]registeredStack, error) {
	var out struct {
		Items []registeredStack `json:"items"`
	}
	q := url.Values{
		"filter":  {fmt.Sprintf("repository='%s'", client.EscapeFilterValue(repositoryID))},
		"perPage": {"400"},
		"fields":  {"id,name,compose_path,compose_file"},
	}
	if err := c.Get(ctx, apiKey, "/api/collections/stacks/records", q, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// annotateRepoFiles turns the route's plain path strings into objects that say
// whether each path is already registered as a wireops stack (or job).
//
// Finding a compose file in a repository and wireops managing it are separate
// things, and nothing in the raw listing said so — the obvious misreading is
// that a listed file is already a stack. A listing that cannot be annotated
// (the lookup failed) is returned unchanged rather than marked false.
func annotateRepoFiles(files []interface{}, registeredBy map[string]map[string]interface{}) []interface{} {
	out := make([]interface{}, 0, len(files))
	for _, file := range files {
		path, ok := file.(string)
		if !ok {
			out = append(out, file)
			continue
		}
		entry := map[string]interface{}{"path": path, "registered": false}
		if meta, found := registeredBy[strings.TrimPrefix(path, "./")]; found {
			entry["registered"] = true
			for key, value := range meta {
				entry[key] = value
			}
		}
		out = append(out, entry)
	}
	return out
}

func listRepoStackFiles(c *client.Client) mcp.ToolHandlerFor[models.RepositoryIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.RepositoryIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		repositoryID, err := resolveRepositoryID(ctx, c, apiKey, in.RepositoryID)
		if err != nil {
			return nil, nil, err
		}
		var out []interface{}
		path := "/api/custom/repositories/" + url.PathEscape(repositoryID) + "/stack-files"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}

		result := map[string]interface{}{"files": out}
		stacks, err := fetchRegisteredStacks(ctx, c, apiKey, repositoryID)
		if err != nil {
			return nil, result, nil
		}
		registeredBy := make(map[string]map[string]interface{}, len(stacks))
		for _, stack := range stacks {
			registeredBy[stack.composeFilePath()] = map[string]interface{}{
				"stack_id":   stack.ID,
				"stack_name": stack.Name,
			}
		}
		result["files"] = annotateRepoFiles(out, registeredBy)
		result["_notice"] = "registered:false means the file is in git but no wireops stack points at it yet — creating one is a separate step (create_stack_from_compose)."
		return nil, result, nil
	}
}

func listRepoJobFiles(c *client.Client) mcp.ToolHandlerFor[models.RepositoryIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.RepositoryIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		repositoryID, err := resolveRepositoryID(ctx, c, apiKey, in.RepositoryID)
		if err != nil {
			return nil, nil, err
		}
		var out []interface{}
		path := "/api/custom/repositories/" + url.PathEscape(repositoryID) + "/job-files"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}

		result := map[string]interface{}{"files": out}
		var jobs struct {
			Items []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				JobFile string `json:"job_file"`
			} `json:"items"`
		}
		q := url.Values{
			"filter":  {fmt.Sprintf("repository='%s'", client.EscapeFilterValue(repositoryID))},
			"perPage": {"400"},
			"fields":  {"id,name,job_file"},
		}
		if err := c.Get(ctx, apiKey, "/api/collections/scheduled_jobs/records", q, &jobs); err != nil {
			return nil, result, nil
		}
		registeredBy := make(map[string]map[string]interface{}, len(jobs.Items))
		for _, job := range jobs.Items {
			registeredBy[strings.TrimPrefix(job.JobFile, "./")] = map[string]interface{}{
				"job_id":   job.ID,
				"job_name": job.Name,
			}
		}
		result["files"] = annotateRepoFiles(out, registeredBy)
		result["_notice"] = "registered:false means the job.yaml is in git but no wireops scheduled job points at it yet — scheduling one is a separate step (create_scheduled_job)."
		return nil, result, nil
	}
}

func getStackIntegrationActions(c *client.Client) mcp.ToolHandlerFor[models.StackIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StackIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		stackID, err := resolveStackID(ctx, c, apiKey, in.StackID)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(stackID) + "/integration-actions"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func listWorkers(c *client.Client) mcp.ToolHandlerFor[models.ListWorkersInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ListWorkersInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out []interface{}
		if err := c.Get(ctx, apiKey, "/api/custom/workers", nil, &out); err != nil {
			return nil, nil, err
		}
		total := len(out)
		filtered := filterRecords(out, in.Limit, func(rec map[string]interface{}) bool {
			return matchesSubstring(recordString(rec, "hostname"), in.HostnameContains) &&
				(in.Status == "" || recordString(rec, "status") == in.Status)
		})
		result := map[string]interface{}{"workers": filtered, "total_before_filter": total}
		if len(filtered) < total {
			result["_notice"] = "The server returns every worker in one response; hostname_contains/status/limit were applied by the MCP server after fetching, so total_before_filter is the real total."
		}
		return nil, result, nil
	}
}

func getWorkerMetrics(c *client.Client) mcp.ToolHandlerFor[models.WorkerIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.WorkerIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		workerID, err := resolveWorkerID(ctx, c, apiKey, in.WorkerID)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/workers/" + url.PathEscape(workerID) + "/metrics"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getWorkerPolicy(c *client.Client) mcp.ToolHandlerFor[models.WorkerIDInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.WorkerIDInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		workerID, err := resolveWorkerID(ctx, c, apiKey, in.WorkerID)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/workers/" + url.PathEscape(workerID) + "/policy"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func getGlobalWorkerPolicy(c *client.Client) mcp.ToolHandlerFor[models.GetGlobalWorkerPolicyInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ models.GetGlobalWorkerPolicyInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		if err := c.Get(ctx, apiKey, "/api/custom/settings/worker-policy", nil, &out); err != nil {
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

// scaffoldStackOutput is the result of the scaffold_stack tool. Wireops is
// omitted unless the caller opted into the legacy two-file layout (TwoFile)
// — by default the wireops fields are embedded in Compose instead, so there
// is nothing separate to commit.
type scaffoldStackOutput struct {
	Wireops *generatedFile `json:"wireops,omitempty"`
	Compose generatedFile  `json:"compose"`
	// Notice points at the conventions this tool cannot express, so the
	// pointer travels with the generated file rather than living in the
	// tool's description where every session pays for it.
	Notice string `json:"_notice,omitempty"`
}

// xWireopsBlock converts a manifest.Definition to the plain
// map[string]interface{} shape needed to splice it into a compose config map
// under the "x-wireops" key, via a YAML round-trip through its yaml tags —
// the same tags manifest.ParseComposeManifest decodes back out of that key.
func xWireopsBlock(def *manifest.Definition) (map[string]interface{}, error) {
	raw, err := yaml.Marshal(def)
	if err != nil {
		return nil, fmt.Errorf("marshaling x-wireops block: %w", err)
	}
	var block map[string]interface{}
	if err := yaml.Unmarshal(raw, &block); err != nil {
		return nil, fmt.Errorf("marshaling x-wireops block: %w", err)
	}
	return block, nil
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

		composeMap, err := composeConfigFrom(in.Services)
		if err != nil {
			return nil, nil, err
		}

		if !in.TwoFile {
			block, err := xWireopsBlock(wireopsDef)
			if err != nil {
				return nil, nil, err
			}
			composeMap["x-wireops"] = block
		}

		if in.WorkerID != "" {
			apiKey, err := apiKeyFrom(ctx)
			if err != nil {
				return nil, nil, err
			}
			workerID, err := resolveWorkerID(ctx, c, apiKey, in.WorkerID)
			if err != nil {
				return nil, nil, err
			}
			workerPolicy, err := fetchEffectiveWorkerPolicy(ctx, c, apiKey, workerID)
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

		out := scaffoldStackOutput{
			Compose: generatedFile{Filename: "docker-compose.yml", Content: string(composeContent)},
			Notice:  "Generated from the structured input only. The wireops label/annotation conventions — customization.image.slug (UI icon), dev.wireops.config.<name> (mount a git-tracked file) and customization.init (run-to-completion service) — have no field in this tool's input and are not in the file above; read the wireops://conventions/compose resource and add the ones you want before committing.",
		}
		if in.TwoFile {
			wireopsContent, err := yaml.Marshal(wireopsDef)
			if err != nil {
				return nil, nil, fmt.Errorf("marshaling wireops.yaml: %w", err)
			}
			out.Wireops = &generatedFile{Filename: "wireops.yaml", Content: string(wireopsContent)}
		}
		return nil, out, nil
	}
}
