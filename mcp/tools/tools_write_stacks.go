package tools

import (
	"context"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

// registerStackWriteTools adds the operate_stacks-scoped tools. Every one of
// them maps onto a route the UI already calls, so the server-side RBAC check
// (rbac.CapOperateStacks) and audit entry come for free — a viewer-scoped API
// key gets a 403 back from the server exactly as it does from the browser.
func registerStackWriteTools(server *mcp.Server, c *client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "deploy_stack",
		Description: "Trigger a git sync + deploy of a wireops stack right now, instead of waiting for the next poll. This is the normal \"apply what's in git\" action: it fetches the tracked branch, re-renders the compose file and, if the rendered output changed, dispatches a deploy to the stack's worker. Requires the stack's worker to be online. Returns as soon as the reconcile is queued — poll get_stack_status / get_sync_logs for the outcome.",
		Annotations: additive(true),
	}, deployStack(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "force_redeploy_stack",
		Description: "Redeploy a wireops stack even when git has not changed, optionally recreating containers, volumes and networks. Use this to recover a stack whose containers drifted or died, or to re-pull a moving tag. recreate_volumes DESTROYS the stack's named volume data and therefore requires confirm to be the stack's exact name. By default the stack is paused after a successful redeploy (pause_after_redeploy), matching the UI — pass false to leave git sync running.",
		Annotations: destructive(false),
	}, forceRedeployStack(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "rollback_stack",
		Description: "Redeploy a wireops stack from an earlier git commit. Find candidate commits with get_repo_commits, and compare the rendered output of two revisions with diff_stack_version first. Requires the stack's worker to be online.",
		Annotations: destructive(false),
	}, rollbackStack(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_stack_paused",
		Description: "Pause or resume git sync/reconcile for a wireops stack. Pausing does NOT stop the running containers — it only stops wireops from acting on new commits; use stop_container for the containers themselves. Resuming makes the stack eligible for the next poll, so a stack that drifted from git will be reconciled shortly after.",
		Annotations: additive(true),
	}, setStackPaused(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_stack_render_overrides",
		Description: "Set the render-time overrides (image/ports/networks/scale) for a wireops stack. Overrides are ephemeral — they are NOT committed to git, and they are reapplied on every reconcile until cleared, so the stack will keep diverging from its repository until clear_stack_render_overrides is called. This replaces the stack's entire override set, so include every service you want overridden. The server rejects the call if the worker policy disables overrides, and validates the result against that policy.",
		Annotations: destructive(true),
	}, setStackRenderOverrides(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "clear_stack_render_overrides",
		Description: "Remove all render-time overrides from a wireops stack, so the next reconcile renders purely from git again. Safe to call on a stack that has none.",
		Annotations: additive(true),
	}, clearStackRenderOverrides(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "restart_container",
		Description: "Restart a single container of a wireops stack, without redeploying the stack. Identify it with container_id or service_name (either works). Requires the stack's worker to be online.",
		Annotations: destructive(false),
	}, restartContainer(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "stop_container",
		Description: "Stop a single container of a wireops stack. The container stays stopped until the next deploy or a restart_container call. confirm must be the container's exact compose service name, which is also what service_name takes — this is a deliberate speed bump, not a lookup you can skip.",
		Annotations: destructive(true),
	}, stopContainer(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "transfer_stack",
		Description: "Move a wireops stack to a different worker: its last deployed revision is deployed on the target, then torn down on the source. Both workers must be online and the stack must have a successful deploy already (deployed_version), otherwise the server refuses. Named volumes do NOT travel with it — Docker volumes are local to a host, so the data stays on the old one. confirm must be the stack's exact name.",
		Annotations: destructive(false),
	}, transferStack(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_stack",
		Description: "Tear a wireops stack down on its worker and delete its wireops record, revisions and sync history. This does not touch the git repository. Set force to true to delete the record even when the worker is offline, which leaves the containers running on that host as orphans. confirm must be the stack's exact name.",
		Annotations: destructive(true),
	}, deleteStack(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_stack_env_vars",
		Description: "Write a wireops stack's environment variables in one atomic call. mode 'append' upserts the given keys and leaves every other key alone; mode 'replace' makes the stack's env vars exactly this list and DELETES any key not present, so it requires confirm to be the stack's exact name. Keys must match ^[A-Za-z_][A-Za-z0-9_]*$. Set secret true to store a value encrypted (it can never be read back through MCP — list_secrets only shows key names). Changing env vars does not redeploy by itself; call deploy_stack afterwards.",
		Annotations: destructive(false),
	}, setStackEnvVars(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "copy_stack_env_vars",
		Description: "Copy named environment variables from another wireops stack into this one, server-side — secret values are never exposed to the caller in the process. keys is required; an empty list copies nothing. Existing keys on the destination are skipped unless overwrite is true. Cross-repository copies are refused when the source stack has SOPS-managed secrets.",
		Annotations: additive(false),
	}, copyStackEnvVars(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "discover_importable_stacks",
		Description: "List docker-compose projects already running on the connected workers that are not managed by wireops yet, as candidates for import_stack. Read-only.",
		Annotations: readOnly(),
	}, discoverImportableStacks(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "import_stack",
		Description: "Adopt a compose project that already exists on a worker's host into wireops, by absolute path to its compose file on that host (see discover_importable_stacks). The stack is created unmanaged by git — migrate_stack moves it into a repository afterwards. recreate_volumes DESTROYS the existing volume data instead of adopting it; leave it false to keep the running data.",
		Annotations: additive(false),
	}, importStack(c))
}

