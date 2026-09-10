// Package prompts registers the wireops MCP prompt templates — canned,
// discoverable (prompts/list) message sequences that operationalize a
// specific use case rather than leaving the client to chain tool calls
// itself. The prompt handler pre-fetches real data through the existing
// REST API (same pass-through auth as mcp/tools) and embeds it directly
// in the returned message, so the model gets grounded context instead of
// free-form tool-calling authority.
package prompts

import (
	"context"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wireops/wireops/mcp/auth"
	"github.com/wireops/wireops/mcp/client"
)

// Register adds every wireops prompt template to server.
func Register(server *mcp.Server, c *client.Client) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "diagnose_stack_failure",
		Description: "Diagnose why a wireops stack's most recent sync/deploy failed, using its current status and recent sync log history.",
		Arguments: []*mcp.PromptArgument{
			{Name: "stack_id", Description: "The wireops stack record id to diagnose.", Required: true},
		},
	}, diagnoseStackFailure(c))

	server.AddPrompt(&mcp.Prompt{
		Name:        "scaffold_new_stack",
		Description: "Research and scaffold a new wireops stack for a described application. By default this generates a single docker-compose.yml with an embedded x-wireops block — the primary, recommended way to define a wireops stack.",
		Arguments: []*mcp.PromptArgument{
			{Name: "app_description", Description: "What the stack should run, e.g. 'a Postgres database with pgAdmin' or 'Ghost blog behind Traefik'.", Required: true},
			{Name: "image", Description: "A specific Docker image to use, if already known. Optional — leave empty to have the model research one.", Required: false},
			{Name: "two_file", Description: "'true' to generate the deprecated two-file layout (separate wireops.yaml + docker-compose.yml) instead of the default single compose file with an embedded x-wireops block. Optional — defaults to false (single file).", Required: false},
		},
	}, scaffoldNewStack())
}

func apiKeyFrom(ctx context.Context) (string, error) {
	apiKey, ok := auth.APIKeyFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("no wireops API key on this MCP session — pass one via the %s header when connecting", "X-Wireops-Api-Key")
	}
	return apiKey, nil
}

func diagnoseStackFailure(c *client.Client) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		stackID := req.Params.Arguments["stack_id"]
		if stackID == "" {
			return nil, fmt.Errorf("stack_id argument is required")
		}

		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, err
		}

		var status any
		statusPath := "/api/collections/stacks/records/" + url.PathEscape(stackID)
		if err := c.Get(ctx, apiKey, statusPath, nil, &status); err != nil {
			return nil, fmt.Errorf("fetching stack status: %w", err)
		}

		var logs any
		q := url.Values{
			"filter":  {fmt.Sprintf("stack='%s'", client.EscapeFilterValue(stackID))},
			"sort":    {"-created"},
			"perPage": {"5"},
		}
		if err := c.Get(ctx, apiKey, "/api/collections/sync_logs/records", q, &logs); err != nil {
			return nil, fmt.Errorf("fetching sync logs: %w", err)
		}

		text := fmt.Sprintf(`Diagnose why wireops stack %s is failing to deploy/sync.

Current stack status:
%v

Last 5 sync log entries (most recent first):
%v

Identify the most likely root cause (e.g. missing/invalid env var, port conflict, image not found, compose syntax error, worker offline, deploy rejected by the worker's security policy). If the sync log output references a specific container, call get_container_logs for that container to confirm before concluding. If the failure looks like a policy rejection (an image/volume/network/capability/device not allowed, a disallowed privileged/host-network/host-PID/host-IPC/docker-socket setting, an untagged/":latest" image, or a host bind-mount), call get_worker_policy for the stack's worker to confirm which rule it violates. State your hypothesis and the evidence for it; do not take any write action.`, stackID, status, logs)

		return &mcp.GetPromptResult{
			Description: "Root-cause diagnosis for a failing wireops stack, grounded in its current status and recent sync history.",
			Messages: []*mcp.PromptMessage{
				{
					Role:    "user",
					Content: &mcp.TextContent{Text: text},
				},
			},
		}, nil
	}
}

