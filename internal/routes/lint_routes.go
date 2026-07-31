package routes

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/lint"
	"github.com/wireops/wireops/internal/policy"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/safepath"
)

const (
	// lintTimeout bounds one lint request end to end: the repository
	// clone/fetch plus the `docker compose config` subprocess.
	lintTimeout = 2 * time.Minute

	// lintRequestMaxBytes caps the request body. The payload is four short
	// identifiers, so anything larger is either a mistake or an attempt to
	// make the server buffer a large body.
	lintRequestMaxBytes = 16 << 10
)

// composeErrorForClient turns a `docker compose config` failure into a message
// safe to hand back over the API.
//
// The diagnostic itself is what makes this endpoint useful — "yaml: line 4,
// column 12: mapping values are not allowed" tells the author exactly what to
// fix — so it is preserved rather than replaced with something generic. What
// is stripped is the server's own filesystem layout: these errors embed the
// absolute repo workspace path, both directly ("compose file not found in
// /data/repos/<id>/svc") and inside the CLI's stderr ("open /data/repos/...").
// The unredacted detail is logged so an operator can still debug server-side.
func composeErrorForClient(e *core.RequestEvent, stage, repoID string, err error) string {
	actor := ""
	if e != nil && e.Auth != nil {
		actor = e.Auth.Id
	}
	log.Printf("[lint] failed to %s compose config for repo %s (actor=%s): %v", stage, repoID, actor, err)

	return "failed to " + stage + " compose config: " + redactWorkspacePaths(err.Error())
}

// redactFindings strips server-side paths from a report before it leaves the
// process.
//
// Findings are not purely author-written text: `docker compose config`
// rewrites relative bind-mount sources into absolute server paths, so a
// volume-policy violation can carry "/data/repos/<id>/config" in its message.
// That is the same disclosure config_error is redacted for, and it has to be
// handled on this path too.
//
// Message and Hint are the only free-text fields. Finding.Path is a dotted
// config location ("services.web.volumes"), not a filesystem path, and there
// is no Subject on a Finding — that field lives on policy.Violation and is
// folded into Message before it reaches here.
//
// Modifies in place; runs before either response so the early
// source-unreadable return is covered too.
func redactFindings(findings []lint.Finding) {
	for i := range findings {
		findings[i].Message = redactWorkspacePaths(findings[i].Message)
		findings[i].Hint = redactWorkspacePaths(findings[i].Hint)
	}
}