func deployStack(c *client.Client) mcp.ToolHandlerFor[models.DeployStackInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.DeployStackInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/sync"
		if err := c.Post(ctx, apiKey, path, nil, struct{}{}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func forceRedeployStack(c *client.Client) mcp.ToolHandlerFor[models.ForceRedeployStackInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ForceRedeployStackInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		// Only the volume recreation is unrecoverable, so that's the one
		// branch that pays the confirmation cost.
		if in.RecreateVolumes {
			if err := requireConfirm(ctx, c, apiKey, confirmStacks, in.StackID, in.Confirm); err != nil {
				return nil, nil, err
			}
		}
		// The server's forceRedeployBody defaults PauseAfter to true and
		// cannot distinguish "false" from "absent", so mirror that default
		// here rather than silently resuming sync on every redeploy.
		pauseAfter := true
		if in.PauseAfter != nil {
			pauseAfter = *in.PauseAfter
		}
		body := map[string]any{
			"recreate_containers":  in.RecreateContainers,
			"recreate_volumes":     in.RecreateVolumes,
			"recreate_networks":    in.RecreateNetworks,
			"pause_after_redeploy": pauseAfter,
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/force-redeploy"
		if err := c.Post(ctx, apiKey, path, nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func rollbackStack(c *client.Client) mcp.ToolHandlerFor[models.RollbackStackInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.RollbackStackInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if in.CommitSHA == "" {
			return nil, nil, fmt.Errorf("commit_sha is required")
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/rollback"
		if err := c.Post(ctx, apiKey, path, nil, map[string]any{"commit_sha": in.CommitSHA}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func setStackPaused(c *client.Client) mcp.ToolHandlerFor[models.SetStackPausedInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.SetStackPausedInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		// Same mechanism the UI uses: pause/resume is a status write on the
		// stacks record, not a custom route.
		status := "active"
		if in.Paused {
			status = "paused"
		}
		var out any
		path := "/api/collections/stacks/records/" + url.PathEscape(in.StackID)
		if err := c.Patch(ctx, apiKey, path, nil, map[string]any{"status": status}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func setStackRenderOverrides(c *client.Client) mcp.ToolHandlerFor[models.SetStackRenderOverridesInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.SetStackRenderOverridesInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if len(in.Overrides) == 0 {
			return nil, nil, fmt.Errorf("overrides must not be empty — use clear_stack_render_overrides to remove them")
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/render-overrides"
		if err := c.Put(ctx, apiKey, path, nil, map[string]any{"overrides": in.Overrides}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func clearStackRenderOverrides(c *client.Client) mcp.ToolHandlerFor[models.ClearStackRenderOverridesInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ClearStackRenderOverridesInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/render-overrides"
		if err := c.Delete(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func restartContainer(c *client.Client) mcp.ToolHandlerFor[models.ContainerActionInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ContainerActionInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		containerID, _, err := resolveContainerTarget(ctx, c, apiKey, in.StackID, in.ContainerID, in.ServiceName)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/container/restart"
		if err := c.Post(ctx, apiKey, path, nil, map[string]any{"container_id": containerID}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func stopContainer(c *client.Client) mcp.ToolHandlerFor[models.StopContainerInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.StopContainerInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		containerID, serviceName, err := resolveContainerTarget(ctx, c, apiKey, in.StackID, in.ContainerID, in.ServiceName)
		if err != nil {
			return nil, nil, err
		}
		if err := matchConfirm(serviceName, in.Confirm); err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/container/stop"
		if err := c.Post(ctx, apiKey, path, nil, map[string]any{"container_id": containerID}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func transferStack(c *client.Client) mcp.ToolHandlerFor[models.TransferStackInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.TransferStackInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if in.TargetWorkerID == "" {
			return nil, nil, fmt.Errorf("target_worker_id is required")
		}
		if err := requireConfirm(ctx, c, apiKey, confirmStacks, in.StackID, in.Confirm); err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/transfer"
		if err := c.Post(ctx, apiKey, path, nil, map[string]any{"target_worker_id": in.TargetWorkerID}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func deleteStack(c *client.Client) mcp.ToolHandlerFor[models.DeleteStackInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.DeleteStackInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if err := requireConfirm(ctx, c, apiKey, confirmStacks, in.StackID, in.Confirm); err != nil {
			return nil, nil, err
		}
		var q url.Values
		if in.Force {
			q = url.Values{"force": {"true"}}
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID)
		if err := c.Delete(ctx, apiKey, path, q, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func setStackEnvVars(c *client.Client) mcp.ToolHandlerFor[models.SetStackEnvVarsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.SetStackEnvVarsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if err := validateEnvVarMode(in.Mode); err != nil {
			return nil, nil, err
		}
		if in.Mode == envVarModeReplace {
			if err := requireConfirm(ctx, c, apiKey, confirmStacks, in.StackID, in.Confirm); err != nil {
				return nil, nil, err
			}
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/env-vars/bulk"
		body := map[string]any{"mode": in.Mode, "vars": in.Vars}
		if err := c.Post(ctx, apiKey, path, nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func copyStackEnvVars(c *client.Client) mcp.ToolHandlerFor[models.CopyStackEnvVarsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.CopyStackEnvVarsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if in.SourceStack == "" {
			return nil, nil, fmt.Errorf("source_stack is required")
		}
		body := map[string]any{
			"source_stack": in.SourceStack,
			"keys":         in.Keys,
			"overwrite":    in.Overwrite,
		}
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/env-vars/copy-from"
		if err := c.Post(ctx, apiKey, path, nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func discoverImportableStacks(c *client.Client) mcp.ToolHandlerFor[models.DiscoverImportableStacksInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ models.DiscoverImportableStacksInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		if err := c.Get(ctx, apiKey, "/api/custom/stacks/import/discover", nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func importStack(c *client.Client) mcp.ToolHandlerFor[models.ImportStackInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.ImportStackInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if in.Name == "" || in.WorkerID == "" || in.ImportPath == "" {
			return nil, nil, fmt.Errorf("name, worker_id and import_path are required")
		}
		body := map[string]any{
			"name":             in.Name,
			"worker_id":        in.WorkerID,
			"import_path":      in.ImportPath,
			"recreate_volumes": in.RecreateVolumes,
		}
		var out any
		if err := c.Post(ctx, apiKey, "/api/custom/stacks/import", nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}
