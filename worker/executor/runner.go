// Package executor handles the execution of deploy commands received from the server.
package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/docker"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/safepath"
)

// stackDir is the root directory under which per-stack compose work dirs are created.
// Defaults to $HOME/.wireops and can be overridden via WORKER_STACK_DIR.
// The supplied path must be absolute and must not contain ".." segments;
// invalid values are rejected with a warning and the default is used instead.
var stackDir = func() string {
	defaultDir := getSecureDefaultStackDir()
	d := strings.TrimSpace(os.Getenv("WORKER_STACK_DIR"))
	if d == "" {
		return defaultDir
	}
	cleaned := filepath.Clean(d)
	if !filepath.IsAbs(cleaned) {
		log.Printf("[worker] WORKER_STACK_DIR %q is not an absolute path — using default %s", d, defaultDir)
		return defaultDir
	}
	if strings.Contains(cleaned, "..") {
		log.Printf("[worker] WORKER_STACK_DIR %q contains invalid traversal — using default %s", d, defaultDir)
		return defaultDir
	}
	return cleaned
}()

// runInWorkDir encapsulates the work directory preparation, logger error handling, and deferred cleanup
// for commands that execute in a temporary compose workspace. onLine, if non-nil, is wrapped so every
// line handed to fn's compose runner has secrets redacted before it reaches the caller (used for live
// streaming); the final joined output and error are redacted the same way.
func runInWorkDir(stackID, commandID, composeFileB64, envFileB64 string, action string, onLine func(string), fn func(workDir, composeFile string, onLine func(string)) (string, error)) (string, error) {
	workDir, composeFile, cleanup, secrets, err := prepareWorkDir(stackID, commandID, composeFileB64, envFileB64)
	if err != nil {
		log.Printf("[executor] %s error stack=%s: %v", action, stackID, err)
		return "", err
	}
	defer cleanup()

	var wrappedOnLine func(string)
	if onLine != nil {
		wrappedOnLine = func(line string) { onLine(redactSecrets(line, secrets)) }
	}

	output, runErr := fn(workDir, composeFile, wrappedOnLine)
	output = redactSecrets(output, secrets)
	if runErr != nil {
		runErr = errors.New(redactSecrets(runErr.Error(), secrets))
	}
	return output, runErr
}

// Deploy decodes the base64 compose file, writes it to a temp file, and runs
// `docker compose up`. Environment variables are passed via cmd.Env, never
// interpolated into the YAML. If EnvFileB64 is set, a .env file is also written
// to the work directory before the compose command runs.
func Deploy(ctx context.Context, cmd protocol.DeployCommand, onLine func(string)) protocol.CommandResult {
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

	output, runErr := runInWorkDir(cmd.StackID, cmd.CommandID, cmd.ComposeFileB64, cmd.EnvFileB64, "deploy", onLine, func(workDir, composeFile string, wrappedOnLine func(string)) (string, error) {
		return compose.RunUp(ctx, compose.RunOptions{
			WorkDir:       workDir,
			ComposeFile:   composeFile,
			ForcePull:     cmd.ForcePull,
			RemoveOrphans: cmd.RemoveOrphans,
			OnLine:        wrappedOnLine,
		})
	})

	elapsed := time.Since(start).Milliseconds()
	result := protocol.CommandResult{CommandID: cmd.CommandID, Output: output, ComposeUpMs: elapsed}
	if runErr != nil {
		result.Error = runErr.Error()
		log.Printf("[executor] deploy error stack=%s trigger=%s elapsed=%dms: %v", cmd.StackID, trigger, elapsed, runErr)
	} else {
		log.Printf("[executor] deploy done stack=%s trigger=%s elapsed=%dms", cmd.StackID, trigger, elapsed)
	}
	return result
}

