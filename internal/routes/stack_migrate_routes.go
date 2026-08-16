package routes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	stdsync "sync"

	"github.com/pocketbase/pocketbase/core"
	"gopkg.in/yaml.v3"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/envvars"
	"github.com/wireops/wireops/internal/hooks"
	"github.com/wireops/wireops/internal/manifest"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/safepath"
	"github.com/wireops/wireops/internal/secrets"
	"github.com/wireops/wireops/internal/sync"
	"github.com/wireops/wireops/pkg/utils"
)

// migrateRequestBody is the shared decoded body for both the preview and
// migrate endpoints. wireops_file is only read when the stack's
// config_source is "wireops_file"; compose_path/compose_file are only read
// otherwise. Confirm and TeardownOldProject are only meaningful for the
// mutating /migrate endpoint.
type migrateRequestBody struct {
	Repository         string `json:"repository"`
	ComposePath        string `json:"compose_path"`
	ComposeFile        string `json:"compose_file"`
	WireopsFile        string `json:"wireops_file"`
	Confirm            bool   `json:"confirm"`
	TeardownOldProject bool   `json:"teardown_old_project"`
}

// extractComposeResourceSets pulls the named-resource sets out of a
// `docker compose config --format json` result (or an equivalent
// already-rendered compose YAML decoded the same way): service names,
// top-level named volumes, top-level named networks.
func extractComposeResourceSets(configMap map[string]interface{}) (services, volumes, networks []string) {
	if s, ok := configMap["services"].(map[string]interface{}); ok {
		services = slices.Sorted(maps.Keys(s))
	}
	if v, ok := configMap["volumes"].(map[string]interface{}); ok {
		volumes = slices.Sorted(maps.Keys(v))
	}
	if n, ok := configMap["networks"].(map[string]interface{}); ok {
		networks = slices.Sorted(maps.Keys(n))
	}
	return services, volumes, networks
}

// composeProjectName returns a resolved compose config's top-level `name:`
// value (the renderer requires this field, internal/sync/renderer.go),
// empty if absent or not a string.
func composeProjectName(configMap map[string]interface{}) string {
	name, _ := configMap["name"].(string)
	return name
}

// extractPublishedHostPorts returns every "<host-port>/<protocol>" pair
// (e.g. "8080/tcp") published by any service in a resolved compose config —
// reuses portNumberString (stack_routes.go) for the same float64/string
// port-field handling the render-overrides diff already needs. Two
// different compose *projects* publishing the same host port+protocol can
// never run at once — that's a hard docker-level bind conflict, not
// something --remove-orphans or any wireops-side check can route around.
func extractPublishedHostPorts(configMap map[string]interface{}) []string {
	services, ok := configMap["services"].(map[string]interface{})
	if !ok {
		return nil
	}
	var ports []string
	for _, svcRaw := range services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}
		list, ok := svc["ports"].([]interface{})
		if !ok {
			continue
		}
		for _, item := range list {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			published := portNumberString(m["published"])
			if published == "" {
				continue
			}
			protocol, _ := m["protocol"].(string)
			if protocol == "" {
				protocol = "tcp"
			}
			ports = append(ports, published+"/"+protocol)
		}
	}
	slices.Sort(ports)
	return slices.Compact(ports)
}

