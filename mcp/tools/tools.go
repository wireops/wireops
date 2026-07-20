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
		Description: "Generate a docker-compose.yml and matching wireops.yaml for a new stack from structured service definitions, ready to commit to a repository. If worker_id is given, validates the compose file against that worker's effective deploy security policy and reports violations instead of silently returning a file that would be rejected at deploy time. Does not create or import any stack.",
	}, scaffoldStack(c))
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