// Redeploy is like Deploy but with force-recreate options.
func Redeploy(ctx context.Context, cmd protocol.RedeployCommand, onLine func(string)) protocol.CommandResult {
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

	output, runErr := runInWorkDir(cmd.StackID, cmd.CommandID, cmd.ComposeFileB64, cmd.EnvFileB64, "redeploy", onLine, func(workDir, composeFile string, wrappedOnLine func(string)) (string, error) {
		return compose.RunForceUp(ctx, compose.ForceUpOptions{
			RunOptions: compose.RunOptions{
				WorkDir:       workDir,
				ComposeFile:   composeFile,
				ForcePull:     cmd.ForcePull,
				RemoveOrphans: cmd.RemoveOrphans,
				OnLine:        wrappedOnLine,
			},
			RecreateContainers: cmd.RecreateContainers,
			RecreateVolumes:    cmd.RecreateVolumes,
			RecreateNetworks:   cmd.RecreateNetworks,
		})
	})

	elapsed := time.Since(start).Milliseconds()
	result := protocol.CommandResult{CommandID: cmd.CommandID, Output: output, ComposeUpMs: elapsed}
	if runErr != nil {
		result.Error = runErr.Error()
		log.Printf("[executor] redeploy error stack=%s trigger=%s elapsed=%dms: %v", cmd.StackID, trigger, elapsed, runErr)
	} else {
		log.Printf("[executor] redeploy done stack=%s trigger=%s elapsed=%dms", cmd.StackID, trigger, elapsed)
	}
	return result
}

// Teardown decodes the base64 compose file and runs `docker compose down --remove-orphans`
// to cleanly stop and remove all containers for the stack.
func Teardown(ctx context.Context, cmd protocol.TeardownCommand, onLine func(string)) protocol.CommandResult {
	log.Printf("[executor] teardown start stack=%s command=%s", cmd.StackID, cmd.CommandID)
	start := time.Now()

	output, runErr := runInWorkDir(cmd.StackID, cmd.CommandID, cmd.ComposeFileB64, cmd.EnvFileB64, "teardown", onLine, func(workDir, composeFile string, wrappedOnLine func(string)) (string, error) {
		return compose.RunDown(ctx, compose.RunOptions{
			WorkDir:     workDir,
			ComposeFile: composeFile,
			OnLine:      wrappedOnLine,
		})
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

	var services []string
	_, runErr := runInWorkDir(cmd.StackID, cmd.CommandID, cmd.ComposeFileB64, cmd.EnvFileB64, "probe", nil, func(workDir, composeFile string, _ func(string)) (string, error) {
		var psErr error
		services, psErr = compose.RunPs(ctx, compose.RunOptions{
			WorkDir:     workDir,
			ComposeFile: composeFile,
		})
		if psErr != nil {
			// Treat ps errors as "nothing found" so a network hiccup doesn't block transfers.
			log.Printf("[executor] probe ps error stack=%s (treating as empty): %v", cmd.StackID, psErr)
			services = nil
		}
		return "", nil
	})

	if runErr != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: runErr.Error()}
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

// StopContainer stops a container on the worker host after confirming it
// belongs to the requested compose project.
func StopContainer(ctx context.Context, cmd protocol.ContainerActionCommand) protocol.CommandResult {
	return executeContainerAction(ctx, cmd, "stop")
}

// RestartContainer restarts a container on the worker host after confirming it
// belongs to the requested compose project.
func RestartContainer(ctx context.Context, cmd protocol.ContainerActionCommand) protocol.CommandResult {
	return executeContainerAction(ctx, cmd, "restart")
}

// verifyContainerAndGetClient validates that the container belongs to the specified project and returns the docker client.
// The caller is responsible for calling defer cli.Close() if no error is returned.
func verifyContainerAndGetClient(ctx context.Context, containerID, projectName string) (*docker.Client, error) {
	cli, err := docker.NewClient()
	if err != nil {
		return nil, err
	}

	belongs, err := compose.ContainerBelongsToProject(ctx, cli.Raw(), containerID, projectName)
	if err != nil {
		cli.Close()
		return nil, err
	}
	if !belongs {
		cli.Close()
		return nil, errors.New("container does not belong to stack")
	}

	return cli, nil
}

func executeContainerAction(ctx context.Context, cmd protocol.ContainerActionCommand, action string) protocol.CommandResult {
	log.Printf("[executor] %s_container start stack=%s project=%s container=%s command=%s", action, cmd.StackID, cmd.ProjectName, cmd.ContainerID, cmd.CommandID)

	cli, err := verifyContainerAndGetClient(ctx, cmd.ContainerID, cmd.ProjectName)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer cli.Close()

	timeout := 10
	var actionErr error
	var output string
	switch action {
	case "stop":
		actionErr = cli.Raw().ContainerStop(ctx, cmd.ContainerID, container.StopOptions{Timeout: &timeout})
		output = "stopped"
	case "restart":
		actionErr = cli.Raw().ContainerRestart(ctx, cmd.ContainerID, container.StopOptions{Timeout: &timeout})
		output = "restarted"
	default:
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: "invalid container action"}
	}

	if actionErr != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: actionErr.Error()}
	}

	return protocol.CommandResult{CommandID: cmd.CommandID, Output: output}
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

	if err := safepath.ValidateHostPath(cmd.Path); err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	cleanedPath := filepath.Clean(cmd.Path)
	ext := strings.ToLower(filepath.Ext(cleanedPath))
	if ext != ".yml" && ext != ".yaml" {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: "only .yml/.yaml files are allowed"}
	}

	resolvedPath, err := filepath.EvalSymlinks(cleanedPath)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}

	if !isAllowedPath(resolvedPath) {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: "access to the requested path is denied by worker policy"}
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}

	log.Printf("[executor] read_file done command=%s bytes=%d", cmd.CommandID, len(data))
	return protocol.CommandResult{CommandID: cmd.CommandID, Output: base64.StdEncoding.EncodeToString(data)}
}

