package routes

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/lint"
	"github.com/wireops/wireops/internal/policy"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/safepath"
)

// lintComposeRequest points the linter at a compose file in a repository.
//
// The stack it belongs to need not exist: the create-stack modal lints a file
// the user has only just picked, which is the main reason this route takes a
// repository plus a path rather than a stack ID.
type lintComposeRequest struct {
	Repository  string `json:"repository"`
	ComposePath string `json:"compose_path"`
	ComposeFile string `json:"compose_file"`
	// Worker selects whose deploy policy the config is checked against.
	// Optional — without it the report carries advisory findings only.
	Worker string `json:"worker"`
	// Stack is optional and only used to resolve the environment variables
	// the stack would deploy with, which is what makes the
	// undefined-variable check meaningful. Omitted for a stack that does not
	// exist yet, in which case that check is skipped rather than guessed at.
	Stack string `json:"stack"`
}

// registerLintRoutes exposes the compose linter.
//
// This runs entirely on the server and never touches a Docker daemon:
// `docker compose config` is a client-side parse, and everything after it is
// static analysis of the resulting config. Checks that would need daemon state
// (does this image exist, is this host port free) are deliberately not here —
// those belong on a worker.
func (rr routeRegistrar) registerLintRoutes() {
	rr.r.POST("/api/custom/lint/compose", func(e *core.RequestEvent) error {
		var body lintComposeRequest
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if body.Repository == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing repository"})
		}

		composeFile := body.ComposeFile
		if composeFile == "" {
			composeFile = "docker-compose.yml"
		}
		if err := safepath.ValidateComposeFile(composeFile); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		if err := safepath.ValidateComposePath(body.ComposePath); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		}

		// Clones or fetches the repo, so the lint reflects the branch's
		// current head rather than whatever was last synced.
		repoDir, ok := rr.repoFilesSetupByID(e, body.Repository)
		if !ok {
			return nil
		}
		workDir := repoDir
		if body.ComposePath != "" && body.ComposePath != "." {
			workDir = filepath.Join(repoDir, filepath.Clean(body.ComposePath))
		}

		ctx := lint.Context{}

		// A worker whose policy cannot be loaded still gets an advisory
		// report — better than failing the whole request over it.
		if body.Worker != "" {
			wp, err := policy.Load(rr.app, body.Worker)
			if err == nil {
				ctx.Policy = wp
			}
		}

		if body.Stack != "" && rr.scheduler != nil {
			if envVars, err := rr.scheduler.LoadStackEnvVars(e.Request.Context(), body.Stack); err == nil {
				ctx.EnvKeys = lint.EnvKeysFromPairs(envVars)
			}
		}

		configOut, err := compose.Config(e.Request.Context(), compose.ConfigOptions{
			WorkDir:     workDir,
			ComposeFile: composeFile,
		}, true)
		if err != nil {
			// A compose file that will not even parse is the most useful
			// thing this route can report, so it comes back as a normal
			// result the UI renders inline, not as a request failure. The
			// key is config_error rather than error because the frontend's
			// customPost treats a top-level "error" as a thrown request
			// failure regardless of status.
			return e.JSON(http.StatusOK, map[string]any{
				"report":       lint.Report{Findings: []lint.Finding{}},
				"config_error": "failed to resolve compose config: " + err.Error(),
			})
		}

		configMap, err := compose.ParseConfigJSON(configOut)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]any{
				"report":       lint.Report{Findings: []lint.Finding{}},
				"config_error": "failed to parse compose config: " + err.Error(),
			})
		}

		return e.JSON(http.StatusOK, map[string]any{"report": lint.Run(configMap, ctx)})
	}).BindFunc(rbac.Require(rbac.CapManageRepos))
}
