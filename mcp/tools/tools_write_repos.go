package tools

import (
	"context"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wireops/wireops/mcp/client"
	"github.com/wireops/wireops/mcp/models"
)

// registerRepoWriteTools adds the manage_repositories-scoped tools, plus the
// operator-only repository reads that had no tool yet. Repository credentials
// (repository_keys: SSH keys, git passwords) are deliberately out of scope —
// nothing here creates, reads or attaches one.
func registerRepoWriteTools(server *mcp.Server, c *client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sync_repository",
		Description: "Clone or fetch a wireops repository's tracked branch right now, refreshing the server's workspace copy. Does not deploy anything — stacks pick the new commit up on their own next reconcile, or immediately via deploy_stack. Useful after pushing a commit you want to act on without waiting for the poll.",
		Annotations: additive(true),
	}, syncRepository(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_stack_from_compose",
		Description: "Create a wireops stack from a compose file already committed in a repository — the primary way to go from scaffold_stack output to a running stack. compose_path is the directory inside the repository and compose_file the file name (default docker-compose.yml); the file's optional top-level 'x-wireops' block supplies the wireops settings. Set paused true when env vars still have to be written (set_stack_env_vars) before the first deploy, then call set_stack_paused with paused false.",
		Annotations: additive(false),
	}, createStackFromCompose(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_stack_from_wireops",
		Description: "DEPRECATED LAYOUT: create a wireops stack from a separate wireops.yaml plus docker-compose.yml pair in a repository. Prefer create_stack_from_compose with an embedded x-wireops block; use this only for a repository already on the two-file layout. wireops_file is the repository-relative path of the wireops.yaml.",
		Annotations: additive(false),
	}, createStackFromWireops(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "lint_compose",
		Description: "Statically analyse a compose file committed in a wireops repository and report problems before deploying it: syntax errors, undefined variables, and (when worker is given) violations of that worker's effective deploy security policy. Pass stack to resolve the env vars an existing stack would deploy with, which is what makes the undefined-variable check meaningful. Read-only: it fetches the repository and parses the file, nothing else.",
		Annotations: readOnly(),
	}, lintCompose(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "preview_stack_migration",
		Description: "Dry-run moving a wireops stack to a different git repository: reports what would be written, which compose resources (services/volumes/networks) change identity, and what would break — without touching anything. Always run this before migrate_stack.",
		Annotations: readOnly(),
	}, previewStackMigration(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "migrate_stack",
		Description: "Move a wireops stack to a different git repository, repointing the stack record at the new repository and compose file. Run preview_stack_migration first. teardown_old_project additionally tears the stack down under its previous compose project name, which stops its current containers. confirm must be the stack's exact name.",
		Annotations: destructive(false),
	}, migrateStack(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "browse_repo_files",
		Description: "List every .yml/.yaml file in a wireops repository (excluding .git, node_modules and vendor), for picking a compose, wireops or job file by hand. Triggers a git fetch first, so it reflects the latest commit on the tracked branch. For the filtered views, prefer list_repo_stack_files / list_repo_job_files.",
		Annotations: readOnly(),
	}, browseRepoFiles(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_repo_compose_definition",
		Description: "Parse a compose file in a wireops repository and return its resolved configuration (the equivalent of `docker compose config`), including any embedded x-wireops block. Use this to inspect a candidate file found via list_repo_stack_files before creating a stack from it.",
		Annotations: readOnly(),
	}, repoDefinition(c, "compose-definition"))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_repo_wireops_definition",
		Description: "Parse a standalone wireops.yaml in a wireops repository (the deprecated two-file layout) and return its validated definition.",
		Annotations: readOnly(),
	}, repoDefinition(c, "wireops-definition"))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_repo_job_definition",
		Description: "Parse a job.yaml in a wireops repository and return its validated definition, before scheduling it with create_scheduled_job. For an already-scheduled job use get_job_definition instead.",
		Annotations: readOnly(),
	}, repoDefinition(c, "job-definition"))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_repository",
		Description: "Register a git repository with wireops. Only public repositories (or ones reachable with an already-configured credential) work through this tool — attaching SSH keys or git passwords is deliberately not exposed over MCP and has to be done in the wireops UI. After creating, call sync_repository to verify wireops can actually clone it.",
		Annotations: additive(false),
	}, createRepository(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_repository",
		Description: "Change a wireops repository's name, clone URL or tracked branch. Only the fields you pass are modified. Switching branch changes what every stack in that repository reconciles against, so run sync_repository afterwards and check the stacks.",
		Annotations: destructive(false),
	}, updateRepository(c))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_repository",
		Description: "Delete a wireops repository record. Stacks and jobs that reference it stop being reconcilable. This does not touch the remote git repository. confirm must be the repository's exact name.",
		Annotations: destructive(true),
	}, deleteRepository(c))
}

