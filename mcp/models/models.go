// Package models holds the JSON-tagged structs exchanged between MCP tool
// handlers and the caller — the input schema for each tool call.
package models

import (
	"fmt"
	"strings"
)

// CanonicalOrAlias normalizes a canonical MCP argument with its documented
// ergonomic alias. Supplying both is fine when they identify the same target;
// a disagreement is rejected rather than silently choosing one.
func CanonicalOrAlias(canonical, alias, canonicalName, aliasName string) (string, error) {
	canonical = strings.TrimSpace(canonical)
	alias = strings.TrimSpace(alias)
	if canonical != "" && alias != "" && canonical != alias {
		return "", fmt.Errorf("%s and %s identify different values; provide one or use the same value for both", canonicalName, aliasName)
	}
	if canonical != "" {
		return canonical, nil
	}
	if alias != "" {
		return alias, nil
	}
	return "", nil
}

// WhoamiInput is the input for the whoami tool. It takes nothing: the API
// key on the session is the subject.
type WhoamiInput struct{}

// ListStacksInput is the input for the list_stacks tool.
type ListStacksInput struct {
	Limit        int      `json:"limit,omitempty" jsonschema:"Maximum number of stacks per page (default 50, max 200)."`
	Page         int      `json:"page,omitempty" jsonschema:"Page number, 1-indexed (default 1)."`
	Status       string   `json:"status,omitempty" jsonschema:"Only stacks with this exact status (active, paused, syncing, error, degraded). Optional."`
	NameContains string   `json:"name_contains,omitempty" jsonschema:"Only stacks whose name contains this substring, case-insensitive. Optional."`
	Repository   string   `json:"repository,omitempty" jsonschema:"Only stacks in this repository (record id or exact name). Optional."`
	Worker       string   `json:"worker,omitempty" jsonschema:"Only stacks assigned to this worker (record id or exact hostname). Optional."`
	Fields       []string `json:"fields,omitempty" jsonschema:"Which record fields to return. Optional — the default is a compact diagnostic projection (id, name, status, repository, worker, versions, last error, sync interval). Pass [\"*\"] for the complete records."`
}

// StackIDInput is the input for tools that operate on a single stack.
type StackIDInput struct {
	StackID string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack. One of stack_id or stack is required."`
	Stack   string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
}

// SyncLogsInput is the input for the get_sync_logs tool.
type SyncLogsInput struct {
	StackID string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack. One of stack_id or stack is required."`
	Stack   string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum number of log entries to return (default 20)."`
}

// ContainerLogsInput is the input for the get_container_logs tool.
type ContainerLogsInput struct {
	StackID       string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack. One of stack_id or stack is required."`
	Stack         string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	ContainerID   string `json:"container_id,omitempty" jsonschema:"The Docker container id as returned by get_stack_services. Alias: container_name."`
	ContainerName string `json:"container_name,omitempty" jsonschema:"Alias for container_id: the real Docker container name inside this stack. May be supplied together only with the same value."`
	ServiceName   string `json:"service_name,omitempty" jsonschema:"The compose service name to fetch logs for, resolved to a container id via get_stack_services. Alternative to container_id."`
	Tail          string `json:"tail,omitempty" jsonschema:"Number of trailing log lines to fetch (default 100). Alias: lines."`
	Lines         string `json:"lines,omitempty" jsonschema:"Alias for tail. May be supplied together only with the same value."`
	Pattern       string `json:"pattern,omitempty" jsonschema:"Regex pattern; only matching log lines are returned. Alias: search."`
	Search        string `json:"search,omitempty" jsonschema:"Alias for pattern. May be supplied together only with the same value."`
	Since         string `json:"since,omitempty" jsonschema:"Start of log window: RFC3339 with timezone, Unix timestamp, or positive duration such as 30m."`
	Until         string `json:"until,omitempty" jsonschema:"End of log window: RFC3339 with timezone, Unix timestamp, or positive duration such as 5m."`
}

// InventoryInput narrows the read-only fleet inventory and pages the compact
// stack observations returned by GET /api/custom/inventory.
type InventoryInput struct {
	Worker string `json:"worker,omitempty" jsonschema:"Optional worker id or exact hostname."`
	Group  string `json:"group,omitempty" jsonschema:"Optional exact stack group."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Stacks per page, default 50, maximum 200."`
	Page   int    `json:"page,omitempty" jsonschema:"Page number, 1-indexed."`
}

