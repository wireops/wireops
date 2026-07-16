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
