// Package protocol defines the shared WebSocket message types used for
// communication between the wireops server (control plane) and remote agents.
package protocol

// MessageType identifies the type of a WebSocket message.
type MessageType string

const (
	// Server → Agent commands
	MsgDeploy       MessageType = "deploy"
	MsgRedeploy     MessageType = "redeploy"
	MsgTeardown     MessageType = "teardown"
	MsgProbe        MessageType = "probe"
	MsgInspect      MessageType = "inspect"
	MsgGetResources MessageType = "get_resources"

	MsgDiscoverProjects MessageType = "discover_projects"
	MsgReadFile         MessageType = "read_file"
	MsgRunJob           MessageType = "run_job"
	MsgKillJob          MessageType = "kill_job"

	// Agent → Server responses/events
	MsgResult       MessageType = "result"
	MsgHeartbeat    MessageType = "heartbeat"
	MsgJobCompleted MessageType = "job_completed"
)

// Envelope wraps every WebSocket message so the receiver can inspect Type
// before deserializing the Payload.
type Envelope struct {
	Type    MessageType `json:"type"`
	Payload interface{} `json:"payload"`
}

// DeployCommand is sent from the server to an agent to run `docker compose up`.
type DeployCommand struct {
	// CommandID is a unique identifier for correlating the response.
	CommandID string `json:"command_id"`

	// StackID, CommitSHA, and Trigger are informational — used for agent-side logging.
	StackID    string `json:"stack_id"`
	CommitSHA  string `json:"commit_sha"`
	Trigger    string `json:"trigger"` // cron, manual, webhook, rollback, etc.
	QueueTotal int    `json:"queue_total,omitempty"`

	// ComposeFileB64 is the base64-encoded rendered compose YAML content.
	// The agent writes this to a temp file before running the command; it is
	// never interpolated with env vars (those are passed via cmd.Env instead).
	ComposeFileB64 string `json:"compose_file_b64"`

	// EnvVars are KEY=VALUE strings injected into the command's environment.
	// Secrets remain server-side until resolved here; they are NOT embedded
	// in the compose YAML.
	EnvVars []string `json:"env_vars"`
}

// RedeployCommand extends DeployCommand with force-recreate options.
type RedeployCommand struct {
	DeployCommand
	RecreateContainers bool `json:"recreate_containers"`
	RecreateVolumes    bool `json:"recreate_volumes"`
	RecreateNetworks   bool `json:"recreate_networks"`
}

// TeardownCommand is sent from the server to an agent to run `docker compose down`.
// The agent uses the rendered compose file so docker compose knows which containers to stop.
type TeardownCommand struct {
	// CommandID is a unique identifier for correlating the response.
	CommandID string `json:"command_id"`

	// StackID is informational, used for agent-side logging.
	StackID string `json:"stack_id"`

	// ComposeFileB64 is the base64-encoded rendered compose YAML so the agent
	// can run `docker compose -f <file> down` with the correct project context.
	ComposeFileB64 string `json:"compose_file_b64"`
}

// CommandResult is sent from the agent back to the server after executing a command.
type CommandResult struct {
	CommandID string `json:"command_id"`
	Output    string `json:"output"`
	// Error is non-empty if the command failed.
	Error string `json:"error,omitempty"`
}

// ProbeCommand is sent from the server to an agent to check whether a compose
// project already has running or stopped containers. The agent runs
// `docker compose ps` and responds with a CommandResult whose Output is a
// JSON-encoded ProbeResult.
type ProbeCommand struct {
	// CommandID correlates the response.
	CommandID string `json:"command_id"`

	// StackID is informational (used for logging on the agent side).
	StackID string `json:"stack_id"`

	// ComposeFileB64 is the base64-encoded compose YAML; the agent writes it
	// temporarily so `docker compose ps` can resolve the project name correctly.
	ComposeFileB64 string `json:"compose_file_b64"`
}

// ProbeResult is the JSON payload inside CommandResult.Output after a ProbeCommand.
type ProbeResult struct {
	// ContainerCount is the total number of containers (any state) found for the project.
	ContainerCount int `json:"container_count"`

	// Services lists the names of services that have containers on this host.
	Services []string `json:"services,omitempty"`
}

// InspectCommand queries the agent for the currently running commit SHA
// from the running container's wireops.commit label.
type InspectCommand struct {
	CommandID string `json:"command_id"`
	StackID   string `json:"stack_id"`
}

// InspectResult is the JSON payload inside CommandResult.Output after an InspectCommand.
type InspectResult struct {
	CommitSHA string `json:"commit_sha"`
}

