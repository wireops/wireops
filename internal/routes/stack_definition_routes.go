package routes

import (
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	stdsync "sync"

	gogit "github.com/go-git/go-git/v5"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/hooks"
	"github.com/wireops/wireops/internal/manifest"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/safepath"
)

// definitionRefreshChange makes a successful refresh inspectable without
// returning the entire stack record. Values are the persisted values before
// and after applying the validated git definition.
type definitionRefreshChange struct {
	From any `json:"from"`
	To   any `json:"to"`
}

type definitionRefreshResponse struct {
	Status          string                             `json:"status"`
	Source          string                             `json:"source"`
	CommitSHA       string                             `json:"commit_sha"`
	ChangedFields   map[string]definitionRefreshChange `json:"changed_fields"`
	DeployTriggered bool                               `json:"deploy_triggered"`
	Preview         bool                               `json:"preview"`
}

type definitionRefreshError struct {
	status  int
	message string
	errors  []string
}

func (e *definitionRefreshError) Error() string { return e.message }

// registerStackDefinitionRoutes adds the deliberate counterpart to the
// create-from-compose/create-from-wireops flows. File-managed stack fields
// remain immutable through generic CRUD; this route is the one controlled
// path that fetches and validates the committed definition before changing
// them. It never deploys, so an operator can review its diff and choose when
// to apply the new compose revision with the ordinary sync route.
func (rr routeRegistrar) registerStackDefinitionRoutes() {
	rr.r.POST("/api/custom/stacks/{id}/resync-definition", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		if stackID == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "missing id"})
		}
		// preview=true reports the same diff and saves nothing. worker_tags is
		// among the fields this route refreshes, and moving a running stack to
		// another node is not something to discover from the response of the
		// call that already did it.
		preview := e.Request.URL.Query().Get("preview") == "true"
		if rr.scheduler == nil {
			return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": "scheduler is unavailable"})
		}
		// Resolve existence before taking a keyed mutex. Besides giving callers a
		// useful 404, this avoids retaining a per-stack lock for arbitrary
		// unknown IDs supplied to this public route.
		if _, err := rr.app.FindRecordById("stacks", stackID); err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		// Take the same mutex as reconcile, migration, and transfer across the
		// fetch/parse/save sequence. A definition must not be updated halfway
		// through a deploy that already read the previous timeout or flags.
		release, ok := rr.scheduler.TryLockStack(stackID)
		if !ok {
			return e.JSON(http.StatusConflict, map[string]string{"error": "stack is currently syncing"})
		}
		var releaseOnce stdsync.Once
		releaseStackLock := func() { releaseOnce.Do(release) }
		defer releaseStackLock()

		// Re-read under the lock: the record might have been removed between
		// the existence check above and acquiring it.
		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}
		source := stack.GetString("config_source")
		if source != "wireops_file" && source != "compose_embedded" {
			return e.JSON(http.StatusConflict, map[string]string{"error": "stack is not managed by a wireops definition in git"})
		}

		repoDir, ok := rr.repoFilesSetupByID(e, stack.GetString("repository"))
		if !ok {
			return nil
		}
		commitSHA, err := currentRepositoryCommit(repoDir)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to identify refreshed repository commit: " + err.Error()})
		}

		def, resolveErr := rr.definitionFromGit(stack, repoDir)
		if resolveErr != nil {
			if len(resolveErr.errors) > 0 {
				return e.JSON(resolveErr.status, map[string]any{"error": resolveErr.message, "errors": resolveErr.errors})
			}
			return e.JSON(resolveErr.status, map[string]string{"error": resolveErr.message})
		}

		changes := applyWireopsDefinition(stack, def, source)
		if preview {
			// The record was mutated in memory only; it is discarded with the
			// request, and the stack lock above kept any concurrent deploy from
			// observing it.
			return e.JSON(http.StatusOK, definitionRefreshResponse{
				Status:          previewStatus(changes),
				Source:          source,
				CommitSHA:       commitSHA,
				ChangedFields:   changes,
				DeployTriggered: false,
				Preview:         true,
			})
		}
		if len(changes) > 0 {
			// The hook deliberately rejects generic edits to these fields. This
			// narrow bypass applies only after the source was fetched and parsed,
			// and is cleared immediately after this Save.
			if err := hooks.WithWireopsDefinitionBypass(stackID, func() error {
				return rr.app.Save(stack)
			}); err != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save refreshed definition: " + err.Error()})
			}
		}

		status := "unchanged"
		if len(changes) > 0 {
			status = "updated"
		}
		return e.JSON(http.StatusOK, definitionRefreshResponse{
			Status:          status,
			Source:          source,
			CommitSHA:       commitSHA,
			ChangedFields:   changes,
			DeployTriggered: false,
		})
		// CapOperateStacks, not CapManageRepos: this mutates one stack's deploy
		// behaviour, which is the same blast radius as deploy_stack, and reads
		// its input from the repository rather than from the caller. Both sit at
		// RoleOperator, so this narrows which capability is required without
		// changing the role tier.
	}).BindFunc(rbac.Require(rbac.CapOperateStacks))
}