func isSubpath(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if parent == child {
		return true
	}
	if !strings.HasSuffix(parent, string(filepath.Separator)) {
		parent += string(filepath.Separator)
	}
	return strings.HasPrefix(child, parent)
}

func resolvePathSymlinks(path string) string {
	cleaned := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err == nil {
		return resolved
	}
	dir := cleaned
	var suffix []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		suffix = append([]string{filepath.Base(dir)}, suffix...)
		dir = parent
		if resolvedDir, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{resolvedDir}, suffix...)...)
		}
	}
	return cleaned
}

func isAllowedPath(path string) bool {
	if err := safepath.ValidateHostPath(path); err != nil {
		return false
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return false
	}

	resolved := resolvePathSymlinks(cleaned)

	// 1. Always allow inside stackDir
	if isSubpath(resolvePathSymlinks(stackDir), resolved) {
		return true
	}

	// 2. Allow if inside WORKER_ALLOWED_IMPORT_DIRS if configured
	if allowedEnv := os.Getenv("WORKER_ALLOWED_IMPORT_DIRS"); allowedEnv != "" {
		dirs := strings.Split(allowedEnv, ",")
		for _, dir := range dirs {
			dir = strings.TrimSpace(dir)
			if dir == "" {
				continue
			}
			if isSubpath(resolvePathSymlinks(dir), resolved) {
				return true
			}
		}
		// If WORKER_ALLOWED_IMPORT_DIRS is set, we strictly enforce it
		return false
	}

	// 3. Fallback blocklist: block known sensitive system directories
	sensitiveRoots := []string{
		"/etc",
		"/root",
		"/var/run",
		"/run",
		"/proc",
		"/sys",
		"/boot",
		"/dev",
	}
	for _, root := range sensitiveRoots {
		if isSubpath(resolvePathSymlinks(root), resolved) {
			return false
		}
	}

	return true
}