// ListJobsInput is the input for the list_jobs tool. The route returns every
// job in one array, so these narrow the response rather than the query.
type ListJobsInput struct {
	NameContains string `json:"name_contains,omitempty" jsonschema:"Only jobs whose name contains this substring, case-insensitive. Optional."`
	Status       string `json:"status,omitempty" jsonschema:"Only jobs with this exact status. Optional."`
	Limit        int    `json:"limit,omitempty" jsonschema:"Return at most this many jobs. Optional — unset returns all of them."`
}

// JobIDInput is the input for tools that operate on a single scheduled job.
type JobIDInput struct {
	JobID string `json:"job_id" jsonschema:"The wireops scheduled job: its record id, or its exact name."`
}

// RepositoryIDInput is the input for tools that operate on a single repository.
type RepositoryIDInput struct {
	RepositoryID string `json:"repository_id" jsonschema:"The wireops repository: its record id, or its exact name."`
}

// ListReposInput is the input for the list_repos tool.
type ListReposInput struct {
	Limit        int      `json:"limit,omitempty" jsonschema:"Maximum number of repositories per page (default 50, max 200)."`
	Page         int      `json:"page,omitempty" jsonschema:"Page number, 1-indexed (default 1)."`
	Status       string   `json:"status,omitempty" jsonschema:"Only repositories with this exact status. Optional."`
	NameContains string   `json:"name_contains,omitempty" jsonschema:"Only repositories whose name contains this substring, case-insensitive. Optional."`
	Fields       []string `json:"fields,omitempty" jsonschema:"Which record fields to return. Optional — the default is a compact projection. Pass [\"*\"] for the complete records."`
}

// DiffStackVersionInput is the input for the diff_stack_version tool.
type DiffStackVersionInput struct {
	StackID  string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack. One of stack_id or stack is required."`
	Stack    string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	VersionA int    `json:"version_a" jsonschema:"The first rendered revision number to compare, e.g. 33."`
	VersionB int    `json:"version_b" jsonschema:"The second rendered revision number to compare, e.g. 34."`
}

// ContainerStatsInput is the input for the get_container_stats tool.
type ContainerStatsInput struct {
	StackID       string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack. One of stack_id or stack is required."`
	Stack         string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	ContainerID   string `json:"container_id,omitempty" jsonschema:"The Docker container id. Alias: container_name. One is required."`
	ContainerName string `json:"container_name,omitempty" jsonschema:"Alias for container_id: the real Docker container name inside this stack."`
}

// StackRevisionInput is the input for the get_stack_revision tool.
type StackRevisionInput struct {
	StackID string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack. One of stack_id or stack is required."`
	Stack   string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	Version int    `json:"version" jsonschema:"The rendered revision number to fetch, e.g. 34."`
}

// ListWorkersInput is the input for the list_workers tool. The route returns
// every worker in one array, so these narrow the response rather than the query.
type ListWorkersInput struct {
	HostnameContains string `json:"hostname_contains,omitempty" jsonschema:"Only workers whose hostname contains this substring, case-insensitive. Optional."`
	Status           string `json:"status,omitempty" jsonschema:"Only workers with this live status (ACTIVE or OFFLINE). Optional."`
	Limit            int    `json:"limit,omitempty" jsonschema:"Return at most this many workers. Optional — unset returns all of them."`
}

// WorkerIDInput is the input for tools that operate on a single worker.
type WorkerIDInput struct {
	WorkerID string `json:"worker_id" jsonschema:"The wireops worker: its record id, or its exact hostname."`
}

// GetGlobalWorkerPolicyInput is the input for the get_global_worker_policy tool.
type GetGlobalWorkerPolicyInput struct{}