func currentRepositoryCommit(repoDir string) (string, error) {
	repo, err := gogit.PlainOpen(repoDir)
	if err != nil {
		return "", err
	}
	head, err := repo.Head()
	if err != nil {
		return "", err
	}
	return head.Hash().String(), nil
}

// definitionFromGit reads the exact source associated with an existing
// file-managed stack. It intentionally never accepts a path from the request:
// the persisted, immutable source identity is the authority.
func (rr routeRegistrar) definitionFromGit(stack *core.Record, repoDir string) (*manifest.Definition, *definitionRefreshError) {
	switch stack.GetString("config_source") {
	case "wireops_file":
		wireopsFile := stack.GetString("wireops_file_path")
		if wireopsFile == "" {
			return nil, &definitionRefreshError{status: http.StatusUnprocessableEntity, message: "stack has no wireops_file_path"}
		}
		def, err := manifest.ParseWireopsFile(config.GetReposWorkspace(), stack.GetString("repository"), wireopsFile)
		if err != nil {
			return nil, definitionParseError(err)
		}
		resolveWireopsComposeFile(repoDir, wireopsFile, def)
		if def.ResolutionError != "" {
			return nil, &definitionRefreshError{status: http.StatusUnprocessableEntity, message: def.ResolutionError}
		}
		return def, nil

	case "compose_embedded":
		composePath := stack.GetString("compose_path")
		if err := safepath.ValidateComposePath(composePath); err != nil {
			return nil, &definitionRefreshError{status: http.StatusUnprocessableEntity, message: err.Error()}
		}
		composeFile := stack.GetString("compose_file")
		if composeFile == "" {
			composeFile = "docker-compose.yml"
		}
		if err := safepath.ValidateComposeFile(composeFile); err != nil {
			return nil, &definitionRefreshError{status: http.StatusUnprocessableEntity, message: err.Error()}
		}

		workDir := repoDir
		if composePath != "" && composePath != "." {
			workDir = filepath.Join(repoDir, composePath)
		}
		data, _, err := compose.ReadFile(repoDir, workDir, composeFile, config.GetComposeMaxBytes())
		if err != nil {
			if errors.Is(err, compose.ErrOutputTooLarge) {
				return nil, &definitionRefreshError{status: http.StatusUnprocessableEntity, message: err.Error()}
			}
			return nil, &definitionRefreshError{status: http.StatusUnprocessableEntity, message: "configured compose file not found"}
		}
		def, err := manifest.ParseComposeManifest(data)
		if err != nil {
			return nil, definitionParseError(err)
		}
		if def == nil {
			return nil, &definitionRefreshError{status: http.StatusUnprocessableEntity, message: "compose file has no x-wireops block"}
		}
		return def, nil
	}

	return nil, &definitionRefreshError{status: http.StatusConflict, message: "stack is not managed by a wireops definition in git"}
}

func definitionParseError(err error) *definitionRefreshError {
	return &definitionRefreshError{
		status:  http.StatusUnprocessableEntity,
		message: err.Error(),
		errors:  wireopsValidationErrors(err),
	}
}

// applyWireopsDefinition updates only the fields which are actually managed
// by a parsed definition. The stack's name, group, assigned worker, source
// identity, secrets, and deployment history deliberately stay untouched.
func applyWireopsDefinition(stack *core.Record, def *manifest.Definition, source string) map[string]definitionRefreshChange {
	changes := map[string]definitionRefreshChange{}
	setBool := func(field string, next bool) {
		if previous := stack.GetBool(field); previous != next {
			stack.Set(field, next)
			changes[field] = definitionRefreshChange{From: previous, To: next}
		}
	}
	setInt := func(field string, next int) {
		if previous := stack.GetInt(field); previous != next {
			stack.Set(field, next)
			changes[field] = definitionRefreshChange{From: previous, To: next}
		}
	}
	setString := func(field, next string) {
		if previous := stack.GetString(field); previous != next {
			stack.Set(field, next)
			changes[field] = definitionRefreshChange{From: previous, To: next}
		}
	}
	setStrings := func(field string, next []string) {
		previous := stack.GetStringSlice(field)
		if !slices.Equal(previous, next) {
			stack.Set(field, next)
			changes[field] = definitionRefreshChange{From: previous, To: next}
		}
	}

	removeOrphans, forcePull, waitRunningJobs, workerTags := resolveWireopsStackFields(def)
	setBool("remove_orphans", removeOrphans)
	setBool("force_pull", forcePull)
	setInt("deploy_timeout_seconds", def.DeployTimeoutSeconds)
	setInt("sync_interval_seconds", def.SyncIntervalSeconds)
	setString("wait_running_jobs", waitRunningJobs)
	setStrings("worker_tags", workerTags)

	if source == "wireops_file" {
		setString("compose_path", def.ResolvedComposePath)
		setString("compose_file", def.ResolvedComposeFile)
	}

	return changes
}

// previewStatus names what an apply would do, without doing it.
func previewStatus(changes map[string]definitionRefreshChange) string {
	if len(changes) == 0 {
		return "unchanged"
	}
	return "would-update"
}