// RunJob starts a one-shot `docker run` container and blocks until it completes.
// It returns a JobCompletedMessage with the final result.
func RunJob(ctx context.Context, cmd protocol.RunJobCommand) protocol.JobCompletedMessage {
	log.Printf("[executor] run_job dispatched job_run=%s image=%s command=%s", cmd.JobRunID, cmd.Image, cmd.CommandID)

	// Validate volume host paths for traversal / relative paths only.
	// Policy enforcement (allowlists, image restrictions, etc.) is the server's
	// responsibility (jobscheduler/dispatchToWorker) — the worker must not
	// duplicate it with hardcoded deny-lists.
	if err := validateVolumePaths(cmd.Volumes); err != nil {
		errMsg := fmt.Sprintf("failed to start job, %v", err)
		log.Printf("[executor] run_job path error: %s", errMsg)
		return protocol.JobCompletedMessage{
			JobRunID: cmd.JobRunID,
			Success:  false,
			Output:   errMsg,
		}
	}

	configVolumes, cleanupConfigs, err := prepareJobConfigFiles(cmd.JobRunID, cmd.ConfigFiles)
	if err != nil {
		errMsg := fmt.Sprintf("failed to stage config files, %v", err)
		log.Printf("[executor] run_job config staging error: %s", errMsg)
		return protocol.JobCompletedMessage{
			JobRunID: cmd.JobRunID,
			Success:  false,
			Output:   errMsg,
		}
	}
	defer cleanupConfigs()
	// Config bind mounts are worker-materialized paths from server-resolved,
	// git-sourced content — not user-supplied host paths — so they bypass the
	// server-side volume allowlist (policy.ValidateVolumes) the same way
	// cmd.Volumes was already checked against above; they're appended after
	// that check runs.
	cmd.Volumes = append(cmd.Volumes, configVolumes...)

	timeout := 10 * time.Minute
	if cmd.TimeoutSeconds > 0 {
		timeout = time.Duration(cmd.TimeoutSeconds) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	args := cmd.BuildDockerRunArgs()
	dockerPath, err := lookPathSecure("docker")
	if err != nil {
		return protocol.JobCompletedMessage{
			JobRunID:   cmd.JobRunID,
			Success:    false,
			Output:     "failed to find docker binary: " + err.Error(),
			DurationMs: 0,
		}
	}
	runCmd := exec.CommandContext(runCtx, dockerPath, args...)
	runCmd.Env = safeEnv()
	out, runErr := runCmd.CombinedOutput()
	jobSecrets := make([]string, 0, len(cmd.Env))
	for _, v := range cmd.Env {
		if len(v) >= minRedactableSecretLen {
			jobSecrets = append(jobSecrets, v)
		}
	}
	output := redactSecrets(string(out), jobSecrets)

	elapsed := time.Since(start).Milliseconds()
	success := runErr == nil

	if !success {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.Canceled) {
			// Execution timed out or cancelled. Explicitly kill the container to ensure it's not orphan.
			log.Printf("[executor] run_job timeout or cancel exceeded for job_run=%s — stopping container", cmd.JobRunID)
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
			if dockerPath, err := lookPathSecure("docker"); err == nil {
				stopCmd := exec.CommandContext(stopCtx, dockerPath, "stop", "wireops-job-"+cmd.JobRunID)
				stopCmd.Env = safeEnv()
				_ = stopCmd.Run()
			}
			stopCancel()
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				output = fmt.Sprintf("failed to start job, timeout exceeded: execution took longer than %v", timeout)
			} else {
				output = "failed to start job, execution was cancelled"
			}
		} else {
			if output != "" {
				output += "\n"
			}
			output += runErr.Error()
			log.Printf("[executor] run_job error job_run=%s elapsed=%dms: %v", cmd.JobRunID, elapsed, runErr)
		}
	} else {
		log.Printf("[executor] run_job done job_run=%s elapsed=%dms", cmd.JobRunID, elapsed)
	}

	return protocol.JobCompletedMessage{
		JobRunID:   cmd.JobRunID,
		Success:    success,
		Output:     output,
		DurationMs: elapsed,
	}
}

// KillJob stops a running job container via docker stop.
// The container exit will be observed by the RunJob goroutine, which will send
// JobCompletedMessage. This command returns immediately after issuing docker stop.
func KillJob(cmd protocol.KillJobCommand) protocol.CommandResult {
	containerName := "wireops-job-" + cmd.JobRunID

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dockerPath, err := lookPathSecure("docker")
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: "failed to find docker binary: " + err.Error()}
	}
	killCmd := exec.CommandContext(ctx, dockerPath, "stop", containerName)
	killCmd.Env = safeEnv()
	out, err := killCmd.CombinedOutput()
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

