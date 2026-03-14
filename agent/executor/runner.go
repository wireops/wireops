// Package executor handles the execution of deploy commands received from the server.
package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/docker"
	"github.com/wireops/wireops/internal/protocol"
)

// tempDir is where the agent writes transient rendered compose files.
var tempDir = func() string {
	d := os.Getenv("WIREOPS_AGENT_WORK_DIR")
	if d == "" {
		d = "/tmp/wireops-agent"
	}
	return d
}()

// Deploy decodes the base64 compose file, writes it to a temp file, and runs
// `docker compose up`. Environment variables are passed via cmd.Env, never
// interpolated into the YAML.
func Deploy(ctx context.Context, cmd protocol.DeployCommand) protocol.CommandResult {
	trigger := cmd.Trigger
	if trigger == "" {
		trigger = "unknown"
	}
	if cmd.QueueTotal > 0 {
		log.Printf("[executor] deploy start stack=%s trigger=%s command=%s queue_total=%d (pulling %d jobs from the queue)", cmd.StackID, trigger, cmd.CommandID, cmd.QueueTotal, cmd.QueueTotal)
	} else {
		log.Printf("[executor] deploy start stack=%s trigger=%s command=%s", cmd.StackID, trigger, cmd.CommandID)
	}
	start := time.Now()

	workDir, composeFile, cleanup, err := prepareComposeFile(cmd.CommandID, cmd.ComposeFileB64)
	if err != nil {
		log.Printf("[executor] deploy error stack=%s trigger=%s: %v", cmd.StackID, trigger, err)
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer cleanup()

	output, runErr := compose.RunUp(ctx, compose.RunOptions{
		WorkDir:     workDir,
		ComposeFile: composeFile,
		EnvVars:     cmd.EnvVars,
	})

	result := protocol.CommandResult{CommandID: cmd.CommandID, Output: output}
	if runErr != nil {
		result.Error = runErr.Error()
		log.Printf("[executor] deploy error stack=%s trigger=%s elapsed=%dms: %v", cmd.StackID, trigger, time.Since(start).Milliseconds(), runErr)
	} else {
		log.Printf("[executor] deploy done stack=%s trigger=%s elapsed=%dms", cmd.StackID, trigger, time.Since(start).Milliseconds())
	}
	return result
}

// Redeploy is like Deploy but with force-recreate options.
func Redeploy(ctx context.Context, cmd protocol.RedeployCommand) protocol.CommandResult {
	trigger := cmd.Trigger
	if trigger == "" {
		trigger = "force-redeploy"
	}
	if cmd.QueueTotal > 0 {
		log.Printf("[executor] redeploy start stack=%s trigger=%s command=%s recreate_containers=%v recreate_volumes=%v recreate_networks=%v queue_total=%d (pulling %d jobs from the queue)",
			cmd.StackID, trigger, cmd.CommandID, cmd.RecreateContainers, cmd.RecreateVolumes, cmd.RecreateNetworks, cmd.QueueTotal, cmd.QueueTotal)
	} else {
		log.Printf("[executor] redeploy start stack=%s trigger=%s command=%s recreate_containers=%v recreate_volumes=%v recreate_networks=%v",
			cmd.StackID, trigger, cmd.CommandID, cmd.RecreateContainers, cmd.RecreateVolumes, cmd.RecreateNetworks)
	}
	start := time.Now()

	workDir, composeFile, cleanup, err := prepareComposeFile(cmd.CommandID, cmd.ComposeFileB64)
	if err != nil {
		log.Printf("[executor] redeploy error stack=%s trigger=%s: %v", cmd.StackID, trigger, err)
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer cleanup()

	output, runErr := compose.RunForceUp(ctx, compose.ForceUpOptions{
		RunOptions: compose.RunOptions{
			WorkDir:     workDir,
			ComposeFile: composeFile,
			EnvVars:     cmd.EnvVars,
		},
		RecreateContainers: cmd.RecreateContainers,
		RecreateVolumes:    cmd.RecreateVolumes,
		RecreateNetworks:   cmd.RecreateNetworks,
	})

	result := protocol.CommandResult{CommandID: cmd.CommandID, Output: output}
	if runErr != nil {
		result.Error = runErr.Error()
		log.Printf("[executor] redeploy error stack=%s trigger=%s elapsed=%dms: %v", cmd.StackID, trigger, time.Since(start).Milliseconds(), runErr)
	} else {
		log.Printf("[executor] redeploy done stack=%s trigger=%s elapsed=%dms", cmd.StackID, trigger, time.Since(start).Milliseconds())
	}
	return result
}

// Teardown decodes the base64 compose file and runs `docker compose down --remove-orphans`
// to cleanly stop and remove all containers for the stack.
func Teardown(ctx context.Context, cmd protocol.TeardownCommand) protocol.CommandResult {
	log.Printf("[executor] teardown start stack=%s command=%s", cmd.StackID, cmd.CommandID)
	start := time.Now()

	workDir, composeFile, cleanup, err := prepareComposeFile(cmd.CommandID, cmd.ComposeFileB64)
	if err != nil {
		log.Printf("[executor] teardown error stack=%s: %v", cmd.StackID, err)
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer cleanup()

	output, runErr := compose.RunDown(ctx, compose.RunOptions{
		WorkDir:     workDir,
		ComposeFile: composeFile,
	})

	result := protocol.CommandResult{CommandID: cmd.CommandID, Output: output}
	if runErr != nil {
		result.Error = runErr.Error()
		log.Printf("[executor] teardown error stack=%s elapsed=%dms: %v", cmd.StackID, time.Since(start).Milliseconds(), runErr)
	} else {
		log.Printf("[executor] teardown done stack=%s elapsed=%dms", cmd.StackID, time.Since(start).Milliseconds())
	}
	return result
}

// Probe checks whether a compose project already has containers (any state) on
// this host by running `docker compose ps`. The result is returned as a
// JSON-encoded protocol.ProbeResult inside CommandResult.Output.
func Probe(ctx context.Context, cmd protocol.ProbeCommand) protocol.CommandResult {
	log.Printf("[executor] probe start stack=%s command=%s", cmd.StackID, cmd.CommandID)
	start := time.Now()

	workDir, composeFile, cleanup, err := prepareComposeFile(cmd.CommandID, cmd.ComposeFileB64)
	if err != nil {
		log.Printf("[executor] probe error stack=%s: %v", cmd.StackID, err)
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer cleanup()

	services, psErr := compose.RunPs(ctx, compose.RunOptions{
		WorkDir:     workDir,
		ComposeFile: composeFile,
	})
	if psErr != nil {
		// Treat ps errors as "nothing found" so a network hiccup doesn't block transfers.
		log.Printf("[executor] probe ps error stack=%s (treating as empty): %v", cmd.StackID, psErr)
		services = nil
	}

	probeResult := protocol.ProbeResult{
		ContainerCount: len(services),
		Services:       services,
	}
	log.Printf("[executor] probe done stack=%s containers=%d services=%v elapsed=%dms",
		cmd.StackID, probeResult.ContainerCount, probeResult.Services, time.Since(start).Milliseconds())

	encoded, _ := json.Marshal(probeResult)
	return protocol.CommandResult{CommandID: cmd.CommandID, Output: string(encoded)}
}

// Inspect runs `docker ps --filter label=dev.wireops.stack_id=<StackID> --format '{{.Label "dev.wireops.repository.commit_sha"}}'`
// and extract the running commit SHA directly from the container label.
func Inspect(ctx context.Context, cmd protocol.InspectCommand) protocol.CommandResult {
	// log.Printf("[executor] inspect start stack=%s command=%s", cmd.StackID, cmd.CommandID)
	// start := time.Now()

	client, err := docker.NewClient()
	if err != nil {
		// log.Printf("[executor] inspect error initializing docker client stack=%s: %v", cmd.StackID, err)
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer client.Close()

	result, err := client.GetRunningStackCommit(ctx, cmd.StackID)

	if err != nil {
		// log.Printf("[executor] inspect error reading container labels stack=%s: %v", cmd.StackID, err)
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}

	inspectResult := protocol.InspectResult{
		CommitSHA: result,
	}

	encoded, _ := json.Marshal(inspectResult)

	// log.Printf("[executor] inspect done stack=%s commit=%s elapsed=%dms", cmd.StackID, inspectResult.CommitSHA, time.Since(start).Milliseconds())

	return protocol.CommandResult{CommandID: cmd.CommandID, Output: string(encoded)}
}

// GetResources queries Docker for volumes and networks belonging to the given
// compose project and returns a JSON-encoded GetResourcesResult.
func GetResources(ctx context.Context, cmd protocol.GetResourcesCommand) protocol.CommandResult {
	log.Printf("[executor] get_resources start stack=%s project=%s command=%s", cmd.StackID, cmd.ProjectName, cmd.CommandID)

	client, err := docker.NewClient()
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer client.Close()

	volumes, err := compose.GetStackVolumes(ctx, client.Raw(), cmd.ProjectName)
	if err != nil {
		log.Printf("[executor] get_resources volumes error stack=%s: %v", cmd.StackID, err)
		volumes = []protocol.VolumeInfo{}
	}

	networks, err := compose.GetStackNetworks(ctx, client.Raw(), cmd.ProjectName)
	if err != nil {
		log.Printf("[executor] get_resources networks error stack=%s: %v", cmd.StackID, err)
		networks = []protocol.NetworkInfo{}
	}

	result := protocol.GetResourcesResult{Volumes: volumes, Networks: networks}
	encoded, _ := json.Marshal(result)

	log.Printf("[executor] get_resources done stack=%s volumes=%d networks=%d", cmd.StackID, len(volumes), len(networks))
	return protocol.CommandResult{CommandID: cmd.CommandID, Output: string(encoded)}
}

// GetStatus queries the agent for live container statuses and labels for a project.
func GetStatus(ctx context.Context, cmd protocol.GetStatusCommand) protocol.CommandResult {
	log.Printf("[executor] get_status start project=%s command=%s", cmd.ProjectName, cmd.CommandID)

	cli, err := docker.NewClient()
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer cli.Close()

	statuses, err := compose.GetStackStatus(ctx, cli.Raw(), cmd.ProjectName)
	if err != nil {
		log.Printf("[executor] get_status error project=%s: %v", cmd.ProjectName, err)
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}

	encoded, err := json.Marshal(statuses)
	if err != nil {
		log.Printf("[executor] get_status json marshal error project=%s: %v", cmd.ProjectName, err)
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}

	log.Printf("[executor] get_status done project=%s services=%d", cmd.ProjectName, len(statuses))
	return protocol.CommandResult{CommandID: cmd.CommandID, Output: string(encoded)}
}

// DiscoverProjects lists Docker Compose projects on this host that are not managed
// by wireops (containers without the wireops.managed=true label). Results are grouped
// by project name and returned as JSON-encoded DiscoverProjectsResult.
func DiscoverProjects(ctx context.Context, cmd protocol.DiscoverProjectsCommand) protocol.CommandResult {
	log.Printf("[executor] discover_projects command=%s", cmd.CommandID)

	cli, err := docker.NewClient()
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer cli.Close()

	f := filters.NewArgs()
	f.Add("label", "com.docker.compose.project")

	containers, err := cli.Raw().ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: f,
	})
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}

	projects := make(map[string]*protocol.DiscoveredProject)
	for _, cnt := range containers {
		if cnt.Labels["dev.wireops.managed"] == "true" {
			continue
		}
		projectName := cnt.Labels["com.docker.compose.project"]
		if projectName == "" {
			continue
		}
		if _, exists := projects[projectName]; !exists {
			projects[projectName] = &protocol.DiscoveredProject{
				ProjectName: projectName,
				ComposePath: cnt.Labels["com.docker.compose.project.working_dir"],
				Services:    []string{},
			}
		}
		svcName := cnt.Labels["com.docker.compose.service"]
		if svcName == "" {
			continue
		}
		proj := projects[projectName]
		found := false
		for _, s := range proj.Services {
			if s == svcName {
				found = true
				break
			}
		}
		if !found {
			proj.Services = append(proj.Services, svcName)
		}
	}

	result := protocol.DiscoverProjectsResult{Projects: make([]protocol.DiscoveredProject, 0, len(projects))}
	for _, p := range projects {
		result.Projects = append(result.Projects, *p)
	}

	encoded, _ := json.Marshal(result)
	log.Printf("[executor] discover_projects done command=%s projects=%d", cmd.CommandID, len(result.Projects))
	return protocol.CommandResult{CommandID: cmd.CommandID, Output: string(encoded)}
}

// ReadFile reads a .yml/.yaml file from the agent's local filesystem and returns
// its content as base64-encoded bytes inside CommandResult.Output.
func ReadFile(_ context.Context, cmd protocol.ReadFileCommand) protocol.CommandResult {
	log.Printf("[executor] read_file command=%s path=%s", cmd.CommandID, cmd.Path)

	if !filepath.IsAbs(cmd.Path) {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: "path must be absolute"}
	}
	ext := strings.ToLower(filepath.Ext(cmd.Path))
	if ext != ".yml" && ext != ".yaml" {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: "only .yml/.yaml files are allowed"}
	}

	data, err := os.ReadFile(cmd.Path)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}

	log.Printf("[executor] read_file done command=%s bytes=%d", cmd.CommandID, len(data))
	return protocol.CommandResult{CommandID: cmd.CommandID, Output: base64.StdEncoding.EncodeToString(data)}
}