func diffSets(source, target []string) MigrateDiff {
	added, removed, common := utils.SetDiff(source, target)
	return MigrateDiff{
		Added:   nonNilStrings(added),
		Removed: nonNilStrings(removed),
		Common:  nonNilStrings(common),
	}
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// buildMigratePreview is the pure core of the preview report: given the
// already-resolved source/target compose configs and an already-resolved
// SOPS check, it computes the resource diffs, project-name comparison, and
// advisory warnings. It does no I/O (no compose.Config shell-out, no repo
// clone) so it's fully testable without docker.
func buildMigratePreview(sourceRepoID, targetRepoID string, sourceConfig, targetConfig map[string]interface{}, sops SopsCheck) MigratePreview {
	sourceServices, sourceVolumes, sourceNetworks := extractComposeResourceSets(sourceConfig)
	targetServices, targetVolumes, targetNetworks := extractComposeResourceSets(targetConfig)

	services := diffSets(sourceServices, targetServices)
	volumes := diffSets(sourceVolumes, targetVolumes)
	networks := diffSets(sourceNetworks, targetNetworks)

	projectName := ProjectNameCheck{
		Source: composeProjectName(sourceConfig),
		Target: composeProjectName(targetConfig),
	}
	projectName.Same = projectName.Source == projectName.Target

	_, _, conflictingPorts := utils.SetDiff(extractPublishedHostPorts(sourceConfig), extractPublishedHostPorts(targetConfig))

	preview := MigratePreview{
		SourceRepository: sourceRepoID,
		TargetRepository: targetRepoID,
		Services:         services,
		Volumes:          volumes,
		Networks:         networks,
		ProjectName:      projectName,
		Sops:             sops,
		Warnings:         []MigrateWarning{},
	}
	preview.Warnings = append(preview.Warnings, migrateWarnings(services, volumes, networks, projectName, sops, conflictingPorts)...)
	return preview
}

// migrateWarnings flattens the diff/project-name/SOPS/port results into the
// advisory list the frontend renders, per the severity table in the
// migration plan (§4.4): a removed named volume is the one finding that
// actually risks data loss (critical); a host port published by both
// projects is the other critical finding — it isn't advisory, the target
// deploy will hard-fail with "address already in use" the moment the old
// project's container is still holding that port (see extractPublishedHostPorts).
// Everything else here is informational.
func migrateWarnings(services, volumes, networks MigrateDiff, projectName ProjectNameCheck, sops SopsCheck, conflictingPorts []string) []MigrateWarning {
	var warnings []MigrateWarning

	for _, v := range volumes.Removed {
		warnings = append(warnings, MigrateWarning{
			Severity: "critical",
			Code:     "volume_removed",
			Message:  fmt.Sprintf("named volume %q does not exist on the target — its data will NOT be preserved through recreation", v),
		})
	}
	for _, s := range services.Removed {
		warnings = append(warnings, MigrateWarning{
			Severity: "warn",
			Code:     "service_removed",
			Message:  fmt.Sprintf("service %q does not exist on the target — its container may be left as an orphan", s),
		})
	}
	for _, n := range networks.Removed {
		warnings = append(warnings, MigrateWarning{
			Severity: "info",
			Code:     "network_removed",
			Message:  fmt.Sprintf("network %q does not exist on the target — it will be recreated", n),
		})
	}
	for _, v := range volumes.Added {
		warnings = append(warnings, MigrateWarning{Severity: "info", Code: "volume_added", Message: fmt.Sprintf("new named volume %q on the target", v)})
	}
	for _, s := range services.Added {
		warnings = append(warnings, MigrateWarning{Severity: "info", Code: "service_added", Message: fmt.Sprintf("new service %q on the target", s)})
	}
	for _, n := range networks.Added {
		warnings = append(warnings, MigrateWarning{Severity: "info", Code: "network_added", Message: fmt.Sprintf("new network %q on the target", n)})
	}

	if !projectName.Same {
		warnings = append(warnings, MigrateWarning{
			Severity: "warn",
			Code:     "project_name_changed",
			Message:  fmt.Sprintf("compose project name changes from %q to %q — containers from the old project are not cleaned up automatically", projectName.Source, projectName.Target),
		})
		// Only relevant when the project name actually changes: the old
		// project keeps running untouched (that's the whole reason it can
		// orphan), so a shared host port is a real bind conflict, not just
		// the same project's container releasing and reclaiming its own port.
		if len(conflictingPorts) > 0 {
			warnings = append(warnings, MigrateWarning{
				Severity: "critical",
				Code:     "port_conflict",
				Message:  fmt.Sprintf("host port(s) %s are published by both the old and new project — deploying the target will fail with \"address already in use\" while the old project is still running (tear it down first, e.g. teardown_old_project)", strings.Join(conflictingPorts, ", ")),
			})
		}
	}

	switch sops.Status {
	case "undecryptable":
		msg := "target repository has a secrets.yaml that does not decrypt with its own SOPS age key — re-encrypt it for the target before relying on it"
		if sops.TargetAgePublicKey != "" {
			msg = fmt.Sprintf("%s (target age public key: %s)", msg, sops.TargetAgePublicKey)
		}
		warnings = append(warnings, MigrateWarning{Severity: "warn", Code: "sops_undecryptable", Message: msg})
	case "source_had_secrets":
		warnings = append(warnings, MigrateWarning{
			Severity: "warn",
			Code:     "sops_source_had_secrets",
			Message:  "source repository had a secrets.yaml but the target does not — SOPS-backed env vars will be missing after migration",
		})
	}

	return warnings
}

// resolveSopsCheck test-decrypts the target repository's secrets.yaml (if
// any) with the target's own SOPS age key — the same lookup/decrypt path
// Reconciler.loadSopsEnv uses at reconcile time, run here ahead of time so
// the preview can surface a mismatch before it fails a real deploy. It
// never returns an error: every failure mode is a reportable SopsCheck
// status instead, since the preview must never 4xx on an incompatibility.
// destRoot/sourceRoot are the repository checkout roots destWorkDir/
// sourceWorkDir must resolve inside — both compose_path values they're built
// from can be attacker-influenced (a manual-mode migrate request supplies
// compose_path directly; see safepath's traversal check, which constrains
// the literal string but not a symlinked path component inside a
// checked-out repo). secrets.FindSecretsFile/ReadSecretsFile route through
// os.Root against these roots so such a component can't be followed outside
// the checkout.
func resolveSopsCheck(ctx context.Context, destRepo *core.Record, destRoot, destWorkDir, sourceRoot, sourceWorkDir string) SopsCheck {
	sourceHadSecrets := false
	if path, _ := secrets.FindSecretsFile(sourceRoot, sourceWorkDir); path != "" {
		sourceHadSecrets = true
	}

	destPath, content, _ := secrets.ReadSecretsFile(destRoot, destWorkDir)
	if destPath == "" {
		if sourceHadSecrets {
			return SopsCheck{Status: "source_had_secrets"}
		}
		return SopsCheck{Status: "none"}
	}

	targetPubKey := destRepo.GetString("sops_age_public_key")
	encryptedKey := destRepo.GetString("sops_age_key")
	if encryptedKey == "" {
		return SopsCheck{Status: "undecryptable", TargetAgePublicKey: targetPubKey}
	}

	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	ageKey, err := crypto.Decrypt(encryptedKey, secretKey)
	if err != nil {
		return SopsCheck{Status: "undecryptable", TargetAgePublicKey: targetPubKey}
	}

	if _, err := secrets.DecryptSecretsFile(ctx, content, string(ageKey)); err != nil {
		return SopsCheck{Status: "undecryptable", TargetAgePublicKey: targetPubKey}
	}
	return SopsCheck{Status: "ok"}
}

// resolveMigrateDestComposePaths derives the target compose_path/
// compose_file the same way for both the preview and the migrate mutation
// (§10.5 of the plan — they must never diverge). For a wireops-managed
// stack it re-parses the target's wireops.yaml, mirroring from-wireops
// (registerCreateFromWireopsRoute); for a manual stack it trusts the
// client-supplied paths, validated by the caller.
func resolveMigrateDestComposePaths(destRepoDir, destRepoID string, wireopsManaged bool, body migrateRequestBody) (composePath, composeFile string, err error) {
	if wireopsManaged {
		if body.WireopsFile == "" {
			return "", "", fmt.Errorf("wireops_file is required for a wireops-managed stack")
		}
		def, perr := manifest.ParseWireopsFile(config.GetReposWorkspace(), destRepoID, body.WireopsFile)
		if perr != nil {
			return "", "", perr
		}
		resolveWireopsComposeFile(destRepoDir, body.WireopsFile, def)
		if def.ResolutionError != "" {
			return "", "", fmt.Errorf("%s", def.ResolutionError)
		}
		return def.ResolvedComposePath, def.ResolvedComposeFile, nil
	}

	if body.ComposePath == "" && body.ComposeFile == "" {
		return "", "", fmt.Errorf("compose_path and compose_file are required")
	}
	composeFile = body.ComposeFile
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}
	return body.ComposePath, composeFile, nil
}