// prepareWorkDir prepares the compose work dir and applies the .env file if provided.
// It returns the workDir, composeFile, a cleanup function, and any error.
// If an error is returned, any created resources are cleaned up before returning.
func prepareWorkDir(stackID, commandID, composeFileB64, envFileB64 string) (string, string, func(), []string, error) {
	workDir, composeFile, cleanup, err := prepareComposeFile(stackID, commandID, composeFileB64)
	if err != nil {
		return "", "", func() {}, nil, err
	}

	secrets, envErr := applyEnvFile(workDir, envFileB64)
	if envErr != nil {
		cleanup()
		return "", "", func() {}, nil, fmt.Errorf("failed to apply env file for stack %s: %w", stackID, envErr)
	}

	return workDir, composeFile, cleanup, secrets, nil
}

// prepareComposeFile decodes the base64 YAML, writes it to a structured directory
// under stackDir/stacks/<stackID>/cmd-<commandID>/, and returns (workDir, filename, cleanupFn, error).
// filepath.Base sanitizes both IDs to prevent path traversal.
func prepareComposeFile(stackID, commandID, b64Content string) (string, string, func(), error) {
	content, err := base64.StdEncoding.DecodeString(b64Content)
	if err != nil {
		return "", "", func() {}, fmt.Errorf("failed to decode compose file: %w", err)
	}

	dir := filepath.Join(stackDir, "stacks", filepath.Base(stackID), "cmd-"+filepath.Base(commandID))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", func() {}, fmt.Errorf("failed to create work dir: %w", err)
	}

	filename := "docker-compose.yml"
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, content, 0600); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", func() {}, fmt.Errorf("failed to write compose file: %w", err)
	}

	log.Printf("[executor] compose workdir prepared stack=%s command=%s dir=%s bytes=%d", stackID, commandID, dir, len(content))

	cleanup := func() {
		if keepWorkerWorkDir() {
			log.Printf("[executor] keeping compose workdir for debugging stack=%s command=%s dir=%s", stackID, commandID, dir)
			return
		}
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("[executor] failed to clean compose workdir stack=%s command=%s dir=%s error=%v", stackID, commandID, dir, err)
			return
		}
		log.Printf("[executor] cleaned compose workdir stack=%s command=%s dir=%s", stackID, commandID, dir)
	}
	return dir, filename, cleanup, nil
}

// jobConfigNamePattern is an allowlist for job run IDs and config names: bare
// identifiers only, no path separators or traversal sequences.
var jobConfigNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// jobConfigRelPathPattern is an allowlist for a config's rel_path once it has
// been cleaned by safepath.CleanRelativePath: relative path segments only.
var jobConfigRelPathPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// validateJobConfigIdent checks value against the bare-identifier allowlist,
// additionally rejecting "." / ".." explicitly since both match the charset.
func validateJobConfigIdent(kind, value string) error {
	if value == "" || value == "." || value == ".." || strings.Contains(value, "..") {
		return fmt.Errorf("invalid %s: %q", kind, value)
	}
	if !jobConfigNamePattern.MatchString(value) {
		return fmt.Errorf("invalid %s: %q", kind, value)
	}
	return nil
}