// redactWorkspacePaths rewrites absolute paths under the repository workspace
// to a repo-relative form, so an error can be shown to the caller without
// describing where the server keeps its data.
func redactWorkspacePaths(msg string) string {
	// Most specific first: the repo workspace usually lives under DATA_DIR, so
	// redacting DATA_DIR first would swallow the more informative "<repos>"
	// label. Each root is replaced in its trailing-separator form first so the
	// bare replacement cannot leave a doubled slash.
	//
	// "<workspace>/<repo-id>/svc/x.yml" becomes "<repos>/<repo-id>/svc/x.yml":
	// the repo id is the caller's own input, so only the server-side root is
	// worth hiding.
	for _, root := range []struct{ path, label string }{
		{config.GetReposWorkspace(), "<repos>"},
		{config.GetStacksStoragePath(), "<stacks>"},
		{config.GetDataDir(), "<data>"},
	} {
		if root.path == "" {
			continue
		}
		// These roots are configured relatively by default (DATA_DIR defaults
		// to "./data", and PB_DATA_DIR=./pb_data resolves DATA_DIR to "."),
		// while `docker compose config` rewrites bind-mount sources to
		// absolute paths — so matching has to happen in absolute form too, or
		// a root like "." never matches the real path it is meant to hide.
		// Resolving "." to an absolute path also closes the actual bug this
		// guards against: as a bare relative root, "." matched (and mangled)
		// every literal "." in a finding's message, e.g. an IP in a hint.
		abs, err := filepath.Abs(root.path)
		if err != nil {
			continue
		}
		trimmed := strings.TrimSuffix(abs, string(filepath.Separator))
		// A root that resolves to the filesystem root has nothing specific to
		// redact and would otherwise match the start of every absolute path.
		if trimmed == "" {
			continue
		}
		msg = strings.ReplaceAll(msg, trimmed+string(filepath.Separator), root.label+"/")
		msg = strings.ReplaceAll(msg, trimmed, root.label)
	}
	return msg
}

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

		// Every request here does real work — a git clone/fetch plus a
		// `docker compose config` subprocess — so bound it rather than
		// letting a slow remote or a pathological repo hold a handler (and
		// its child process) open for as long as the client keeps the
		// connection.
		reqCtx, cancel := context.WithTimeout(e.Request.Context(), lintTimeout)
		defer cancel()

		// Everything that can reject the request is resolved before the git
		// clone/fetch below. A request that is going to be refused must not
		// first cost the server a network round trip to a remote it was never
		// entitled to reach on that caller's behalf.
		if _, err := rr.app.FindRecordById("repositories", body.Repository); err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "repository not found"})
		}

		ctx := lint.Context{}

		// An unknown worker must be rejected rather than silently falling back
		// to the global policy: policy.Load returns the global policy for a
		// worker it cannot find, which would report "no policy violations" for
		// a worker whose policy was never actually consulted.
		if body.Worker != "" {
			if _, err := rr.app.FindRecordById("workers", body.Worker); err != nil {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "worker not found"})
			}
			wp, err := policy.Load(rr.app, body.Worker)
			if err != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load worker policy"})
			}
			ctx.Policy = wp
		}

		// The stack must belong to the repository being linted. Without that
		// check any caller could point this at an unrelated stack and use the
		// undefined-variable rule as an oracle — a compose file referencing
		// ${FOO} reports a finding only when the stack does not define FOO,
		// which enumerates another stack's variable names one at a time. It
		// would also make this route resolve that stack's secrets (Vault /
		// Infisical fetches) as a side effect.
		if body.Stack != "" {
			stack, err := rr.app.FindRecordById("stacks", body.Stack)
			if err != nil {
				return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
			}
			if stack.GetString("repository") != body.Repository {
				return e.JSON(http.StatusBadRequest, map[string]string{"error": "stack does not belong to the given repository"})
			}
			if rr.scheduler != nil {
				envVars, err := rr.scheduler.LoadStackEnvVars(reqCtx, body.Stack)
				if err != nil {
					return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to resolve stack environment variables"})
				}
				ctx.EnvKeys = lint.EnvKeysFromPairs(envVars)
			}
		}

		// Clones or fetches the repo, so the lint reflects the branch's
		// current head rather than whatever was last synced. Bounded by
		// reqCtx: a hung remote must not outlive the request's own deadline.
		repoDir, ok := rr.repoFilesSetupByIDContext(e, reqCtx, body.Repository)
		if !ok {
			return nil
		}
		workDir := repoDir
		if body.ComposePath != "" && body.ComposePath != "." {
			workDir = filepath.Join(repoDir, filepath.Clean(body.ComposePath))
		}

		// Root is the repo checkout, not workDir: compose_path is request
		// input, so the directory has to be contained too, not just the file
		// inside it.
		configOut, err := compose.Config(reqCtx, compose.ConfigOptions{
			WorkDir:     workDir,
			ComposeFile: composeFile,
			Root:        repoDir,
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
				"config_error": composeErrorForClient(e, "resolve", body.Repository, err),
			})
		}

		configMap, err := compose.ParseConfigJSON(configOut)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]any{
				"report":       lint.Report{Findings: []lint.Finding{}},
				"config_error": composeErrorForClient(e, "parse", body.Repository, err),
			})
		}

		ctx.RepoRoot = repoDir
		report := lint.Run(configMap, ctx)
		redactFindings(report.Findings)

		// The source is read for the preview and, more importantly, to give
		// each finding a line number: the rules run against the resolved
		// config, which carries no positions, so the mapping back to the file
		// has to happen against the file itself.
		source, filename, readErr := compose.ReadFile(repoDir, workDir, composeFile, 0)
		if readErr != nil {
			// The lint itself succeeded, so return it without the preview
			// rather than discarding a perfectly good report.
			log.Printf("[lint] report ready but source unreadable for repo %s: %v", body.Repository, readErr)
			return e.JSON(http.StatusOK, map[string]any{"report": report})
		}

		report.Findings = lint.ResolveLines(source, report.Findings)

		return e.JSON(http.StatusOK, map[string]any{
			"report":   report,
			"content":  string(source),
			"filename": filename,
		})
	}).Bind(apis.BodyLimit(lintRequestMaxBytes)).BindFunc(rbac.Require(rbac.CapManageRepos))
}