// composeWorkDir joins a repo checkout root with a (validated) compose_path.
func composeWorkDir(repoDir, composePath string) string {
	if composePath == "" || composePath == "." {
		return repoDir
	}
	return filepath.Join(repoDir, composePath)
}

// loadComposeConfigMap shells out to `docker compose config` for workDir/
// composeFile and parses the JSON result — the one place in this file that
// touches docker.
func loadComposeConfigMap(ctx context.Context, workDir, composeFile string, envVars []string, root string) (map[string]interface{}, error) {
	out, err := compose.Config(ctx, compose.ConfigOptions{
		WorkDir:     workDir,
		ComposeFile: composeFile,
		EnvVars:     envVars,
		Root:        root,
	}, true)
	if err != nil {
		return nil, err
	}
	return compose.ParseConfigJSON(out)
}

// resolveCurrentStackConfigMap resolves the source side of the diff: the
// stack's last rendered revision on disk when one exists (already the
// compose config actually running in production, per §4.1 step 6), falling
// back to a live `docker compose config` against the source repo checkout
// for a stack that has never synced yet.
func (rr routeRegistrar) resolveCurrentStackConfigMap(ctx context.Context, stack *core.Record, envVars []string) (map[string]interface{}, error) {
	if currentVersion := stack.GetInt("current_version"); currentVersion > 0 {
		renderer := sync.NewRenderer(rr.app)
		data, err := os.ReadFile(renderer.GetRevisionFilePath(stack.Id, currentVersion))
		if err == nil {
			var configMap map[string]interface{}
			if yerr := yaml.Unmarshal(data, &configMap); yerr == nil {
				return configMap, nil
			}
		}
	}

	workDir := stackWorkDir(rr.app, stack)
	root := filepath.Join(config.GetReposWorkspace(), stack.GetString("repository"))
	composeFile := stack.GetString("compose_file")
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}
	return loadComposeConfigMap(ctx, workDir, composeFile, envVars, root)
}