func scaffoldNewStack() mcp.PromptHandler {
	return func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		appDescription := req.Params.Arguments["app_description"]
		if appDescription == "" {
			return nil, fmt.Errorf("app_description argument is required")
		}
		image := req.Params.Arguments["image"]
		twoFile := req.Params.Arguments["two_file"] == "true"

		imageHint := "You do not have a specific image yet — use your own web search tool to find the official Docker image (its official site, official Docker Hub/GHCR page, or official GitHub repository) and its current required configuration (ports, volumes, required environment variables) before proceeding."
		if image != "" {
			imageHint = fmt.Sprintf("A candidate image was given: %q. Do not trust this as-is — use your own web search tool to confirm it against the project's official site, official image registry page, or official GitHub repository, and pull its current required configuration (ports, volumes, required environment variables) before proceeding.", image)
		}

		fileModeHint := "Call scaffold_stack with two_file left false (the default): it embeds the wireops fields as a top-level x-wireops block inside the compose file and returns that one file only — no separate wireops.yaml. This is the primary, recommended way to define a wireops stack. Create the stack via POST /api/custom/stacks/from-compose (compose_path/compose_file)."
		if twoFile {
			fileModeHint = "Call scaffold_stack with two_file: true: it returns the deprecated layout, a separate wireops.yaml plus docker-compose.yml. Create the stack via POST /api/custom/stacks/from-wireops. Note this two-file layout is deprecated in favor of the embedded x-wireops block."
		}

		text := fmt.Sprintf(`Scaffold a new wireops stack for: %s

%s

Always look up current information, never rely on memorized/training-data knowledge of the image — versions, default ports, volume paths, and required env vars change over time. Prefer the project's official website, official image registry listing, or official GitHub repository over blog posts, forums, or third-party tutorials.

Once you have the image(s) and their verified, up-to-date ports/volumes/environment variables, call the scaffold_stack tool with a service definition for each container. Do not invent image names, tags, or configuration you have not verified against an official source.

%s

Before designing service definitions, check the deploy security policy that will actually enforce them: call get_worker_policy if you know which wireops worker this stack will run on (its "effective" field is what gets enforced), or get_global_worker_policy otherwise for the instance-wide default every worker falls back to. Design every service to already comply, rather than discovering violations after generating the file:
- An empty allowlist (allowed_images/allowed_volumes/allowed_networks/allowed_cap_add/allowed_devices/allowed_security_opt) means unrestricted for that dimension; a non-empty one means every image/volume-source/network/cap_add/device/security_opt you use must match one of its entries.
- If prevent_latest_images is true, always pin an explicit version tag — never ":latest" or a tag-less image reference.
- If block_host_volumes is true, do not bind-mount host paths — use named volumes instead.
- Never set privileged: true, network_mode: host, pid: host, ipc: host, or mount the Docker socket (/var/run/docker.sock) when the matching flag (block_privileged, block_host_network, block_host_pid, block_host_ipc, block_docker_socket) is true.
- If enabled is false (worker) or the whole policy predictably resolves as disabled, none of the above is actually enforced on deploy — but keep following it anyway as a safety default unless the user asks otherwise.

Then still pass worker_id to scaffold_stack: it re-validates the generated compose file against the same effective policy as a final check and reports any violation you missed, instead of silently returning a file that would be rejected at deploy time.

scaffold_stack's structured input has no field for labels or annotations, so apply these optional wireops compose conventions by hand-editing the returned compose YAML before you present or commit it, only where they're actually wanted:

- customization.image.slug — a label (or annotation, at the service level or under deploy:) giving the service a proper icon in the wireops UI instead of a generic one. Its value must match an existing slug in the selfh.st/icons catalog (https://selfh.st/icons/) — do not invent one. Example:
    services:
      app:
        image: nginx:1.31.4-alpine
        labels:
          - "customization.image.slug=nginx"

- dev.wireops.config.<name> — a service-level annotation (not a label; put it under an "annotations:" map, not "labels:") that mounts a git-tracked config file or directory straight into the container. <name> is an arbitrary identifier distinguishing multiple such annotations on one service (letters/digits/-/_, no path separators or leading dot); its value is "<repo-relative source>:<absolute in-container target>". wireops synthesizes the compose top-level configs: block and each service's configs: list from this annotation at render time — never write configs: yourself, and never invent a wireops.yaml field for it, since none exists. Example, mapping a committed nginx.conf to its in-container path:
    services:
      app:
        image: nginx:1.31.4-alpine
        annotations:
          dev.wireops.config.nginx-conf: "conf/nginx.conf:/etc/nginx/nginx.conf"
  Rules enforced at sync/render time, so the stack fails to sync if violated:
  - source must exist in the repository being committed to.
  - source may name a single file, or a whole directory — a directory is expanded recursively into one mounted file per entry, preserving its relative layout under target (e.g. source "conf/" containing "conf/site.conf" and "conf/certs/a.pem" mounts to "target/site.conf" and "target/certs/a.pem"). You do not need to list files individually.
  - source must stay inside the repository (no absolute path, no "../" escaping it) and must not be or contain a symlink — both are rejected outright, not silently followed.
  - each resolved file is capped at 1MB, and the combined size of every config resolved for the stack is capped at 5MB — keep large/binary assets out of this mechanism.
  - target must be a clean absolute in-container path (e.g. "/etc/nginx/nginx.conf"), never relative and never containing "..".

- customization.init — a label marking a service as a run-to-completion container (a migration, seed script, or other one-shot init job that is expected to run once and exit, not stay up). Without this label, wireops treats any service whose container isn't "running" as missing and marks the whole stack degraded/error — a clean exit still looks like a crash. With it, the service is only considered unhealthy if it exits non-zero or gets stuck in a restart loop; exiting 0 (or being gone entirely, once Docker garbage-collects it) counts as healthy. Only add it to services that are genuinely meant to exit on their own — never to a long-running service, since that would hide a real crash. Example:
    services:
      migrate:
        image: myapp:1.4.0
        command: ["./migrate", "up"]
        restart: "no"
        labels:
          - "customization.init=true"`, appDescription, imageHint, fileModeHint)

		return &mcp.GetPromptResult{
			Description: "Research-grounded scaffolding for a new wireops stack from a natural-language description.",
			Messages: []*mcp.PromptMessage{
				{
					Role:    "user",
					Content: &mcp.TextContent{Text: text},
				},
			},
		}, nil
	}
}
