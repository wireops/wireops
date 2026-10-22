package tools

import (
	"context"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

// registerJobWriteTools adds the manage_jobs-scoped tools. A scheduled job's
// actual behaviour (cron expression, image, command, resources) lives in the
// repository's job.yaml — the record these tools write only points at that
// file, so changing a schedule means committing a new job.yaml, not calling
// update_scheduled_job.
// registerJobReadTools registers the read-only job tools (group listing). They are annotated read-only, so they belong to
// the read tool group even though they sit in this file.
func registerJobReadTools(server *mcp.Server, c *client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_job_groups",
		Description: "List the groups wireops scheduled jobs are organised into (the 'group' field of each job.yaml), with their job counts. Read-only.",
		Annotations: readOnly(),
	}, listJobGroups(c))
}

func registerJobWriteTools(server *mcp.Server, c *client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_job",
		Description: "Trigger a wireops scheduled job to run immediately, outside its cron schedule. The job.yaml is re-parsed and validated first, so a broken definition fails here instead of silently not running. Returns once the run is queued — poll list_jobs for the run's outcome.",
		Annotations: additive(false),
	}, runJob(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "cancel_job_run",
		Description: "Cancel an in-flight job run, killing its container on the worker. Only affects runs that are still running; finished runs return an error.",
		Annotations: destructive(true),
	}, cancelJobRun(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_job_run",
		Description: "Delete a job run record and its captured output. The server only allows this for runs whose status is 'stalled' — the way to clear a run left behind by a worker that died mid-run; anything else is refused, so cancel_job_run a run that is genuinely still executing. confirm must be the literal string 'delete'.",
		Annotations: destructive(true),
	}, deleteJobRun(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_job_env_vars",
		Description: "Write a wireops scheduled job's environment variables in one atomic call. mode 'append' upserts the given keys and leaves every other key alone; mode 'replace' makes the job's env vars exactly this list and DELETES any key not present, so it requires confirm to be the job's exact name. Keys must match ^[A-Za-z_][A-Za-z0-9_]*$. Secret values can never be read back through MCP.",
		Annotations: destructive(false),
	}, setJobEnvVars(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_scheduled_job",
		Description: "Schedule a job.yaml that is already committed in a wireops repository. The cron expression, image, command and resource limits come from that file — use generate_job_yaml to author one and get_repo_job_definition to validate it first. enabled defaults to false so the job is created without immediately entering the cron rotation; flip it with update_scheduled_job once its env vars are in place.",
		Annotations: additive(false),
	}, createScheduledJob(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_scheduled_job",
		Description: "Change a wireops scheduled job record: its name, description, the repository/job.yaml it points at, whether its cron schedule is live (enabled), or its status. Only the fields you pass are modified. The schedule itself lives in job.yaml, not here.",
		Annotations: destructive(false),
	}, updateScheduledJob(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_scheduled_job",
		Description: "Delete a wireops scheduled job record, its env vars and its run history. The job.yaml stays in the repository. confirm must be the job's exact name.",
		Annotations: destructive(true),
	}, deleteScheduledJob(c))
}

func runJob(c *client.Client) mcp.ToolHandlerFor[models.RunJobInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.RunJobInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		jobID, err := resolveJobID(ctx, c, apiKey, in.JobID)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/jobs/" + url.PathEscape(jobID) + "/run"
		if err := c.Post(ctx, apiKey, path, nil, struct{}{}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func cancelJobRun(c *client.Client) mcp.ToolHandlerFor[models.JobRunInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.JobRunInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/job-runs/" + url.PathEscape(in.RunID) + "/cancel"
		if err := c.Post(ctx, apiKey, path, nil, struct{}{}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func deleteJobRun(c *client.Client) mcp.ToolHandlerFor[models.DeleteJobRunInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.DeleteJobRunInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		// A run has no name to echo back, so the confirmation is the literal
		// word rather than a lookup.
		if err := matchConfirm("delete", in.Confirm); err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/job-runs/" + url.PathEscape(in.RunID)
		if err := c.Delete(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func setJobEnvVars(c *client.Client) mcp.ToolHandlerFor[models.SetJobEnvVarsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.SetJobEnvVarsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		jobID, err := resolveJobID(ctx, c, apiKey, in.JobID)
		if err != nil {
			return nil, nil, err
		}
		if err := validateEnvVarMode(in.Mode); err != nil {
			return nil, nil, err
		}
		if in.Mode == envVarModeReplace {
			if err := requireConfirm(ctx, c, apiKey, confirmSchedJobs, jobID, in.Confirm); err != nil {
				return nil, nil, err
			}
		}
		var out any
		path := "/api/custom/jobs/" + url.PathEscape(jobID) + "/env-vars/bulk"
		body := map[string]any{"mode": in.Mode, "vars": in.Vars}
		if err := c.Post(ctx, apiKey, path, nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func listJobGroups(c *client.Client) mcp.ToolHandlerFor[models.ListJobGroupsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ models.ListJobGroupsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		if err := c.Get(ctx, apiKey, "/api/custom/jobs/groups", nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func createScheduledJob(c *client.Client) mcp.ToolHandlerFor[models.CreateScheduledJobInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.CreateScheduledJobInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		repositoryID, err := resolveRepositoryID(ctx, c, apiKey, in.Repository)
		if err != nil {
			return nil, nil, err
		}
		if in.Name == "" || repositoryID == "" || in.JobFile == "" {
			return nil, nil, fmt.Errorf("name, repository and job_file are required")
		}
		body := map[string]any{
			"name":        in.Name,
			"repository":  repositoryID,
			"job_file":    in.JobFile,
			"description": in.Description,
			"enabled":     in.Enabled,
			"status":      "active",
		}
		var out any
		if err := c.Post(ctx, apiKey, "/api/collections/scheduled_jobs/records", nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func updateScheduledJob(c *client.Client) mcp.ToolHandlerFor[models.UpdateScheduledJobInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.UpdateScheduledJobInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		jobID, err := resolveJobID(ctx, c, apiKey, in.JobID)
		if err != nil {
			return nil, nil, err
		}
		repositoryID, err := resolveRepositoryID(ctx, c, apiKey, in.Repository)
		if err != nil {
			return nil, nil, err
		}
		body := map[string]any{}
		setIfNonEmpty(body, "name", in.Name)
		setIfNonEmpty(body, "repository", repositoryID)
		setIfNonEmpty(body, "job_file", in.JobFile)
		setIfNonEmpty(body, "description", in.Description)
		setIfNonEmpty(body, "status", in.Status)
		if in.Enabled != nil {
			body["enabled"] = *in.Enabled
		}
		if len(body) == 0 {
			return nil, nil, fmt.Errorf("nothing to update — pass at least one field")
		}
		var out any
		path := "/api/collections/scheduled_jobs/records/" + url.PathEscape(jobID)
		if err := c.Patch(ctx, apiKey, path, nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func deleteScheduledJob(c *client.Client) mcp.ToolHandlerFor[models.DeleteScheduledJobInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.DeleteScheduledJobInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		jobID, err := resolveJobID(ctx, c, apiKey, in.JobID)
		if err != nil {
			return nil, nil, err
		}
		if err := requireConfirm(ctx, c, apiKey, confirmSchedJobs, jobID, in.Confirm); err != nil {
			return nil, nil, err
		}
		path := "/api/collections/scheduled_jobs/records/" + url.PathEscape(jobID)
		if err := c.Delete(ctx, apiKey, path, nil, nil); err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"status": "deleted", "job_id": jobID}, nil
	}
}