// registerMigratePreviewRoute registers POST
// /api/custom/stacks/{id}/migrate/preview — a read-only report of what
// migrating the stack to a different (already-registered) repository would
// change, so an operator can review it before committing via
// POST .../migrate. Never returns a 4xx for a config incompatibility —
// only for a malformed request or a lookup failure.
func (rr routeRegistrar) registerMigratePreviewRoute() {
	rr.r.POST("/api/custom/stacks/{id}/migrate/preview", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		var body migrateRequestBody
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if body.Repository == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "repository is required"})
		}
		sourceRepoID := stack.GetString("repository")
		if body.Repository == sourceRepoID {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "target repository is the same"})
		}
		destRepo, err := rr.app.FindRecordById("repositories", body.Repository)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "target repository not found"})
		}

		destRepoDir, ok := rr.repoFilesSetupByID(e, body.Repository)
		if !ok {
			return nil
		}

		wireopsManaged := stack.GetString("config_source") == "wireops_file"
		composePath, composeFile, err := resolveMigrateDestComposePaths(destRepoDir, body.Repository, wireopsManaged, body)
		if err != nil {
			return e.JSON(http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		}
		if verr := safepath.ValidateComposePath(composePath); verr != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": verr.Error()})
		}
		if verr := safepath.ValidateComposeFile(composeFile); verr != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": verr.Error()})
		}

		envVars, err := rr.scheduler.LoadStackEnvVars(e.Request.Context(), stackID)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to resolve env vars: " + err.Error()})
		}

		destWorkDir := composeWorkDir(destRepoDir, composePath)
		targetConfig, err := loadComposeConfigMap(e.Request.Context(), destWorkDir, composeFile, envVars, destRepoDir)
		if err != nil {
			return e.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "failed to resolve target compose config: " + err.Error()})
		}

		sourceConfig, err := rr.resolveCurrentStackConfigMap(e.Request.Context(), stack, envVars)
		if err != nil {
			return e.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "failed to resolve source compose config: " + err.Error()})
		}

		sourceRoot := filepath.Join(config.GetReposWorkspace(), stack.GetString("repository"))
		sops := resolveSopsCheck(e.Request.Context(), destRepo, destRepoDir, destWorkDir, sourceRoot, stackWorkDir(rr.app, stack))

		preview := buildMigratePreview(sourceRepoID, body.Repository, sourceConfig, targetConfig, sops)
		return e.JSON(http.StatusOK, preview)
	}).BindFunc(rbac.Require(rbac.CapManageRepos))
}