// GenerateWireopsYAMLInput is the input for the generate_wireops_yaml tool.
// Fields mirror internal/manifest.Definition.
type GenerateWireopsYAMLInput struct {
	Name            string   `json:"name" jsonschema:"Stack name (required)."`
	Timeout         string   `json:"timeout,omitempty" jsonschema:"Deploy timeout as a Go duration string (e.g. '5m'). Optional."`
	RemoveOrphans   *bool    `json:"remove_orphans,omitempty" jsonschema:"Whether 'docker compose up' should remove orphaned containers. Optional."`
	ForcePull       *bool    `json:"force_pull,omitempty" jsonschema:"Whether to force-pull images on every deploy. Optional."`
	WaitRunningJobs *bool    `json:"wait_running_jobs,omitempty" jsonschema:"Whether deploy should wait for in-flight scheduled jobs on this stack to finish first. Optional."`
	WorkerTags      []string `json:"worker_tags,omitempty" jsonschema:"Worker tags this stack should be scheduled onto. Optional."`
	SyncIntervalGo  string   `json:"sync_interval,omitempty" jsonschema:"Git polling interval as a Go duration string (e.g. '30s'). Overrides the server's global SCAN_PERIOD for this stack. Optional."`
}

// GenerateJobYAMLInput is the input for the generate_job_yaml tool. Fields
// mirror internal/job.Definition.
type GenerateJobYAMLInput struct {
	Name        string   `json:"name" jsonschema:"Job name (required)."`
	Description string   `json:"description" jsonschema:"Human-readable description (required)."`
	Cron        string   `json:"cron" jsonschema:"Cron schedule expression (required, e.g. '0 * * * *')."`
	Image       string   `json:"image" jsonschema:"Docker image to run (required)."`
	Command     []string `json:"command,omitempty" jsonschema:"Command to run inside the container. Optional."`
	Tags        []string `json:"tags,omitempty" jsonschema:"Worker tags this job should be scheduled onto. Optional."`
	Mode        string   `json:"mode,omitempty" jsonschema:"Dispatch mode: 'once' (single matching worker, round-robin) or 'once_all' (every matching worker). Defaults to 'once'."`
	Volumes     []string `json:"volumes,omitempty" jsonschema:"Bind mounts in 'host:container' form. Optional."`
	Network     string   `json:"network,omitempty" jsonschema:"Docker network to attach the job container to. Optional."`
	CPU         string   `json:"cpu" jsonschema:"CPU limit (required, e.g. '1')."`
	Memory      string   `json:"memory" jsonschema:"Memory limit (required, e.g. '512mb')."`
	ResTimeout  string   `json:"timeout" jsonschema:"Job run timeout as a Go duration string (required, e.g. '30s')."`
}

// ComposeServiceInput describes one docker-compose service for the
// scaffold_stack tool.
type ComposeServiceInput struct {
	Name        string            `json:"name" jsonschema:"Service name (required)."`
	Image       string            `json:"image" jsonschema:"Docker image, including tag (required)."`
	Command     []string          `json:"command,omitempty" jsonschema:"Container command override. Optional."`
	Environment map[string]string `json:"environment,omitempty" jsonschema:"Environment variables. Optional."`
	Ports       []string          `json:"ports,omitempty" jsonschema:"Port mappings in 'host:container' form. Optional."`
	Volumes     []string          `json:"volumes,omitempty" jsonschema:"Volume mounts in 'source:target' form. Optional."`
	Networks    []string          `json:"networks,omitempty" jsonschema:"Docker networks this service attaches to. Optional."`
	DependsOn   []string          `json:"depends_on,omitempty" jsonschema:"Other service names this service depends on. Optional."`
}

// ScaffoldStackInput is the input for the scaffold_stack tool.
type ScaffoldStackInput struct {
	Name     string                   `json:"name" jsonschema:"Stack name (required)."`
	Wireops  GenerateWireopsYAMLInput `json:"wireops" jsonschema:"wireops fields for this stack. 'name' inside this object is optional and defaults to the top-level 'name'."`
	Services []ComposeServiceInput    `json:"services" jsonschema:"The docker-compose services to generate (required, at least one)."`
	WorkerID string                   `json:"worker_id,omitempty" jsonschema:"If set (record id or hostname), validates the generated compose file against this worker's effective deploy security policy before returning."`
	TwoFile  bool                     `json:"two_file,omitempty" jsonschema:"Leave this false (the default and the recommended, primary way to define a wireops stack): the wireops fields are embedded as a top-level 'x-wireops' extension block inside the single returned compose file, ready for POST /api/custom/stacks/from-compose. docker compose ignores unknown top-level 'x-*' keys, and this block never overlaps the service-level dev.wireops.* labels/annotations wireops itself injects or reads. Set true only to generate the legacy two-file layout (a separate wireops.yaml plus docker-compose.yml, for POST /api/custom/stacks/from-wireops) — e.g. to match an existing repository that already uses that layout, or on an explicit user request."`
}

