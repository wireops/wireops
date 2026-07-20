// Package models holds the JSON-tagged structs exchanged between MCP tool
// handlers and the caller — the input schema for each tool call.
package models

// ListStacksInput is the input for the list_stacks tool.
type ListStacksInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"Maximum number of stacks to return (default 50)."`
}

// StackIDInput is the input for tools that operate on a single stack.
type StackIDInput struct {
	StackID string `json:"stack_id" jsonschema:"The wireops stack record id."`
}

// SyncLogsInput is the input for the get_sync_logs tool.
type SyncLogsInput struct {
	StackID string `json:"stack_id" jsonschema:"The wireops stack record id."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum number of log entries to return (default 20)."`
}

// ContainerLogsInput is the input for the get_container_logs tool.
type ContainerLogsInput struct {
	StackID     string `json:"stack_id" jsonschema:"The wireops stack record id."`
	ContainerID string `json:"container_id" jsonschema:"The Docker container id or name, as returned by get_stack_services."`
	Tail        string `json:"tail,omitempty" jsonschema:"Number of trailing log lines to fetch (default 100)."`
}

// ListJobsInput is the input for the list_jobs tool.
type ListJobsInput struct{}

// JobIDInput is the input for tools that operate on a single scheduled job.
type JobIDInput struct {
	JobID string `json:"job_id" jsonschema:"The wireops scheduled job record id."`
}

// RepositoryIDInput is the input for tools that operate on a single repository.
type RepositoryIDInput struct {
	RepositoryID string `json:"repository_id" jsonschema:"The wireops repository record id."`
}

// ListWorkersInput is the input for the list_workers tool.
type ListWorkersInput struct{}

// WorkerIDInput is the input for tools that operate on a single worker.
type WorkerIDInput struct {
	WorkerID string `json:"worker_id" jsonschema:"The wireops worker record id."`
}

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
	Wireops  GenerateWireopsYAMLInput `json:"wireops" jsonschema:"wireops.yaml fields for this stack. 'name' inside this object is optional and defaults to the top-level 'name'."`
	Services []ComposeServiceInput    `json:"services" jsonschema:"The docker-compose services to generate (required, at least one)."`
	WorkerID string                   `json:"worker_id,omitempty" jsonschema:"If set, validates the generated compose file against this worker's effective deploy security policy before returning."`
}