// registerMigrateRoute registers POST /api/custom/stacks/{id}/migrate — the
// mutating half of the preview/migrate pair. It re-points the stack's
// repository (+ derived compose paths) inside a transaction, resets the
// sync state so the next reconcile does a clean diff, then triggers that
// reconcile through the normal TriggerSync path (trigger="migrate"): no
// dedicated reconciler method needed, since re-pointing `repository` and
// clearing desired_commit/checksum is all ReconcileStack needs to treat
// this like any other config change (see plan §4.2's discarded
// TriggerMigrate alternative).
func (rr routeRegistrar) registerMigrateRoute() {
	rr.r.POST("/api/custom/stacks/{id}/migrate", func(e *core.RequestEvent) error {
		stackID := e.Request.PathValue("id")
		stack, err := rr.app.FindRecordById("stacks", stackID)
		if err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "stack not found"})
		}

		var body migrateRequestBody
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if !body.Confirm {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "confirm must be true"})
		}
		if body.Repository == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "repository is required"})
		}
		sourceRepoID := stack.GetString("repository")
		if body.Repository == sourceRepoID {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "target repository is the same"})
		}
		if _, err := rr.app.FindRecordById("repositories", body.Repository); err != nil {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "target repository not found"})
		}

		// Held across the whole validate-then-act section below (teardown +
		// mutation): a plain IsSyncing probe here would be a TOCTOU — nothing
		// would stop a cron-triggered reconcile, or a second concurrent
		// migrate request for the same stack, from acquiring the lock in the
		// gap between the check and the mutation. Released explicitly right
		// before TriggerSync (not deferred past it), since ReconcileStack
		// itself needs this same lock and would otherwise skip silently.
		release, ok := rr.scheduler.TryLockStack(stackID)
		if !ok {
			return e.JSON(http.StatusConflict, map[string]string{"error": "stack is currently syncing"})
		}
		var releaseOnce stdsync.Once
		releaseStackLock := func() { releaseOnce.Do(release) }
		defer releaseStackLock()

		destRepoDir, ok := rr.repoFilesSetupByID(e, body.Repository)
		if !ok {
			return nil
		}

		wireopsManaged := stack.GetString("config_source") == "wireops_file"
		composePath, composeFile, err := resolveMigrateDestComposePaths(destRepoDir, body.Repository, wireopsManaged, body)
		if err != nil {
			return e.JSON(http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		}
		if verr := safepath.ValidateComposePath(composePath); verr != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": verr.Error()})
		}
		if verr := safepath.ValidateComposeFile(composeFile); verr != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": verr.Error()})
		}

		oldPath := stack.GetString("compose_path")

		// Coordinated teardown (plan §6/Fase 4): when compose_path changes,
		// the docker compose project name changes with it (renderer.go
		// requires a resolved `name:`, normally derived from the compose
		// file's directory) — the new project's `--remove-orphans` has no
		// visibility into containers under the OLD project label, so they'd
		// otherwise sit there forever. Tearing the old project down here,
		// before the mutation below re-points the record, reclaims both the
		// orphaned containers and any host ports they held. This trades a
		// window of downtime for that cleanup, so it's opt-in
		// (teardown_old_project) rather than the default.
		if body.TeardownOldProject {
			if !rr.workerOnline(e, stackID) {
				return nil
			}
			if _, terr := rr.dispatchTeardownForMigration(e.Request.Context(), stack); terr != nil {
				return e.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("teardown of old project failed, migration aborted: %v", terr)})
			}
		}

		err = hooks.WithMigrationBypass(stackID, func() error {
			return rr.app.RunInTransaction(func(txApp core.App) error {
				txStack, ferr := txApp.FindRecordById("stacks", stackID)
				if ferr != nil {
					return ferr
				}
				txStack.Set("repository", body.Repository)
				txStack.Set("compose_path", composePath)
				txStack.Set("compose_file", composeFile)
				if wireopsManaged {
					txStack.Set("wireops_file_path", body.WireopsFile)
				}
				txStack.Set("status", "pending")
				txStack.Set("desired_commit", "")
				txStack.Set("checksum", "")
				return txApp.Save(txStack)
			})
		})
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to migrate stack: " + err.Error()})
		}

		recordMigrateAudit(rr.app, e, stackID, sourceRepoID, body.Repository, oldPath, composePath, body.TeardownOldProject)

		// Release before triggering the reconcile: ReconcileStack acquires
		// this same per-stack lock itself, and TryLock-based, not
		// blocking — still holding it here would make the reconcile skip as
		// "already syncing" instead of actually running.
		releaseStackLock()

		var userID string
		if e.Auth != nil {
			userID = e.Auth.Id
		}
		rr.scheduler.TriggerSync(stackID, "migrate", 0, userID)
		return e.JSON(http.StatusAccepted, map[string]string{"status": "migration_started"})
	}).BindFunc(rbac.Require(rbac.CapManageRepos))
}