// ---------------------------------------------------------------------------
// Write tool inputs (operator-scoped). Every tool whose effect cannot be
// undone carries a Confirm field — see mcp/tools/confirm.go for the rule.
// ---------------------------------------------------------------------------

// DeployStackInput is the input for the deploy_stack tool.
type DeployStackInput struct {
	StackID        string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack."`
	Stack          string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	Wait           bool   `json:"wait,omitempty" jsonschema:"Block until the reconcile finishes and return its outcome (final sync status, stack status, deploy log) instead of returning as soon as it is queued. Optional."`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"How long to wait when wait is true. Optional — defaults to 120, capped at 600."`
}

// ForceRedeployStackInput is the input for the force_redeploy_stack tool.
type ForceRedeployStackInput struct {
	StackID            string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack."`
	Stack              string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	RecreateContainers bool   `json:"recreate_containers,omitempty" jsonschema:"Recreate every container even if its config is unchanged. Optional."`
	RecreateVolumes    bool   `json:"recreate_volumes,omitempty" jsonschema:"DESTROYS DATA: delete and recreate the stack's named volumes. Requires confirm. Optional."`
	RecreateNetworks   bool   `json:"recreate_networks,omitempty" jsonschema:"Delete and recreate the stack's networks. Optional."`
	PauseAfter         *bool  `json:"pause_after_redeploy,omitempty" jsonschema:"Pause git sync after a successful redeploy. Defaults to true, matching the UI."`
	Confirm            string `json:"confirm,omitempty" jsonschema:"Required only when recreate_volumes is true: the stack's exact name, confirming the volume data loss."`
	Wait               bool   `json:"wait,omitempty" jsonschema:"Block until the reconcile finishes and return its outcome (final sync status, stack status, deploy log) instead of returning as soon as it is queued. Optional."`
	TimeoutSeconds     int    `json:"timeout_seconds,omitempty" jsonschema:"How long to wait when wait is true. Optional — defaults to 120, capped at 600."`
}

// RollbackStackInput is the input for the rollback_stack tool.
type RollbackStackInput struct {
	StackID        string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack."`
	Stack          string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	CommitSHA      string `json:"commit_sha" jsonschema:"The git commit SHA to roll the stack back to."`
	Wait           bool   `json:"wait,omitempty" jsonschema:"Block until the reconcile finishes and return its outcome (final sync status, stack status, deploy log) instead of returning as soon as it is queued. Optional."`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"How long to wait when wait is true. Optional — defaults to 120, capped at 600."`
}

// SetStackPausedInput is the input for the set_stack_paused tool.
type SetStackPausedInput struct {
	StackID string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack."`
	Stack   string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	Paused  bool   `json:"paused" jsonschema:"true pauses git sync/reconcile for this stack, false resumes it (only allowed when the stack is currently paused). Does not start or stop any container."`
}

// ServiceOverrideInput is one service's render-time override, mirroring
// internal/sync.ServiceOverride.
type ServiceOverrideInput struct {
	Image    string   `json:"image,omitempty" jsonschema:"Image to use instead of the one in git, including tag. Optional."`
	Ports    []string `json:"ports,omitempty" jsonschema:"Port mappings in 'host:container' form, replacing the ones in git. Optional."`
	Networks []string `json:"networks,omitempty" jsonschema:"Networks to attach instead of the ones in git. Optional."`
	Scale    *int     `json:"scale,omitempty" jsonschema:"Number of replicas to run. Optional."`
}

// SetStackRenderOverridesInput is the input for the set_stack_render_overrides tool.
type SetStackRenderOverridesInput struct {
	StackID   string                          `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack."`
	Stack     string                          `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	Overrides map[string]ServiceOverrideInput `json:"overrides" jsonschema:"Map of compose service name to the override applied to it. Replaces the stack's whole override set; must not be empty."`
}

// ClearStackRenderOverridesInput is the input for the clear_stack_render_overrides tool.
type ClearStackRenderOverridesInput struct {
	StackID string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack."`
	Stack   string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
}