// JobSendFunc is the callback the executor uses to push messages back to the server.
type JobSendFunc func(msgType protocol.MessageType, payload interface{})

// RunJob starts a one-shot `docker run` container asynchronously. It returns an
// immediate ack CommandResult so the server's Dispatch call unblocks quickly.
// When the container exits, it invokes send with a JobCompletedMessage so the
// server can update the job_run record — without ever holding the WebSocket open
// for the entire duration of the container.
func RunJob(cmd protocol.RunJobCommand, send JobSendFunc) protocol.CommandResult {
	log.Printf("[executor] run_job dispatched job_run=%s image=%s command=%s", cmd.JobRunID, cmd.Image, cmd.CommandID)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		start := time.Now()
		args := cmd.BuildDockerRunArgs()
		out, runErr := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		output := string(out)

		elapsed := time.Since(start).Milliseconds()
		success := runErr == nil

		if !success {
			if output != "" {
				output += "\n"
			}
			output += runErr.Error()
			log.Printf("[executor] run_job error job_run=%s elapsed=%dms: %v", cmd.JobRunID, elapsed, runErr)
		} else {
			log.Printf("[executor] run_job done job_run=%s elapsed=%dms", cmd.JobRunID, elapsed)
		}

		send(protocol.MsgJobCompleted, protocol.JobCompletedMessage{
			JobRunID:   cmd.JobRunID,
			Success:    success,
			Output:     output,
			DurationMs: elapsed,
		})
	}()

	// Ack: container is starting; the caller should not block waiting for completion.
	return protocol.CommandResult{CommandID: cmd.CommandID, Output: "started"}
}