// GetResourcesCommand is sent from the server to an agent to query Docker volumes
// and networks associated with a compose project.
type GetResourcesCommand struct {
	// CommandID correlates the response.
	CommandID string `json:"command_id"`

	// StackID is informational (used for logging on the agent side).
	StackID string `json:"stack_id"`

	// ProjectName is the Docker Compose project name, derived from the workdir basename.
	ProjectName string `json:"project_name"`
}

// VolumeInfo describes a Docker volume associated with a compose project.
type VolumeInfo struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
	Scope      string `json:"scope"`
}

// NetworkInfo describes a Docker network associated with a compose project.
type NetworkInfo struct {
	Name    string `json:"name"`
	Driver  string `json:"driver"`
	Scope   string `json:"scope"`
	Subnet  string `json:"subnet,omitempty"`
	Gateway string `json:"gateway,omitempty"`
}

// GetResourcesResult is the JSON payload inside CommandResult.Output after a GetResourcesCommand.
type GetResourcesResult struct {
	Volumes  []VolumeInfo  `json:"volumes"`
	Networks []NetworkInfo `json:"networks"`
}

// DiscoverProjectsCommand asks the agent to list Docker Compose projects on this
// host that are not managed by wireops (containers without the wireops.managed=true label).
type DiscoverProjectsCommand struct {
	CommandID string `json:"command_id"`
}

// DiscoveredProject describes a single compose project found on the agent host.
type DiscoveredProject struct {
	ProjectName string   `json:"project_name"`
	ComposePath string   `json:"compose_path"` // from com.docker.compose.project.working_dir label
	Services    []string `json:"services"`
}

// DiscoverProjectsResult is the JSON payload inside CommandResult.Output after
// a DiscoverProjectsCommand.
type DiscoverProjectsResult struct {
	Projects []DiscoveredProject `json:"projects"`
}

// ReadFileCommand asks the agent to read a file from its local filesystem and
// return the raw content as base64-encoded bytes. Only .yml/.yaml files are permitted.
type ReadFileCommand struct {
	CommandID string `json:"command_id"`
	Path      string `json:"path"`
}

// RunJobCommand is sent from the server to an agent to execute a one-shot
// docker run job. The agent starts the container asynchronously and immediately
// acks via CommandResult; the final outcome arrives as a JobCompletedMessage.
type RunJobCommand struct {
	// CommandID correlates the immediate ack response.
	CommandID string `json:"command_id"`

	// JobRunID is the PocketBase record ID for the job_run being executed.
	JobRunID string `json:"job_run_id"`

	// Image is the Docker image to run (e.g. "alpine:latest").
	Image string `json:"image"`

	// Command is the command + args passed after the image.
	Command []string `json:"command"`

	// Env holds KEY=VALUE pairs injected into the container environment.
	Env map[string]string `json:"env,omitempty"`

	// RepositoryID is used to inject the dev.wireops.repository.id label.
	RepositoryID string `json:"repository_id,omitempty"`

	// RepositoryBranch is used to inject the dev.wireops.repository.branch label.
	RepositoryBranch string `json:"repository_branch,omitempty"`

	// RepositoryFile is used to inject the dev.wireops.repository.file label.
	RepositoryFile string `json:"repository_file,omitempty"`

	// CommitSHA is used to inject the dev.wireops.repository.commit_sha label.
	CommitSHA string `json:"commit_sha,omitempty"`

	// JobName is used to inject the dev.wireops.job.name label.
	JobName string `json:"job_name,omitempty"`

	// Volumes are passed as -v to docker run (e.g. host:container or host:container:ro).
	Volumes []string `json:"volumes,omitempty"`

	// Network is the name of an existing Docker network; passed as --network <name>.
	Network string `json:"network,omitempty"`
}

// KillJobCommand tells the agent to stop a running job container.
// The agent runs `docker stop wireops-job-<JobRunID>`. The container exit will
// trigger the existing RunJob completion flow and send JobCompletedMessage.
type KillJobCommand struct {
	CommandID string `json:"command_id"`
	JobRunID  string `json:"job_run_id"`
}

// JobCompletedMessage is sent from the agent to the server when a job container
// exits. It is an unsolicited push message, not a reply to a specific command.
type JobCompletedMessage struct {
	// JobRunID matches the job_run record on the server.
	JobRunID string `json:"job_run_id"`

	// Success is true when the container exited with code 0.
	Success bool `json:"success"`

	// Output is the combined stdout+stderr from the container.
	Output string `json:"output"`

	// DurationMs is the wall-clock time from container start to exit.
	DurationMs int64 `json:"duration_ms"`
}

// HeartbeatPayload is the optional body sent inside a MsgHeartbeat envelope.
// ActiveJobRunIDs lists the job_run IDs whose containers are still running on
// this agent — the server uses this as a liveness signal.
type HeartbeatPayload struct {
	ActiveJobRunIDs []string `json:"active_job_run_ids,omitempty"`
}