// prepareJobConfigFiles decodes each job.yaml `configs:` entry and writes it
// to a job-scoped directory under stackDir/jobs/<jobRunID>/configs/<name>,
// returning the "-v hostPath:target:ro" docker run flags to bind-mount them.
// Every path component sourced from the dispatch payload (job run ID,
// config name, rel path) is validated with safepath *before* it is used to
// build a filesystem path — reject-on-invalid, not sanitize-and-continue —
// matching the pattern used elsewhere in this file (e.g. ValidateHostPath).
func prepareJobConfigFiles(jobRunID string, files []protocol.ConfigFileSpec) ([]string, func(), error) {
	noop := func() {}
	if len(files) == 0 {
		return nil, noop, nil
	}

	if err := safepath.ValidateConfigName(jobRunID); err != nil {
		return nil, noop, fmt.Errorf("invalid job run id: %w", err)
	}
	if err := validateJobConfigIdent("job run id", jobRunID); err != nil {
		return nil, noop, err
	}

	// Directory-source binds (groupDir below) are mounted directly into the
	// job container, and RunJob never forces --user root — the container's
	// image may run as an arbitrary non-root UID. That UID needs at least
	// read+execute on every directory in the bind path to traverse it, so
	// these are 0755, not 0700 (files themselves stay locked down by
	// configFileMode, default 0444 world-readable, which is what actually
	// gates content access here).
	dir := filepath.Join(stackDir, "jobs", jobRunID, "configs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, noop, fmt.Errorf("failed to create job config dir: %w", err)
	}
	cleanup := func() {
		if keepWorkerWorkDir() {
			log.Printf("[executor] keeping job config dir for debugging job_run=%s dir=%s", jobRunID, dir)
			return
		}
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("[executor] failed to clean job config dir job_run=%s dir=%s error=%v", jobRunID, dir, err)
		}
	}

	// Group by Name: a job.yaml entry whose source was a single file yields
	// exactly one spec with RelPath=="" (bind-mounted as a single file at
	// Target); one whose source was a directory yields multiple specs
	// sharing Name and Target, each with its own RelPath (written under
	// configs/<name>/<relpath> and bind-mounted once as a directory at
	// Target, instead of one bind per file).
	var order []string
	grouped := map[string][]protocol.ConfigFileSpec{}
	for _, f := range files {
		if err := safepath.ValidateConfigName(f.Name); err != nil {
			return nil, noop, fmt.Errorf("invalid config name %q: %w", f.Name, err)
		}
		if err := validateJobConfigIdent("config name", f.Name); err != nil {
			return nil, noop, err
		}
		if f.RelPath != "" {
			cleanedRelPath, err := safepath.CleanRelativePath(f.RelPath)
			if err != nil {
				return nil, noop, fmt.Errorf("invalid config %q rel_path %q: %w", f.Name, f.RelPath, err)
			}
			if !jobConfigRelPathPattern.MatchString(cleanedRelPath) {
				return nil, noop, fmt.Errorf("invalid config %q rel_path %q: contains disallowed characters", f.Name, f.RelPath)
			}
			f.RelPath = cleanedRelPath
		}
		if _, seen := grouped[f.Name]; !seen {
			order = append(order, f.Name)
		}
		grouped[f.Name] = append(grouped[f.Name], f)
	}

	volumes := make([]string, 0, len(order))
	for _, name := range order {
		specs := grouped[name]

		if len(specs) == 1 && specs[0].RelPath == "" {
			f := specs[0]
			content, err := base64.StdEncoding.DecodeString(f.ContentB64)
			if err != nil {
				cleanup()
				return nil, noop, fmt.Errorf("failed to decode config %q: %w", f.Name, err)
			}
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, content, configFileMode(f.Mode)); err != nil {
				cleanup()
				return nil, noop, fmt.Errorf("failed to write config %q: %w", f.Name, err)
			}
			volumes = append(volumes, path+":"+f.Target+":ro")
			continue
		}

		groupDir := filepath.Join(dir, name)
		if err := os.MkdirAll(groupDir, 0755); err != nil {
			cleanup()
			return nil, noop, fmt.Errorf("failed to create config dir %q: %w", name, err)
		}
		canonicalGroupDir, err := filepath.EvalSymlinks(groupDir)
		if err != nil {
			cleanup()
			return nil, noop, fmt.Errorf("failed to resolve config dir %q: %w", groupDir, err)
		}
		for _, f := range specs {
			content, err := base64.StdEncoding.DecodeString(f.ContentB64)
			if err != nil {
				cleanup()
				return nil, noop, fmt.Errorf("failed to decode config %q: %w", f.Name, err)
			}
			relPath := filepath.Clean(filepath.FromSlash(f.RelPath))
			if relPath == "." || relPath == "" || filepath.IsAbs(relPath) || relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
				cleanup()
				return nil, noop, fmt.Errorf("invalid config relative path %q for %q", f.RelPath, f.Name)
			}
			candidatePath := filepath.Join(groupDir, relPath)

			absCandidatePath, err := filepath.Abs(candidatePath)
			if err != nil {
				cleanup()
				return nil, noop, fmt.Errorf("failed to resolve config path %q for %q: %w", f.RelPath, f.Name, err)
			}
			if absCandidatePath != canonicalGroupDir && !strings.HasPrefix(absCandidatePath, canonicalGroupDir+string(os.PathSeparator)) {
				cleanup()
				return nil, noop, fmt.Errorf("invalid config relative path %q for %q", f.RelPath, f.Name)
			}

			parentDir := filepath.Dir(absCandidatePath)
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				cleanup()
				return nil, noop, fmt.Errorf("failed to create config dir for %q: %w", f.Name, err)
			}
			canonicalParentDir, err := filepath.EvalSymlinks(parentDir)
			if err != nil {
				cleanup()
				return nil, noop, fmt.Errorf("failed to resolve config dir for %q: %w", f.Name, err)
			}
			finalPath := filepath.Join(canonicalParentDir, filepath.Base(absCandidatePath))

			if canonicalParentDir != canonicalGroupDir && !strings.HasPrefix(canonicalParentDir, canonicalGroupDir+string(os.PathSeparator)) {
				cleanup()
				return nil, noop, fmt.Errorf("invalid config relative path %q for %q", f.RelPath, f.Name)
			}
			if finalPath != canonicalGroupDir && !strings.HasPrefix(finalPath, canonicalGroupDir+string(os.PathSeparator)) {
				cleanup()
				return nil, noop, fmt.Errorf("invalid config relative path %q for %q", f.RelPath, f.Name)
			}

			fd, err := os.OpenFile(finalPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_NOFOLLOW, configFileMode(f.Mode))
			if err != nil {
				cleanup()
				return nil, noop, fmt.Errorf("failed to open config %q for writing: %w", f.Name, err)
			}
			if _, err := fd.Write(content); err != nil {
				_ = fd.Close()
				cleanup()
				return nil, noop, fmt.Errorf("failed to write config %q: %w", f.Name, err)
			}
			if err := fd.Close(); err != nil {
				cleanup()
				return nil, noop, fmt.Errorf("failed to close config %q: %w", f.Name, err)
			}
		}
		volumes = append(volumes, groupDir+":"+specs[0].Target+":ro")
	}

	return volumes, cleanup, nil
}