// ContainerActionInput is the input for the restart_container tool.
type ContainerActionInput struct {
	StackID       string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack."`
	Stack         string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	ContainerID   string `json:"container_id,omitempty" jsonschema:"The Docker container id. Alias: container_name."`
	ContainerName string `json:"container_name,omitempty" jsonschema:"Alias for container_id: real Docker container name inside this stack."`
	ServiceName   string `json:"service_name,omitempty" jsonschema:"The compose service name, resolved to a container id via get_stack_services. Alternative to container_id."`
}

// StopContainerInput is the input for the stop_container tool.
type StopContainerInput struct {
	StackID       string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack."`
	Stack         string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	ContainerID   string `json:"container_id,omitempty" jsonschema:"The Docker container id. Alias: container_name."`
	ContainerName string `json:"container_name,omitempty" jsonschema:"Alias for container_id: real Docker container name inside this stack."`
	ServiceName   string `json:"service_name,omitempty" jsonschema:"The compose service name, resolved to a container id via get_stack_services. Alternative to container_id."`
	Confirm       string `json:"confirm" jsonschema:"The exact compose service name of the container being stopped, confirming the intended target."`
}

// TransferStackInput is the input for the transfer_stack tool.
type TransferStackInput struct {
	StackID        string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack."`
	Stack          string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	TargetWorkerID string `json:"target_worker_id" jsonschema:"The worker to move the stack to: its record id, or its exact hostname. Must be online."`
	Confirm        string `json:"confirm" jsonschema:"The stack's exact name. Transferring tears the stack down on its current worker."`
}

// DeleteStackInput is the input for the delete_stack tool.
type DeleteStackInput struct {
	StackID string `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack."`
	Stack   string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	Force   bool   `json:"force,omitempty" jsonschema:"Delete the wireops record even if the assigned worker is offline, leaving the containers running on the host. Optional."`
	Confirm string `json:"confirm" jsonschema:"The stack's exact name. This tears the stack down and deletes its record."`
}

// BulkEnvVarInput is one entry of a bulk env var upsert, mirroring the
// server's bulkEnvVar.
type BulkEnvVarInput struct {
	Key            string `json:"key" jsonschema:"Env var name. Must match ^[A-Za-z_][A-Za-z0-9_]*$."`
	Value          string `json:"value" jsonschema:"Env var value, or the external provider reference when secret_provider is set."`
	Secret         bool   `json:"secret,omitempty" jsonschema:"Store the value encrypted and mask it on every read path. Optional."`
	SecretProvider string `json:"secret_provider,omitempty" jsonschema:"External secret provider ('vault', 'infisical', ...) when the value is a reference rather than the secret itself. Optional."`
}

// SetStackEnvVarsInput is the input for the set_stack_env_vars tool.
type SetStackEnvVarsInput struct {
	StackID string            `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack."`
	Stack   string            `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	Mode    string            `json:"mode" jsonschema:"'append' upserts the given keys and leaves the rest alone; 'replace' makes the stack's env vars exactly this list, deleting any key not present."`
	Vars    []BulkEnvVarInput `json:"vars" jsonschema:"The env vars to write."`
	Confirm string            `json:"confirm,omitempty" jsonschema:"Required only when mode is 'replace': the stack's exact name, confirming that unlisted keys get deleted."`
}

// CopyStackEnvVarsInput is the input for the copy_stack_env_vars tool.
type CopyStackEnvVarsInput struct {
	StackID     string   `json:"stack_id,omitempty" jsonschema:"The destination wireops stack id or exact name. Alias: stack."`
	Stack       string   `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	SourceStack string   `json:"source_stack" jsonschema:"The stack to copy env vars from: its record id, or its exact name."`
	Keys        []string `json:"keys" jsonschema:"The env var keys to copy. Required — an empty list copies nothing. Use list_secrets plus the stack_env_vars collection to find the available keys."`
	Overwrite   bool     `json:"overwrite,omitempty" jsonschema:"Overwrite keys that already exist on the destination stack. Optional."`
}

// DiscoverImportableStacksInput is the input for the discover_importable_stacks tool.
type DiscoverImportableStacksInput struct{}