// KillJob stops a running job container via docker stop.
// The container exit will be observed by the RunJob goroutine, which will send
// JobCompletedMessage. This command returns immediately after issuing docker stop.
func KillJob(cmd protocol.KillJobCommand) protocol.CommandResult {
	containerName := "wireops-job-" + cmd.JobRunID

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "stop", containerName).CombinedOutput()
	if err != nil {
		output := string(out)
		if ctx.Err() == context.DeadlineExceeded {
			output += "\nTimeout reached stopping container " + containerName
		}
		if output != "" {
			output += "\n"
		}
		output += err.Error()
		log.Printf("[executor] kill_job failed job_run=%s: %v", cmd.JobRunID, err)
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: output}
	}
	log.Printf("[executor] kill_job sent job_run=%s", cmd.JobRunID)
	return protocol.CommandResult{CommandID: cmd.CommandID, Output: "stopped"}
}



// prepareComposeFile decodes the base64 YAML, writes it to a temporary directory,
// and returns (workDir, filename, cleanupFn, error).
func prepareComposeFile(commandID, b64Content string) (string, string, func(), error) {
	content, err := base64.StdEncoding.DecodeString(b64Content)
	if err != nil {
		return "", "", func() {}, fmt.Errorf("failed to decode compose file: %w", err)
	}

	// Ensure the parent temporary directory exists.
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		return "", "", func() {}, fmt.Errorf("failed to create temp parent dir: %w", err)
	}

	// Create a unique temporary directory under tempDir.
	// We sanitize commandID using filepath.Base to ensure it doesn't contain path separators,
	// which MkdirTemp would reject.
	dir, err := os.MkdirTemp(tempDir, "cmd-"+filepath.Base(commandID)+"-*")
	if err != nil {
		return "", "", func() {}, fmt.Errorf("failed to create secure work dir: %w", err)
	}

	filename := "docker-compose.yml"
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, content, 0600); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", func() {}, fmt.Errorf("failed to write compose file: %w", err)
	}

	cleanup := func() { _ = os.RemoveAll(dir) }
	return dir, filename, cleanup, nil
}