func configFileMode(modeStr string) os.FileMode {
	mode := os.FileMode(0444)
	if modeStr != "" {
		if parsed, err := strconv.ParseUint(modeStr, 8, 32); err == nil {
			mode = os.FileMode(parsed)
		} else {
			log.Printf("[executor] invalid config file mode %q, falling back to 0444: %v", modeStr, err)
		}
	}
	return mode
}

func keepWorkerWorkDir() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("WORKER_KEEP_WORKDIR")), "true")
}

// applyEnvFile writes or removes the .env file in workDir based on envFileB64.
// If envFileB64 is non-empty, it base64-decodes the content and writes .env with
// mode 0600. If empty, any existing .env in the directory is removed.
// Errors during write or decode are returned so callers can abort deployment.
func applyEnvFile(workDir, envFileB64 string) ([]string, error) {
	envPath := filepath.Join(workDir, ".env")
	if envFileB64 == "" {
		if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to remove .env: %w", err)
		}
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(envFileB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode .env content: %w", err)
	}
	if err := os.WriteFile(envPath, data, 0600); err != nil {
		return nil, fmt.Errorf("failed to write .env: %w", err)
	}
	return parseEnvValues(data), nil
}

// validateVolumePaths checks that every host path in the volume specs is
// absolute and free of path traversal. Named volumes (no "/" in the host part)
// are filesystem-agnostic and are skipped.
//
// Policy enforcement (allowlists, forbidden paths, etc.) is the server's
// responsibility — the worker must not duplicate it with hardcoded deny-lists.
func validateVolumePaths(volumes []string) error {
	for _, vol := range volumes {
		vol = strings.TrimSpace(vol)
		if vol == "" {
			continue
		}
		hostPath := strings.SplitN(vol, ":", 3)[0]
		// Only validate paths that look like filesystem references.
		if strings.Contains(hostPath, "/") || strings.HasPrefix(hostPath, ".") {
			if err := safepath.ValidateHostPath(hostPath); err != nil {
				return fmt.Errorf("invalid volume path: %w", err)
			}
		}
	}
	return nil
}