// ImportStackInput is the input for the import_stack tool.
type ImportStackInput struct {
	Name            string `json:"name" jsonschema:"Name for the new wireops stack."`
	WorkerID        string `json:"worker_id" jsonschema:"The worker whose host holds the compose file: its record id, or its exact hostname. Must be online."`
	ImportPath      string `json:"import_path" jsonschema:"Absolute path to the existing docker-compose .yml/.yaml file on the worker's host. No '..' allowed."`
	RecreateVolumes bool   `json:"recreate_volumes,omitempty" jsonschema:"DESTROYS DATA: recreate the imported stack's volumes instead of adopting them. Optional."`
}

// SyncRepositoryInput is the input for the sync_repository tool.
type SyncRepositoryInput struct {
	RepositoryID string `json:"repository_id" jsonschema:"The wireops repository: its record id, or its exact name."`
}

// CreateStackFromComposeInput is the input for the create_stack_from_compose tool.
type CreateStackFromComposeInput struct {
	Repository  string `json:"repository" jsonschema:"The wireops repository holding the compose file: its record id, or its exact name."`
	Worker      string `json:"worker" jsonschema:"The worker to deploy the stack on: its record id, or its exact hostname."`
	ComposePath string `json:"compose_path,omitempty" jsonschema:"Directory inside the repository containing the compose file. Optional — defaults to the repository root."`
	ComposeFile string `json:"compose_file,omitempty" jsonschema:"Compose file name. Optional — defaults to docker-compose.yml."`
	Paused      bool   `json:"paused,omitempty" jsonschema:"Create the stack paused instead of deploying it immediately — useful when env vars still have to be filled in. Optional."`
}

// CreateStackFromWireopsInput is the input for the create_stack_from_wireops tool.
type CreateStackFromWireopsInput struct {
	Repository  string `json:"repository" jsonschema:"The wireops repository holding the wireops.yaml: its record id, or its exact name."`
	Worker      string `json:"worker" jsonschema:"The worker to deploy the stack on: its record id, or its exact hostname."`
	WireopsFile string `json:"wireops_file" jsonschema:"Repository-relative path of the wireops.yaml, as returned by browse_repo_files."`
	Paused      bool   `json:"paused,omitempty" jsonschema:"Create the stack paused instead of deploying it immediately. Optional."`
}

// LintComposeInput is the input for the lint_compose tool. Mirrors the
// server's lintComposeRequest — the linter reads the file out of the
// repository workspace, it does not take raw YAML.
type LintComposeInput struct {
	Repository  string `json:"repository" jsonschema:"The wireops repository holding the compose file: its record id, or its exact name."`
	ComposePath string `json:"compose_path,omitempty" jsonschema:"Directory inside the repository containing the compose file. Optional — defaults to the repository root."`
	ComposeFile string `json:"compose_file,omitempty" jsonschema:"Compose file name. Optional — defaults to docker-compose.yml."`
	Worker      string `json:"worker,omitempty" jsonschema:"Also check the file against this worker's effective deploy security policy (record id or hostname). Optional — without it the report carries advisory findings only."`
	StackID     string `json:"stack_id,omitempty" jsonschema:"Existing stack id or name whose env vars should be resolved. Alias: stack. Optional."`
	Stack       string `json:"stack,omitempty" jsonschema:"Alias for stack_id when resolving env vars for an existing stack. Optional; both values must match when supplied."`
}

// PreviewStackMigrationInput is the input for the preview_stack_migration
// tool. Mirrors the server's migrateRequestBody, minus the mutating fields.
type PreviewStackMigrationInput struct {
	StackID     string `json:"stack_id,omitempty" jsonschema:"The wireops stack to migrate. Alias: stack."`
	Stack       string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	Repository  string `json:"repository" jsonschema:"The destination repository: its record id, or its exact name. Must differ from the stack's current repository."`
	ComposePath string `json:"compose_path,omitempty" jsonschema:"Directory inside the destination repository holding the compose file. Optional."`
	ComposeFile string `json:"compose_file,omitempty" jsonschema:"Compose file name in the destination repository. Optional — defaults to docker-compose.yml."`
	WireopsFile string `json:"wireops_file,omitempty" jsonschema:"wireops.yaml path in the destination repository, for the deprecated two-file layout. Optional."`
}