func recordMigrateAudit(app core.App, e *core.RequestEvent, stackID, oldRepoID, newRepoID, oldPath, newPath string, tornDownOldProject bool) {
	audit.RecordRequest(app, e, audit.Event{
		Action:       "stack.migrate",
		ResourceType: "stacks",
		ResourceID:   stackID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"from_repository":       oldRepoID,
			"to_repository":         newRepoID,
			"from_path":             oldPath,
			"to_path":               newPath,
			"torn_down_old_project": tornDownOldProject,
		},
	})
}

// dispatchTeardownForMigration tears down stack's currently-deployed compose
// project — read from its last rendered revision, i.e. the OLD project,
// before the caller re-points repository/compose_path — mirroring the
// teardown block in registerStackDeleteRoute (stack_routes.go). Dispatched
// directly here rather than through a Reconciler method: internal/hooks
// already imports internal/sync (for Register's *sync.Scheduler param), so a
// sync-package method reaching for hooks.WithMigrationBypass would create an
// import cycle. Returns ("", nil) when the stack has never synced — nothing
// is running yet, so there is nothing to tear down.
func (rr routeRegistrar) dispatchTeardownForMigration(ctx context.Context, stack *core.Record) (string, error) {
	stackID := stack.Id
	currentVersion := stack.GetInt("current_version")
	if currentVersion == 0 {
		return "", nil
	}

	renderer := sync.NewRenderer(rr.app)
	composeContent, err := os.ReadFile(renderer.GetRevisionFilePath(stackID, currentVersion))
	if err != nil {
		return "", fmt.Errorf("failed to read rendered compose file for teardown: %w", err)
	}

	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	envVars, err := envvars.LoadStack(ctx, rr.app, secrets.NewDefaultRegistry(rr.app, secretKey), stackID)
	if err != nil {
		return "", fmt.Errorf("failed to load env vars for teardown: %w", err)
	}
	var envFileB64 string
	if len(envVars) > 0 {
		envFileB64, err = sync.BuildEnvFileB64(envVars)
		if err != nil {
			return "", fmt.Errorf("failed to serialize env vars for teardown: %w", err)
		}
	}

	workerID := stack.GetString("worker")
	result, dispatchErr := rr.workerSvc.Dispatch(ctx, workerID, protocol.TeardownCommand{
		CommandID:      fmt.Sprintf("migrate-teardown-%s", stackID),
		StackID:        stackID,
		ComposeFileB64: base64.StdEncoding.EncodeToString(composeContent),
		EnvFileB64:     envFileB64,
	})
	if dispatchErr != nil {
		return "", fmt.Errorf("teardown dispatch failed: %w", dispatchErr)
	}
	if result.Error != "" {
		return result.Output, fmt.Errorf("worker teardown failed: %s", result.Error)
	}
	return result.Output, nil
}