func matchPattern(val, pattern string) bool {
	if pattern == "*" {
		return true
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return val == pattern
	}
	if !strings.HasPrefix(val, parts[0]) {
		return false
	}
	if !strings.HasSuffix(val, parts[len(parts)-1]) {
		return false
	}
	curr := val
	for _, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(curr, part)
		if idx == -1 {
			return false
		}
		curr = curr[idx+len(part):]
	}
	return true
}

func safeEnv() []string {
	env := os.Environ()
	safeDirs := []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin"}
	safePath := "PATH=" + strings.Join(safeDirs, string(filepath.ListSeparator))
	found := false
	for i, kv := range env {
		if strings.HasPrefix(strings.ToUpper(kv), "PATH=") {
			env[i] = safePath
			found = true
		}
	}
	if !found {
		env = append(env, safePath)
	}
	return env
}

// GetContainerStats queries Docker on the worker for CPU, memory, and network stats of a container
// after verifying it belongs to the specified compose project.
func GetContainerStats(ctx context.Context, cmd protocol.GetContainerStatsCommand) protocol.CommandResult {
	log.Printf("[executor] get_container_stats start stack=%s project=%s container=%s command=%s", cmd.StackID, cmd.ProjectName, cmd.ContainerID, cmd.CommandID)

	cli, err := verifyContainerAndGetClient(ctx, cmd.ContainerID, cmd.ProjectName)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer cli.Close()

	stats, err := compose.GetContainerStats(ctx, cli.Raw(), cmd.ContainerID)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}

	encoded, err := json.Marshal(stats)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}

	return protocol.CommandResult{CommandID: cmd.CommandID, Output: string(encoded)}
}

// GetContainerLogs retrieves logs for a container after verifying it belongs to the project.
func GetContainerLogs(ctx context.Context, cmd protocol.GetContainerLogsCommand) protocol.CommandResult {
	log.Printf("[executor] get_container_logs start stack=%s project=%s container=%s command=%s tail=%s", cmd.StackID, cmd.ProjectName, cmd.ContainerID, cmd.CommandID, cmd.Tail)

	cli, err := verifyContainerAndGetClient(ctx, cmd.ContainerID, cmd.ProjectName)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer cli.Close()

	tail := cmd.Tail
	if tail == "" {
		tail = "100"
	}

	reader, err := cli.Raw().ContainerLogs(ctx, cmd.ContainerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
		Timestamps: true,
	})
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.CommandID, Error: err.Error()}
	}
	defer reader.Close()

	const maxPayloadSize = 10 * 1024 * 1024
	buf := new(strings.Builder)
	header := make([]byte, 8)
	for {
		_, err := io.ReadFull(reader, header)
		if err != nil {
			break
		}
		size := int(header[4])<<24 | int(header[5])<<16 | int(header[6])<<8 | int(header[7])
		if size == 0 {
			continue
		}
		if size < 0 || size > maxPayloadSize {
			log.Printf("[executor] invalid payload size %d", size)
			break
		}
		payload := make([]byte, size)
		_, err = io.ReadFull(reader, payload)
		if err != nil {
			break
		}
		buf.Write(payload)
	}

	return protocol.CommandResult{CommandID: cmd.CommandID, Output: buf.String()}
}

func lookPathSecure(file string) (string, error) {
	safeDirs := []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin"}
	for _, dir := range safeDirs {
		path := filepath.Join(dir, file)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("executable %q not found in safe paths", file)
}

var defaultStackDir string

func init() {
	home, err := os.UserHomeDir()
	if err == nil {
		defaultStackDir = filepath.Join(home, ".wireops")
		return
	}
	tempDir, err := os.MkdirTemp("", "wireops-*")
	if err == nil {
		defaultStackDir = tempDir
		return
	}
	if cwd, err := os.Getwd(); err == nil {
		defaultStackDir = filepath.Join(cwd, ".wireops")
		return
	}
	defaultStackDir = "./.wireops"
}

func getSecureDefaultStackDir() string {
	return defaultStackDir
}