// MigrateStackInput is the input for the migrate_stack tool. The server
// additionally requires its own confirm:true in the body; the tool sets that
// itself once the name confirmation below has passed.
type MigrateStackInput struct {
	StackID            string `json:"stack_id,omitempty" jsonschema:"The wireops stack to migrate. Alias: stack."`
	Stack              string `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	Repository         string `json:"repository" jsonschema:"The destination repository: its record id, or its exact name. Must differ from the stack's current repository."`
	ComposePath        string `json:"compose_path,omitempty" jsonschema:"Directory inside the destination repository holding the compose file. Optional."`
	ComposeFile        string `json:"compose_file,omitempty" jsonschema:"Compose file name in the destination repository. Optional — defaults to docker-compose.yml."`
	WireopsFile        string `json:"wireops_file,omitempty" jsonschema:"wireops.yaml path in the destination repository, for the deprecated two-file layout. Optional."`
	TeardownOldProject bool   `json:"teardown_old_project,omitempty" jsonschema:"Tear the stack down under its old compose project name after the migration. Optional."`
	Confirm            string `json:"confirm" jsonschema:"The stack's exact name. The migration repoints the stack at another repository."`
}

// BrowseRepoFilesInput is the input for the browse_repo_files tool.
type BrowseRepoFilesInput struct {
	RepositoryID string `json:"repository_id" jsonschema:"The wireops repository: its record id, or its exact name."`
}

// RepoFileDefinitionInput is the input for the tools that parse one file out
// of a repository (compose, wireops.yaml, job.yaml).
type RepoFileDefinitionInput struct {
	RepositoryID string `json:"repository_id" jsonschema:"The wireops repository: its record id, or its exact name."`
	Path         string `json:"path" jsonschema:"Repository-relative path of the file to parse, as returned by list_repo_stack_files / list_repo_job_files / browse_repo_files."`
}

// CreateRepositoryInput is the input for the create_repository tool.
type CreateRepositoryInput struct {
	Name   string `json:"name" jsonschema:"Display name for the repository."`
	GitURL string `json:"git_url" jsonschema:"Clone URL (https:// or git@...)."`
	Branch string `json:"branch,omitempty" jsonschema:"Branch to track. Optional — defaults to main."`
}

// UpdateRepositoryInput is the input for the update_repository tool.
type UpdateRepositoryInput struct {
	RepositoryID string `json:"repository_id" jsonschema:"The wireops repository: its record id, or its exact name."`
	Name         string `json:"name,omitempty" jsonschema:"New display name. Optional."`
	GitURL       string `json:"git_url,omitempty" jsonschema:"New clone URL. Optional."`
	Branch       string `json:"branch,omitempty" jsonschema:"New tracked branch. Optional."`
}

// DeleteRepositoryInput is the input for the delete_repository tool.
type DeleteRepositoryInput struct {
	RepositoryID string `json:"repository_id" jsonschema:"The wireops repository: its record id, or its exact name."`
	Confirm      string `json:"confirm" jsonschema:"The repository's exact name."`
}

// RunJobInput is the input for the run_job tool.
type RunJobInput struct {
	JobID string `json:"job_id" jsonschema:"The wireops scheduled job: its record id, or its exact name."`
}

// JobRunInput is the input for the job-run tools (cancel, delete).
type JobRunInput struct {
	RunID string `json:"run_id" jsonschema:"The job_runs record id."`
}

// DeleteJobRunInput is the input for the delete_job_run tool.
type DeleteJobRunInput struct {
	RunID   string `json:"run_id" jsonschema:"The job_runs record id."`
	Confirm string `json:"confirm" jsonschema:"The literal string 'delete', confirming the run record and its logs are discarded."`
}

// SetJobEnvVarsInput is the input for the set_job_env_vars tool.
type SetJobEnvVarsInput struct {
	JobID   string            `json:"job_id" jsonschema:"The wireops scheduled job: its record id, or its exact name."`
	Mode    string            `json:"mode" jsonschema:"'append' upserts the given keys and leaves the rest alone; 'replace' makes the job's env vars exactly this list, deleting any key not present."`
	Vars    []BulkEnvVarInput `json:"vars" jsonschema:"The env vars to write."`
	Confirm string            `json:"confirm,omitempty" jsonschema:"Required only when mode is 'replace': the job's exact name, confirming that unlisted keys get deleted."`
}

// ListJobGroupsInput is the input for the list_job_groups tool. It takes
// nothing: the route answers with a short list of group *names*, so there is
// nothing to filter or page.
type ListJobGroupsInput struct{}

// CreateScheduledJobInput is the input for the create_scheduled_job tool.
// The job's schedule, image and command live in the repository's job.yaml —
// this record only points at that file. Use generate_job_yaml to produce one.
type CreateScheduledJobInput struct {
	Name        string `json:"name" jsonschema:"Display name for the scheduled job."`
	Repository  string `json:"repository" jsonschema:"The wireops repository holding the job.yaml: its record id, or its exact name."`
	JobFile     string `json:"job_file" jsonschema:"Repository-relative path of the job.yaml, as returned by list_repo_job_files."`
	Description string `json:"description,omitempty" jsonschema:"Human-readable description. Optional."`
	Enabled     bool   `json:"enabled,omitempty" jsonschema:"Whether the cron schedule is live. Optional — defaults to false, so the job is created without running."`
}

// UpdateScheduledJobInput is the input for the update_scheduled_job tool.
// Only the fields present in the call are sent to the server.
type UpdateScheduledJobInput struct {
	JobID       string `json:"job_id" jsonschema:"The wireops scheduled job: its record id, or its exact name."`
	Name        string `json:"name,omitempty" jsonschema:"New display name. Optional."`
	Repository  string `json:"repository,omitempty" jsonschema:"New repository: its record id, or its exact name. Optional."`
	JobFile     string `json:"job_file,omitempty" jsonschema:"New repository-relative job.yaml path. Optional."`
	Description string `json:"description,omitempty" jsonschema:"New description. Optional."`
	Enabled     *bool  `json:"enabled,omitempty" jsonschema:"Enable or disable the cron schedule. Optional."`
	Status      string `json:"status,omitempty" jsonschema:"'active' or 'paused'. Optional."`
}

// DeleteScheduledJobInput is the input for the delete_scheduled_job tool.
type DeleteScheduledJobInput struct {
	JobID   string `json:"job_id" jsonschema:"The wireops scheduled job: its record id, or its exact name."`
	Confirm string `json:"confirm" jsonschema:"The job's exact name."`
}

// RevokeWorkerInput is the input for the revoke_worker tool.
type RevokeWorkerInput struct {
	WorkerID string `json:"worker_id" jsonschema:"The wireops worker: its record id, or its exact hostname."`
	Confirm  string `json:"confirm" jsonschema:"The worker's exact hostname. Revoking disconnects it and invalidates its token."`
}

// ExecInContainerInput is the input for the exec_in_container tool.
type ExecInContainerInput struct {
	StackID        string   `json:"stack_id,omitempty" jsonschema:"The wireops stack id or exact name. Alias: stack. One of stack_id or stack is required."`
	Stack          string   `json:"stack,omitempty" jsonschema:"Alias for stack_id. May be supplied together only with the same value."`
	ContainerID    string   `json:"container_id,omitempty" jsonschema:"The Docker container id as returned by get_stack_services. Alias: container_name."`
	ContainerName  string   `json:"container_name,omitempty" jsonschema:"Alias for container_id: the real Docker container name inside this stack."`
	ServiceName    string   `json:"service_name,omitempty" jsonschema:"The compose service name, resolved to a container id via get_stack_services. Alternative to container_id."`
	Command        string   `json:"command,omitempty" jsonschema:"The shell command line to run inside the container, e.g. \"cat /etc/nginx/nginx.conf\". Run through the shell below, so pipes and redirection work. Either this or argv is required."`
	Argv           []string `json:"argv,omitempty" jsonschema:"The command as an argv array, run directly with no shell in between, e.g. [\"cat\",\"/etc/nginx/nginx.conf\"]. Use this when the container has no shell, or to avoid shell quoting entirely. Alternative to command."`
	Shell          []string `json:"shell,omitempty" jsonschema:"Shell used to run command, as an argv array. Optional — defaults to ['/bin/sh']. Ignored when argv is given."`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty" jsonschema:"How long to wait for the command to finish. Optional — defaults to 30, capped at 120."`
}