func syncRepository(c *client.Client) mcp.ToolHandlerFor[models.SyncRepositoryInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.SyncRepositoryInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out any
		path := "/api/custom/repositories/" + url.PathEscape(in.RepositoryID) + "/sync"
		if err := c.Post(ctx, apiKey, path, nil, struct{}{}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func createStackFromCompose(c *client.Client) mcp.ToolHandlerFor[models.CreateStackFromComposeInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.CreateStackFromComposeInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if in.Repository == "" || in.Worker == "" {
			return nil, nil, fmt.Errorf("repository and worker are required")
		}
		body := map[string]any{
			"repository":   in.Repository,
			"worker":       in.Worker,
			"compose_path": in.ComposePath,
			"compose_file": in.ComposeFile,
			"paused":       in.Paused,
		}
		var out any
		if err := c.Post(ctx, apiKey, "/api/custom/stacks/from-compose", nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func createStackFromWireops(c *client.Client) mcp.ToolHandlerFor[models.CreateStackFromWireopsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.CreateStackFromWireopsInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if in.Repository == "" || in.Worker == "" || in.WireopsFile == "" {
			return nil, nil, fmt.Errorf("repository, worker and wireops_file are required")
		}
		body := map[string]any{
			"repository":   in.Repository,
			"worker":       in.Worker,
			"wireops_file": in.WireopsFile,
			"paused":       in.Paused,
		}
		var out any
		if err := c.Post(ctx, apiKey, "/api/custom/stacks/from-wireops", nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func lintCompose(c *client.Client) mcp.ToolHandlerFor[models.LintComposeInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.LintComposeInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if in.Repository == "" {
			return nil, nil, fmt.Errorf("repository is required")
		}
		body := map[string]any{
			"repository":   in.Repository,
			"compose_path": in.ComposePath,
			"compose_file": in.ComposeFile,
			"worker":       in.Worker,
			"stack":        in.Stack,
		}
		var out any
		if err := c.Post(ctx, apiKey, "/api/custom/lint/compose", nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

// migrateBody builds the shared migrateRequestBody payload. confirm is the
// server's own boolean gate, set only by the mutating tool.
func migrateBody(repository, composePath, composeFile, wireopsFile string, teardownOld, confirm bool) map[string]any {
	return map[string]any{
		"repository":           repository,
		"compose_path":         composePath,
		"compose_file":         composeFile,
		"wireops_file":         wireopsFile,
		"teardown_old_project": teardownOld,
		"confirm":              confirm,
	}
}

func previewStackMigration(c *client.Client) mcp.ToolHandlerFor[models.PreviewStackMigrationInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.PreviewStackMigrationInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if in.Repository == "" {
			return nil, nil, fmt.Errorf("repository is required")
		}
		body := migrateBody(in.Repository, in.ComposePath, in.ComposeFile, in.WireopsFile, false, false)
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/migrate/preview"
		if err := c.Post(ctx, apiKey, path, nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func migrateStack(c *client.Client) mcp.ToolHandlerFor[models.MigrateStackInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.MigrateStackInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if in.Repository == "" {
			return nil, nil, fmt.Errorf("repository is required")
		}
		if err := requireConfirm(ctx, c, apiKey, confirmStacks, in.StackID, in.Confirm); err != nil {
			return nil, nil, err
		}
		// The server has its own confirm:true gate; the name check above is
		// what actually involves a human, so this one is just satisfied.
		body := migrateBody(in.Repository, in.ComposePath, in.ComposeFile, in.WireopsFile, in.TeardownOldProject, true)
		var out any
		path := "/api/custom/stacks/" + url.PathEscape(in.StackID) + "/migrate"
		if err := c.Post(ctx, apiKey, path, nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func browseRepoFiles(c *client.Client) mcp.ToolHandlerFor[models.BrowseRepoFilesInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.BrowseRepoFilesInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		var out []interface{}
		path := "/api/custom/repositories/" + url.PathEscape(in.RepositoryID) + "/files"
		if err := c.Get(ctx, apiKey, path, nil, &out); err != nil {
			return nil, nil, err
		}
		return nil, map[string]interface{}{"files": out}, nil
	}
}

// repoDefinition builds the handler for the three
// /api/custom/repositories/{id}/{kind} routes, which differ only in which
// parser the server runs on the ?file= parameter.
func repoDefinition(c *client.Client, kind string) mcp.ToolHandlerFor[models.RepoFileDefinitionInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.RepoFileDefinitionInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if in.Path == "" {
			return nil, nil, fmt.Errorf("path is required")
		}
		var out any
		path := "/api/custom/repositories/" + url.PathEscape(in.RepositoryID) + "/" + kind
		if err := c.Get(ctx, apiKey, path, url.Values{"file": {in.Path}}, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func createRepository(c *client.Client) mcp.ToolHandlerFor[models.CreateRepositoryInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.CreateRepositoryInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if in.Name == "" || in.GitURL == "" {
			return nil, nil, fmt.Errorf("name and git_url are required")
		}
		branch := in.Branch
		if branch == "" {
			branch = "main"
		}
		body := map[string]any{"name": in.Name, "git_url": in.GitURL, "branch": branch}
		var out any
		if err := c.Post(ctx, apiKey, "/api/collections/repositories/records", nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func updateRepository(c *client.Client) mcp.ToolHandlerFor[models.UpdateRepositoryInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.UpdateRepositoryInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		// Only send what was actually given: a PATCH with empty strings would
		// blank fields the caller never mentioned.
		body := map[string]any{}
		setIfNonEmpty(body, "name", in.Name)
		setIfNonEmpty(body, "git_url", in.GitURL)
		setIfNonEmpty(body, "branch", in.Branch)
		if len(body) == 0 {
			return nil, nil, fmt.Errorf("nothing to update — pass at least one of name, git_url, branch")
		}
		var out any
		path := "/api/collections/repositories/records/" + url.PathEscape(in.RepositoryID)
		if err := c.Patch(ctx, apiKey, path, nil, body, &out); err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	}
}

func deleteRepository(c *client.Client) mcp.ToolHandlerFor[models.DeleteRepositoryInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in models.DeleteRepositoryInput) (*mcp.CallToolResult, any, error) {
		apiKey, err := apiKeyFrom(ctx)
		if err != nil {
			return nil, nil, err
		}
		if err := requireConfirm(ctx, c, apiKey, confirmRepos, in.RepositoryID, in.Confirm); err != nil {
			return nil, nil, err
		}
		path := "/api/collections/repositories/records/" + url.PathEscape(in.RepositoryID)
		if err := c.Delete(ctx, apiKey, path, nil, nil); err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"status": "deleted", "repository_id": in.RepositoryID}, nil
	}
}
