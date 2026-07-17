package sync

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/constants"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/envvars"
	gitpkg "github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/notify"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/safepath"
	"github.com/wireops/wireops/internal/secrets"
)

// WorkerDispatcher defines how the reconciler sends compose commands to workers.
// The MTLSServer implements this interface.
type WorkerDispatcher interface {
	Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error)
	// IsConnected reports whether the worker currently has an active WebSocket connection.
	IsConnected(workerID string) bool
}

type Reconciler struct {
	app             core.App
	mu              sync.Map
	notifier        *notify.Notifier
	renderer        *Renderer
	dispatcher      WorkerDispatcher
	secretsRegistry *secrets.Registry
}

func NewReconciler(app core.App, notifier *notify.Notifier, dispatcher WorkerDispatcher) *Reconciler {
	key := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))

	reg := secrets.NewDefaultRegistry(app, key)

	return &Reconciler{
		app:             app,
		notifier:        notifier,
		renderer:        NewRenderer(app),
		dispatcher:      dispatcher,
		secretsRegistry: reg,
	}
}

// resolveWorker returns the assigned worker id and fingerprint for a stack.
func (r *Reconciler) resolveWorker(stack *core.Record) (workerID, fingerprint string, err error) {
	workerID = stack.GetString("worker")
	if workerID == "" {
		return "", "", fmt.Errorf("stack has no worker assigned")
	}
	worker, err := r.app.FindRecordById("workers", workerID)
	if err != nil {
		return "", "", fmt.Errorf("failed to find worker %s: %w", workerID, err)
	}
	return workerID, worker.GetString("fingerprint"), nil
}

// resolveComposeRuntimeFlags resolves the force_pull/remove_orphans flags to
// send with a deploy command. remove_orphans defaults to true (the
// historical, unconditional behavior) for every stack except one explicitly
// created from a wireops.yaml with remove_orphans: false.
func resolveComposeRuntimeFlags(stack *core.Record) (forcePull, removeOrphans bool) {
	forcePull = stack.GetBool("force_pull")
	removeOrphans = true
	if stack.GetString("config_source") == "wireops_file" && !stack.GetBool("remove_orphans") {
		removeOrphans = false
	}
	return forcePull, removeOrphans
}

// withDeployTimeout wraps ctx with a deadline: the stack's own
// deploy_timeout_seconds when positive (sourced from wireops.yaml's timeout
// field), otherwise the global default from config.GetDeployTimeout().
func withDeployTimeout(ctx context.Context, stack *core.Record) (context.Context, context.CancelFunc) {
	seconds := stack.GetInt("deploy_timeout_seconds")
	if seconds > 0 {
		return context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	}
	return context.WithTimeout(ctx, config.GetDeployTimeout())
}

// ReconcileStack fetches the repo, checks for changes, and deploys the compose stack.
func (r *Reconciler) ReconcileStack(ctx context.Context, stackID string, trigger string, queueTotal int) error {
	mu := r.stackMutex(stackID)
	if !mu.TryLock() {
		log.Printf("[reconciler] stack %s already syncing, skipping", stackID)
		return nil
	}
	defer mu.Unlock()

	stack, err := r.app.FindRecordById("stacks", stackID)
	if err != nil {
		return fmt.Errorf("stack not found: %w", err)
	}

	if stack.GetString("status") == "paused" {
		return nil
	}

	if stack.GetString("source_type") == "local" {
		return r.reconcileLocalStack(ctx, stackID, stack, trigger)
	}

	if err := r.checkSecretBackends(stackID, trigger, "", stack); err != nil {
		return err
	}

	prevStatus := stack.GetString("status")
	if err := r.saveRecordStatus(stack, "stacks", "syncing", fmt.Sprintf("start reconcile trigger=%s", trigger)); err != nil {
		return err
	}

	// --- git fetch ---
	gitFetchStart := time.Now()

	repoID := stack.GetString("repository")
	repo, err := r.app.FindRecordById("repositories", repoID)
	if err != nil {
		errMsg := fmt.Sprintf("repository %s not found for stack %s", repoID, stackID)
		r.logFailureWithPhase(stackID, trigger, "", errMsg, constants.PhaseGitFetch, gitFetchStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s: %w", errMsg, err)
	}

	gitAuth, err := r.resolveGitAuth(repoID)
	if err != nil {
		log.Printf("[reconciler] failed to resolve auth for repo %s; continuing without auth", repoID)
	}

	workspace := r.reposWorkspace()
	gitURL := repo.GetString("git_url")
	branch := repo.GetString("branch")
	if branch == "" {
		branch = "main"
	}

	repoDir := filepath.Join(workspace, repoID)
	if _, statErr := os.Stat(filepath.Join(repoDir, ".git")); os.IsNotExist(statErr) {
		_ = os.RemoveAll(repoDir)
		log.Printf("[reconciler] repo dir missing for %s, will clone fresh", repoID)
	}

	gitRepo, err := r.cloneOrFetchWithRetry(ctx, repoID, gitURL, branch, gitAuth, workspace)
	if err != nil {
		errMsg := fmt.Sprintf("git operation failed for repo %s (%s): %v", repo.GetString("name"), gitURL, err)
		r.logFailureWithPhase(stackID, trigger, "", errMsg, constants.PhaseGitFetch, gitFetchStart)
		r.markError(repo, "repositories")
		r.markError(stack, "stacks")
		return fmt.Errorf("git operation failed: %w", err)
	}

	remoteSHA, err := gitpkg.LocalHeadSHA(gitRepo)
	if err != nil {
		errMsg := fmt.Sprintf("failed to get local SHA after fetching branch %s: %v", branch, err)
		_ = r.logFailureWithPhase(stackID, trigger, "", errMsg, constants.PhaseGitFetch, gitFetchStart)
		if saveErr := r.saveRecordStatus(stack, "stacks", prevStatus, "restore status after local SHA failure"); saveErr != nil {
			return saveErr
		}
		return fmt.Errorf("failed to get local SHA: %w", err)
	}

	lastSHA := repo.GetString("last_commit_sha")

	repo.Set("last_commit_sha", remoteSHA)
	repo.Set("last_fetched_at", time.Now().UTC().Format(time.RFC3339))
	repo.Set("status", "connected")
	if err := r.saveRecord(repo, "repositories", "persist fetched repository state"); err != nil {
		_ = r.logFailureWithPhase(stackID, trigger, remoteSHA, err.Error(), constants.PhaseGitFetch, gitFetchStart)
		_ = r.markError(stack, "stacks")
		return err
	}

	workerID, workerFingerprint, err := r.resolveWorker(stack)
	if err != nil {
		errMsg := fmt.Sprintf("worker resolution failed: %v", err)
		r.logFailureWithPhase(stackID, trigger, "", errMsg, constants.PhaseGitFetch, gitFetchStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	// git_fetch phase (repo lookup, clone/fetch, local SHA, persist repo,
	// worker resolution) is now known-complete — its sync_log_phases row is
	// written retroactively once we know which sync_logs row (if any) this
	// deploy attempt ends up with, below.
	gitFetchDuration := time.Since(gitFetchStart).Milliseconds()

	isOnline := r.dispatcher != nil && r.dispatcher.IsConnected(workerID)

	neverSynced := stack.GetString("last_synced_at") == ""
	repoChanged := gitpkg.HasChanged(remoteSHA, lastSHA)

	if !isOnline {
		if repoChanged || trigger != "cron" {
			log.Printf("[reconciler] worker %s is offline, queueing pending reconcile for stack %s", workerID, stackID)
			if err := r.queuePendingReconcile(stackID, trigger, remoteSHA); err != nil {
				_ = r.logFailure(stackID, trigger, remoteSHA, err.Error())
				_ = r.markError(stack, "stacks")
				return err
			}
			if err := r.saveRecordStatus(stack, "stacks", "pending", "mark stack pending after offline queue"); err != nil {
				return err
			}
			return nil
		}
		// Worker offline but no changes and it's a cron, just skip quietly.
		if err := r.saveRecordStatus(stack, "stacks", prevStatus, "restore status after offline cron skip"); err != nil {
			return err
		}
		return nil
	}

	// Worker is online. Fetch the currently running commit SHA from the worker.
	// This is used as a fast-path skip for the cron trigger: if the container is
	// already running the expected commit AND the repo hasn't changed, we can skip
	// without even running the renderer. However, this check can fail if docker
	// compose didn't recreate the container (e.g. only wireops labels changed),
	// leaving a stale commit_sha label. The renderer-based skip below handles that.
	containerSHA := ""
	if !neverSynced {
		containerSHA = r.inspectStackCommit(ctx, workerID, stackID)
	}

	if trigger == "cron" && !neverSynced && !repoChanged && containerSHA == remoteSHA {
		if err := r.saveRecordStatus(stack, "stacks", prevStatus, "restore status after unchanged container skip"); err != nil {
			return err
		}
		return nil
	}

	commitMsg := ""
	if obj, err := gitRepo.CommitObject(mustParseHash(remoteSHA)); err == nil {
		commitMsg = obj.Message
	}

	reusedSyncLog, err := r.waitForRunningJobs(ctx, stack, repoID, stackID, trigger, remoteSHA)
	if err != nil {
		return err
	}

	// --- compose deploy ---
	renderStart := time.Now()
	workDir, err := r.stackWorkDir(stack, repoID)
	if err != nil {
		errMsg := fmt.Sprintf("invalid compose_path: %v", err)
		r.logFailureWithPhase(stackID, trigger, remoteSHA, errMsg, constants.PhaseRender, renderStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	composeFile, err := r.resolveComposeFile(stack, workDir, stackID, trigger, remoteSHA)
	if err != nil {
		return err
	}

	envVars, envErr := r.loadEnvVars(ctx, stackID)
	if envErr != nil {
		errMsg := fmt.Sprintf("failed to load env vars: %v", envErr)
		r.logFailureWithPhase(stackID, trigger, remoteSHA, errMsg, constants.PhaseRender, renderStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	sopsValues, sopsErr := r.loadSopsEnv(ctx, repo, workDir)
	if sopsErr != nil {
		errMsg := fmt.Sprintf("failed to decrypt SOPS secrets file: %v", sopsErr)
		r.logFailureWithPhase(stackID, trigger, remoteSHA, errMsg, constants.PhaseRender, renderStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	envVars = overlaySopsEnv(envVars, sopsValues)

	// Write .env to the repo workDir NOW so that compose config (called by
	// GenerateRevision below via compose.Config) can resolve ${VAR} interpolations.
	// The actual docker compose up runs from the rendered dir — that copy is written later.
	if envWriteErr := WriteEnvFile(workDir, envVars); envWriteErr != nil {
		log.Printf("[reconciler] warning: failed to write .env to repo dir for stack %s: %v", stackID, envWriteErr)
	}
	if giErr := EnsureGitignoreHasEnv(workDir); giErr != nil {
		log.Printf("[reconciler] warning: failed to update .gitignore for stack %s: %v", stackID, giErr)
	}

	// Reload stack after possible checksum/version update by renderer setup.
	// (stack record may have been modified above by markError etc.)
	stack, err = r.app.FindRecordById("stacks", stackID)
	if err != nil {
		return fmt.Errorf("stack vanished mid-reconcile: %w", err)
	}
	prevChecksum := stack.GetString("checksum")
	prevVersion := stack.GetInt("current_version")

	renderRes, err := r.renderer.GenerateRevision(ctx, stack, repo, workDir, composeFile, envVars, remoteSHA, false, workerID, workerFingerprint)
	if err != nil {
		errMsg := fmt.Sprintf("failed to generate label revision: %v", err)
		r.logFailureWithPhase(stackID, trigger, remoteSHA, errMsg, constants.PhaseRender, renderStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	renderDuration := time.Since(renderStart).Milliseconds()

	// If the renderer found no changes (same checksum -> same version returned),
	// the compose file content is identical to what's already deployed. Skip the
	// deploy regardless of whether the commit SHA changed — a new commit may have
	// only touched other files in the repo (e.g. README, job.yaml, etc.).
	// The compose checksum is the definitive signal, not the commit SHA.
	if !neverSynced && renderRes.Checksum == prevChecksum && renderRes.Version == prevVersion {
		log.Printf("[reconciler] %s skip: compose unchanged for stack %s (checksum=%s)", trigger, stackID, renderRes.Checksum)
		if err := r.saveRecordStatus(stack, "stacks", prevStatus, "restore status after unchanged compose skip"); err != nil {
			return err
		}
		if trigger != "cron" {
			output := fmt.Sprintf(
				"No changes detected.\n\nRendered compose checksum: %s\nRevision: v%d\nDeployment skipped because the active stack already matches the desired compose state.",
				renderRes.Checksum,
				renderRes.Version,
			)
			return r.logNoopSyncWithPhases(ctx, stack, stackID, trigger, remoteSHA, commitMsg, output, reusedSyncLog, gitFetchStart, gitFetchDuration, renderStart, renderDuration)
		}
		return nil
	}

	var syncLog *core.Record
	if reusedSyncLog != nil {
		syncLog = reusedSyncLog
	} else {
		syncLog, err = r.createSyncLog(stackID, trigger, remoteSHA, commitMsg)
		if err != nil {
			return fmt.Errorf("failed to create sync log: %w", err)
		}
	}

	pt := newPhaseTracker(r.app, syncLog.Id)
	defer pt.finishCurrentAsError("deploy aborted")
	_ = pt.recordCompleted(constants.PhaseGitFetch, constants.PhaseStatusSuccess, gitFetchStart, gitFetchDuration, "")
	if reusedSyncLog == nil {
		_ = pt.recordSkipped(constants.PhasePolicyCheck, "no wait needed")
	}
	_ = pt.recordCompleted(constants.PhaseRender, constants.PhaseStatusSuccess, renderStart, renderDuration, fmt.Sprintf("checksum=%s version=%d", renderRes.Checksum, renderRes.Version))

	r.notifier.Dispatch(ctx, notify.Payload{
		Event:     notify.SyncStarted,
		StackID:   stackID,
		StackName: stack.GetString("name"),
		SyncLogID: syncLog.Id,
		Trigger:   trigger,
		CommitSHA: remoteSHA,
	})

	renderedFilePath := r.renderer.GetRevisionFilePath(stackID, renderRes.Version)
	var output string
	var runErr error
	var duration int64
	var lastComposeContent []byte

	_ = pt.start(constants.PhaseDispatch)

	var lastAttemptStart time.Time
	var composeUpMs int64

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		start := time.Now()
		lastAttemptStart = start
		composeContent, err := r.readRenderedCompose(stack, stackID, trigger, remoteSHA, renderedFilePath)
		if err != nil {
			return err
		}
		lastComposeContent = composeContent
		envFileB64, b64Err := buildEnvFileB64(envVars)
		if b64Err != nil {
			runErr = fmt.Errorf("failed to serialize env vars for remote deploy: %w", b64Err)
		} else {
			forcePull, removeOrphans := resolveComposeRuntimeFlags(stack)
			dispatchCtx, cancelDispatch := withDeployTimeout(ctx, stack)
			result, dispatchErr := r.dispatcher.Dispatch(dispatchCtx, workerID, protocol.DeployCommand{
				CommandID:      syncLog.Id,
				StackID:        stackID,
				CommitSHA:      remoteSHA,
				Trigger:        trigger,
				QueueTotal:     queueTotal,
				ComposeFileB64: base64.StdEncoding.EncodeToString(composeContent),
				EnvFileB64:     envFileB64,
				ForcePull:      forcePull,
				RemoveOrphans:  removeOrphans,
			})
			cancelDispatch()
			composeUpMs = result.ComposeUpMs
			output, runErr = extractDispatchResult(result, dispatchErr)
		}

		duration += time.Since(start).Milliseconds()

		if runErr == nil {
			break // Success
		}

		if attempt < maxRetries {
			log.Printf("[reconciler] deploy attempt %d of %d failed for stack %s: %v, retrying in 3s...", attempt, maxRetries, stackID, runErr)
			if syncLog != nil {
				if err := r.updateSyncLog(syncLog.Id, "running", fmt.Sprintf("%s\n\n[Attempt %d failed: %v. Retrying in 3s...]\n", output, attempt, runErr), duration); err != nil {
					_ = r.markError(stack, "stacks")
					return err
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}
	}

	if runErr != nil {
		_ = pt.finish(constants.PhaseDispatch, constants.PhaseStatusError, runErr.Error())
		errOutput := buildErrorOutput(output, runErr)
		if err := r.updateSyncLog(syncLog.Id, "error", errOutput, duration); err != nil {
			_ = r.markError(stack, "stacks")
			return err
		}
		if err := r.markError(stack, "stacks"); err != nil {
			return err
		}
		_ = pt.start(constants.PhaseNotify)
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:      notify.SyncError,
			StackID:    stackID,
			StackName:  stack.GetString("name"),
			SyncLogID:  syncLog.Id,
			Trigger:    trigger,
			CommitSHA:  remoteSHA,
			DurationMs: duration,
			Error:      runErr.Error(),
		})
		_ = pt.finish(constants.PhaseNotify, constants.PhaseStatusSuccess, "")
		return runErr
	}
	_ = pt.finish(constants.PhaseDispatch, constants.PhaseStatusSuccess, "")
	r.recordWorkerAckAndComposeUpPhases(pt, syncLog.Id, lastAttemptStart, composeUpMs)

	_ = pt.start(constants.PhasePostCheck)
	check := r.postDeployCheck(ctx, workerID, stackID, workDir, lastComposeContent)
	output = output + "\n\n[post-check] " + check.Detail
	postCheckStatus := constants.PhaseStatusSuccess
	if check.Status == "error" || check.Status == "degraded" {
		postCheckStatus = constants.PhaseStatusError
	}
	_ = pt.finish(constants.PhasePostCheck, postCheckStatus, check.Detail)

	stack.Set("last_synced_at", time.Now().UTC().Format(time.RFC3339))
	stack.Set("status", check.Status)
	stack.Set("deployed_version", renderRes.Version)
	stack.Set("deployed_commit", remoteSHA)
	stack.Set("deployed_checksum", renderRes.Checksum)
	stack.Set("deployed_at", time.Now().UTC().Format(time.RFC3339))
	if err := r.saveRecord(stack, "stacks", "complete reconcile"); err != nil {
		_ = r.updateSyncLog(syncLog.Id, "error", "worker deploy succeeded but failed to persist stack success: "+err.Error(), duration)
		return err
	}
	logStatus := "success"
	if check.Status != "active" {
		logStatus = check.Status
	}
	if err := r.updateSyncLog(syncLog.Id, logStatus, output, duration); err != nil {
		_ = r.markError(stack, "stacks")
		return err
	}
	notifyEvent := notify.SyncDone
	notifyErr := ""
	if check.Status == "error" {
		notifyEvent = notify.SyncError
		notifyErr = check.Detail
	}
	_ = pt.start(constants.PhaseNotify)
	r.notifier.Dispatch(ctx, notify.Payload{
		Event:      notifyEvent,
		StackID:    stackID,
		StackName:  stack.GetString("name"),
		SyncLogID:  syncLog.Id,
		Trigger:    trigger,
		CommitSHA:  remoteSHA,
		DurationMs: duration,
		Error:      notifyErr,
	})
	_ = pt.finish(constants.PhaseNotify, constants.PhaseStatusSuccess, "")

	return nil
}

// RollbackStack resets the repo to a given commit and redeploys the stack.
func (r *Reconciler) RollbackStack(ctx context.Context, stackID string, commitSHA string) error {
	mu := r.stackMutex(stackID)
	if !mu.TryLock() {
		return fmt.Errorf("stack %s already syncing", stackID)
	}
	defer mu.Unlock()

	stack, err := r.app.FindRecordById("stacks", stackID)
	if err != nil {
		return fmt.Errorf("stack not found: %w", err)
	}

	if err := r.checkSecretBackends(stackID, "manual", commitSHA, stack); err != nil {
		return err
	}

	if err := r.saveRecordStatus(stack, "stacks", "syncing", "start rollback"); err != nil {
		return err
	}

	gitFetchStart := time.Now()

	repoID := stack.GetString("repository")
	repo, err := r.app.FindRecordById("repositories", repoID)
	if err != nil {
		errMsg := fmt.Sprintf("repository %s not found for stack %s", repoID, stackID)
		r.logFailureWithPhase(stackID, "manual", commitSHA, errMsg, constants.PhaseGitFetch, gitFetchStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s: %w", errMsg, err)
	}

	workspace := r.reposWorkspace()
	repoDir := filepath.Join(workspace, repoID)

	gogitRepo, err := gogit.PlainOpen(repoDir)
	if err != nil {
		errMsg := fmt.Sprintf("failed to open local repo directory: %s", repoDir)
		r.logFailureWithPhase(stackID, "manual", commitSHA, errMsg, constants.PhaseGitFetch, gitFetchStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("failed to open repo: %w", err)
	}

	wt, err := gogitRepo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	if err := wt.Reset(&gogit.ResetOptions{
		Commit: mustParseHash(commitSHA),
		Mode:   gogit.HardReset,
	}); err != nil {
		errMsg := fmt.Sprintf("git reset to %s failed: %v", commitSHA, err)
		r.logFailureWithPhase(stackID, "manual", commitSHA, errMsg, constants.PhaseGitFetch, gitFetchStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("git reset failed: %w", err)
	}
	gitFetchDuration := time.Since(gitFetchStart).Milliseconds()

	renderStart := time.Now()
	workDir, err := r.stackWorkDir(stack, repoID)
	if err != nil {
		errMsg := fmt.Sprintf("invalid compose_path: %v", err)
		r.logFailureWithPhase(stackID, "manual", commitSHA, errMsg, constants.PhaseRender, renderStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	composeFile, err := r.resolveComposeFile(stack, workDir, stackID, "manual", commitSHA)
	if err != nil {
		return err
	}

	envVars, envErr := r.loadEnvVars(ctx, stackID)
	if envErr != nil {
		errMsg := fmt.Sprintf("failed to load env vars: %v", envErr)
		r.logFailureWithPhase(stackID, "manual", commitSHA, errMsg, constants.PhaseRender, renderStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	sopsValues, sopsErr := r.loadSopsEnv(ctx, repo, workDir)
	if sopsErr != nil {
		errMsg := fmt.Sprintf("failed to decrypt SOPS secrets file: %v", sopsErr)
		r.logFailureWithPhase(stackID, "manual", commitSHA, errMsg, constants.PhaseRender, renderStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	envVars = overlaySopsEnv(envVars, sopsValues)

	workerID, workerFingerprint, err := r.resolveWorker(stack)
	if err != nil {
		errMsg := fmt.Sprintf("worker resolution failed: %v", err)
		r.logFailureWithPhase(stackID, "manual", commitSHA, errMsg, constants.PhaseGitFetch, gitFetchStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	// Write .env to workDir so that compose config (called inside
	// GenerateRevision) can resolve ${VAR} interpolations from the repo file.
	if envWriteErr := WriteEnvFile(workDir, envVars); envWriteErr != nil {
		log.Printf("[reconciler] warning: failed to write .env to repo dir for stack %s (rollback): %v", stackID, envWriteErr)
	}
	if giErr := EnsureGitignoreHasEnv(workDir); giErr != nil {
		log.Printf("[reconciler] warning: failed to update .gitignore for stack %s (rollback): %v", stackID, giErr)
	}

	renderRes, err := r.renderer.GenerateRevision(ctx, stack, repo, workDir, composeFile, envVars, commitSHA, true, workerID, workerFingerprint)
	if err != nil {
		errMsg := fmt.Sprintf("failed to generate label revision on rollback: %v", err)
		r.logFailureWithPhase(stackID, "manual", commitSHA, errMsg, constants.PhaseRender, renderStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	renderDuration := time.Since(renderStart).Milliseconds()

	syncLog, err := r.createSyncLog(stackID, "manual", commitSHA, "rollback to "+commitSHA)
	if err != nil {
		_ = r.markError(stack, "stacks")
		return err
	}

	pt := newPhaseTracker(r.app, syncLog.Id)
	defer pt.finishCurrentAsError("rollback aborted")
	_ = pt.recordCompleted(constants.PhaseGitFetch, constants.PhaseStatusSuccess, gitFetchStart, gitFetchDuration, "")
	_ = pt.recordSkipped(constants.PhasePolicyCheck, "n/a: rollback")
	_ = pt.recordCompleted(constants.PhaseRender, constants.PhaseStatusSuccess, renderStart, renderDuration, fmt.Sprintf("checksum=%s version=%d", renderRes.Checksum, renderRes.Version))

	r.notifier.Dispatch(ctx, notify.Payload{
		Event:     notify.SyncStarted,
		StackID:   stackID,
		StackName: stack.GetString("name"),
		SyncLogID: syncLog.Id,
		Trigger:   "manual",
		CommitSHA: commitSHA,
	})

	start := time.Now()
	_ = pt.start(constants.PhaseDispatch)

	renderedFilePath := r.renderer.GetRevisionFilePath(stackID, renderRes.Version)
	var output string
	var runErr error

	composeContent, err := r.readRenderedCompose(stack, stackID, "manual", commitSHA, renderedFilePath)
	if err != nil {
		return err
	}
	var cmdID string
	if syncLog != nil {
		cmdID = syncLog.Id
	}
	var composeUpMs int64
	envFileB64, b64Err := buildEnvFileB64(envVars)
	if b64Err != nil {
		runErr = fmt.Errorf("failed to serialize env vars for remote rollback: %w", b64Err)
	} else {
		forcePull, removeOrphans := resolveComposeRuntimeFlags(stack)
		dispatchCtx, cancelDispatch := withDeployTimeout(ctx, stack)
		defer cancelDispatch()
		result, dispatchErr := r.dispatcher.Dispatch(dispatchCtx, workerID, protocol.DeployCommand{
			CommandID:      cmdID,
			StackID:        stackID,
			CommitSHA:      commitSHA,
			Trigger:        "rollback",
			ComposeFileB64: base64.StdEncoding.EncodeToString(composeContent),
			EnvFileB64:     envFileB64,
			ForcePull:      forcePull,
			RemoveOrphans:  removeOrphans,
		})
		composeUpMs = result.ComposeUpMs
		output, runErr = extractDispatchResult(result, dispatchErr)
	}

	duration := time.Since(start).Milliseconds()

	if runErr != nil {
		_ = pt.finish(constants.PhaseDispatch, constants.PhaseStatusError, runErr.Error())
		errOutput := buildErrorOutput(output, runErr)
		if err := r.updateSyncLog(syncLog.Id, "error", errOutput, duration); err != nil {
			_ = r.markError(stack, "stacks")
			return err
		}
		_ = pt.start(constants.PhaseNotify)
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:      notify.SyncError,
			StackID:    stackID,
			StackName:  stack.GetString("name"),
			SyncLogID:  syncLog.Id,
			Trigger:    "manual",
			CommitSHA:  commitSHA,
			DurationMs: duration,
			Error:      runErr.Error(),
		})
		_ = pt.finish(constants.PhaseNotify, constants.PhaseStatusSuccess, "")
		if err := r.markError(stack, "stacks"); err != nil {
			return err
		}
		return runErr
	}
	_ = pt.finish(constants.PhaseDispatch, constants.PhaseStatusSuccess, "")
	r.recordWorkerAckAndComposeUpPhases(pt, cmdID, start, composeUpMs)

	repo.Set("last_commit_sha", commitSHA)
	repo.Set("last_fetched_at", time.Now().UTC().Format(time.RFC3339))
	if err := r.saveRecord(repo, "repositories", "persist rollback repository state"); err != nil {
		_ = r.updateSyncLog(syncLog.Id, "error", "rollback succeeded but failed to persist repository state: "+err.Error(), duration)
		return err
	}

	_ = pt.start(constants.PhasePostCheck)
	check := r.postDeployCheck(ctx, workerID, stackID, workDir, composeContent)
	output = output + "\n\n[post-check] " + check.Detail
	postCheckStatus := constants.PhaseStatusSuccess
	if check.Status == "error" || check.Status == "degraded" {
		postCheckStatus = constants.PhaseStatusError
	}
	_ = pt.finish(constants.PhasePostCheck, postCheckStatus, check.Detail)

	// Rollback's terminal success state is historically "paused" (to avoid
	// GitOps re-syncing it back to HEAD), not "active" — only remap it when
	// the post-check found a real problem.
	rollbackStatus := "paused"
	if check.Status != "active" {
		rollbackStatus = check.Status
	}

	stack.Set("last_synced_at", time.Now().UTC().Format(time.RFC3339))
	stack.Set("status", rollbackStatus)
	stack.Set("deployed_version", renderRes.Version)
	stack.Set("deployed_commit", commitSHA)
	stack.Set("deployed_checksum", renderRes.Checksum)
	stack.Set("deployed_at", time.Now().UTC().Format(time.RFC3339))
	if err := r.saveRecord(stack, "stacks", "complete rollback"); err != nil {
		_ = r.updateSyncLog(syncLog.Id, "error", "rollback succeeded but failed to persist stack state: "+err.Error(), duration)
		return err
	}
	logStatus := "success"
	if check.Status != "active" {
		logStatus = check.Status
	}
	if err := r.updateSyncLog(syncLog.Id, logStatus, output, duration); err != nil {
		_ = r.markError(stack, "stacks")
		return err
	}
	notifyEvent := notify.SyncDone
	notifyErr := ""
	if check.Status == "error" {
		notifyEvent = notify.SyncError
		notifyErr = check.Detail
	}
	_ = pt.start(constants.PhaseNotify)
	r.notifier.Dispatch(ctx, notify.Payload{
		Event:      notifyEvent,
		StackID:    stackID,
		StackName:  stack.GetString("name"),
		SyncLogID:  syncLog.Id,
		Trigger:    "manual",
		CommitSHA:  commitSHA,
		DurationMs: duration,
		Error:      notifyErr,
	})
	_ = pt.finish(constants.PhaseNotify, constants.PhaseStatusSuccess, "")

	return nil
}

// ForceRedeployStack runs a force redeploy with recreate options, logs it, and pauses the stack.
func (r *Reconciler) ForceRedeployStack(ctx context.Context, stackID string, recreateContainers, recreateVolumes, recreateNetworks bool) error {
	mu := r.stackMutex(stackID)
	if !mu.TryLock() {
		return fmt.Errorf("stack %s already syncing", stackID)
	}
	defer mu.Unlock()

	stack, err := r.app.FindRecordById("stacks", stackID)
	if err != nil {
		return fmt.Errorf("stack not found: %w", err)
	}

	if err := r.checkSecretBackends(stackID, "redeploy", "", stack); err != nil {
		return err
	}

	if err := r.saveRecordStatus(stack, "stacks", "syncing", "start force redeploy"); err != nil {
		return err
	}

	repoID := stack.GetString("repository")
	repo, err := r.app.FindRecordById("repositories", repoID)
	if err != nil {
		errMsg := fmt.Sprintf("repository %s not found for stack %s", repoID, stackID)
		r.logFailure(stackID, "redeploy", "", errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s: %w", errMsg, err)
	}

	lastSHA := repo.GetString("last_commit_sha")
	syncLog, err := r.createSyncLog(stackID, "redeploy", lastSHA, "force redeploy")
	if err != nil {
		_ = r.markError(stack, "stacks")
		return err
	}
	if r.notifier != nil {
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:     notify.SyncStarted,
			StackID:   stackID,
			StackName: stack.GetString("name"),
			SyncLogID: syncLog.Id,
			Trigger:   "redeploy",
			CommitSHA: lastSHA,
		})
	}

	pt := newPhaseTracker(r.app, syncLog.Id)
	defer pt.finishCurrentAsError("force redeploy aborted")
	// Force redeploy reuses the repository's already-known last_commit_sha —
	// there's no git fetch or wait_running_jobs step in this flow.
	_ = pt.recordSkipped(constants.PhaseGitFetch, "n/a: force redeploy uses last known commit")
	_ = pt.recordSkipped(constants.PhasePolicyCheck, "n/a: force redeploy")
	_ = pt.start(constants.PhaseRender)

	failRedeploy := func(errMsg string, duration int64) error {
		pt.finishCurrentAsError(errMsg)
		if err := r.updateSyncLog(syncLog.Id, "error", errMsg, duration); err != nil {
			_ = r.markError(stack, "stacks")
			return err
		}
		if r.notifier != nil {
			r.notifier.Dispatch(ctx, notify.Payload{
				Event:      notify.SyncError,
				StackID:    stackID,
				StackName:  stack.GetString("name"),
				SyncLogID:  syncLog.Id,
				Trigger:    "redeploy",
				CommitSHA:  lastSHA,
				DurationMs: duration,
				Error:      errMsg,
			})
		}
		if err := r.markError(stack, "stacks"); err != nil {
			return err
		}
		return fmt.Errorf("%s", errMsg)
	}

	start := time.Now()

	workDir, err := r.stackWorkDir(stack, repoID)
	if err != nil {
		errMsg := fmt.Sprintf("invalid compose_path: %v", err)
		return failRedeploy(errMsg, time.Since(start).Milliseconds())
	}
	composeFile := stack.GetString("compose_file")
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}

	envVars, envErr := r.loadEnvVars(ctx, stackID)
	if envErr != nil {
		errMsg := fmt.Sprintf("failed to load env vars: %v", envErr)
		return failRedeploy(errMsg, time.Since(start).Milliseconds())
	}

	sopsValues, sopsErr := r.loadSopsEnv(ctx, repo, workDir)
	if sopsErr != nil {
		errMsg := fmt.Sprintf("failed to decrypt SOPS secrets file: %v", sopsErr)
		return failRedeploy(errMsg, time.Since(start).Milliseconds())
	}
	envVars = overlaySopsEnv(envVars, sopsValues)

	workerID, workerFingerprint, err := r.resolveWorker(stack)
	if err != nil {
		errMsg := fmt.Sprintf("worker resolution failed: %v", err)
		return failRedeploy(errMsg, time.Since(start).Milliseconds())
	}

	// Write .env to workDir so that compose config (called inside
	// GenerateRevision) can resolve ${VAR} interpolations from the repo file.
	if envWriteErr := WriteEnvFile(workDir, envVars); envWriteErr != nil {
		log.Printf("[reconciler] warning: failed to write .env to repo dir for stack %s (redeploy): %v", stackID, envWriteErr)
	}
	if giErr := EnsureGitignoreHasEnv(workDir); giErr != nil {
		log.Printf("[reconciler] warning: failed to update .gitignore for stack %s (redeploy): %v", stackID, giErr)
	}

	renderRes, err := r.renderer.GenerateRevision(ctx, stack, repo, workDir, composeFile, envVars, lastSHA, true, workerID, workerFingerprint)
	if err != nil {
		errMsg := fmt.Sprintf("failed to generate label revision on redeploy: %v", err)
		return failRedeploy(errMsg, time.Since(start).Milliseconds())
	}

	renderedFilePath := r.renderer.GetRevisionFilePath(stackID, renderRes.Version)
	var output string
	var runErr error

	composeContent, err := r.readRenderedCompose(stack, stackID, "redeploy", lastSHA, renderedFilePath)
	if err != nil {
		return failRedeploy(err.Error(), time.Since(start).Milliseconds())
	}
	_ = pt.finish(constants.PhaseRender, constants.PhaseStatusSuccess, fmt.Sprintf("checksum=%s version=%d", renderRes.Checksum, renderRes.Version))
	_ = pt.start(constants.PhaseDispatch)
	dispatchStart := time.Now()
	var composeUpMs int64
	var cmdID string
	if syncLog != nil {
		cmdID = syncLog.Id
	}
	envFileB64, b64Err := buildEnvFileB64(envVars)
	if b64Err != nil {
		runErr = fmt.Errorf("failed to serialize env vars for remote redeploy: %w", b64Err)
	} else {
		forcePull, removeOrphans := resolveComposeRuntimeFlags(stack)
		dispatchCtx, cancelDispatch := withDeployTimeout(ctx, stack)
		defer cancelDispatch()
		result, dispatchErr := r.dispatcher.Dispatch(dispatchCtx, workerID, protocol.RedeployCommand{
			DeployCommand: protocol.DeployCommand{
				CommandID:      cmdID,
				StackID:        stackID,
				CommitSHA:      lastSHA,
				Trigger:        "force-redeploy",
				ComposeFileB64: base64.StdEncoding.EncodeToString(composeContent),
				EnvFileB64:     envFileB64,
				ForcePull:      forcePull,
				RemoveOrphans:  removeOrphans,
			},
			RecreateContainers: recreateContainers,
			RecreateVolumes:    recreateVolumes,
			RecreateNetworks:   recreateNetworks,
		})
		composeUpMs = result.ComposeUpMs
		output, runErr = extractDispatchResult(result, dispatchErr)
	}

	duration := time.Since(start).Milliseconds()

	if runErr != nil {
		errOutput := buildErrorOutput(output, runErr)
		return failRedeploy(errOutput, duration)
	}
	_ = pt.finish(constants.PhaseDispatch, constants.PhaseStatusSuccess, "")
	r.recordWorkerAckAndComposeUpPhases(pt, cmdID, dispatchStart, composeUpMs)

	_ = pt.start(constants.PhasePostCheck)
	check := r.postDeployCheck(ctx, workerID, stackID, workDir, composeContent)
	output = output + "\n\n[post-check] " + check.Detail
	postCheckStatus := constants.PhaseStatusSuccess
	if check.Status == "error" || check.Status == "degraded" {
		postCheckStatus = constants.PhaseStatusError
	}
	_ = pt.finish(constants.PhasePostCheck, postCheckStatus, check.Detail)

	redeployStatus := "paused"
	if check.Status != "active" {
		redeployStatus = check.Status
	}

	stack.Set("last_synced_at", time.Now().UTC().Format(time.RFC3339))
	stack.Set("status", redeployStatus)
	stack.Set("deployed_version", renderRes.Version)
	stack.Set("deployed_commit", lastSHA)
	stack.Set("deployed_checksum", renderRes.Checksum)
	stack.Set("deployed_at", time.Now().UTC().Format(time.RFC3339))
	if err := r.saveRecord(stack, "stacks", "complete force redeploy"); err != nil {
		_ = r.updateSyncLog(syncLog.Id, "error", "redeploy succeeded but failed to persist stack state: "+err.Error(), duration)
		return err
	}
	logStatus := "success"
	if check.Status != "active" {
		logStatus = check.Status
	}
	if err := r.updateSyncLog(syncLog.Id, logStatus, output, duration); err != nil {
		_ = r.markError(stack, "stacks")
		return err
	}
	_ = pt.start(constants.PhaseNotify)
	if r.notifier != nil {
		notifyEvent := notify.SyncDone
		notifyErr := ""
		if check.Status == "error" {
			notifyEvent = notify.SyncError
			notifyErr = check.Detail
		}
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:      notifyEvent,
			StackID:    stackID,
			StackName:  stack.GetString("name"),
			SyncLogID:  syncLog.Id,
			Trigger:    "redeploy",
			CommitSHA:  lastSHA,
			DurationMs: duration,
			Error:      notifyErr,
		})
	}
	_ = pt.finish(constants.PhaseNotify, constants.PhaseStatusSuccess, "")

	return nil
}

// --- helpers ---

func (r *Reconciler) stackMutex(stackID string) *sync.Mutex {
	mu, _ := r.mu.LoadOrStore(stackID, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

func (r *Reconciler) stackWorkDir(stack *core.Record, repoID string) (string, error) {
	workspace := r.reposWorkspace()
	base := filepath.Join(workspace, repoID)
	composePath := stack.GetString("compose_path")
	if err := safepath.ValidateComposePath(composePath); err != nil {
		return "", err
	}
	if composePath != "" && composePath != "." {
		return filepath.Join(base, composePath), nil
	}
	return base, nil
}

// reconcileLocalStack handles the reconcile loop for stacks imported from a local
// filesystem path (source_type=local), bypassing the git fetch flow.
func (r *Reconciler) reconcileLocalStack(ctx context.Context, stackID string, stack *core.Record, trigger string) (retErr error) {
	if err := r.checkSecretBackends(stackID, trigger, "", stack); err != nil {
		return err
	}

	importPath := stack.GetString("import_path")
	if importPath == "" {
		errMsg := "import_path is required for local stacks"
		r.logFailure(stackID, trigger, "", errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	workerID, workerFingerprint, err := r.resolveWorker(stack)
	if err != nil {
		errMsg := fmt.Sprintf("worker resolution failed: %v", err)
		r.logFailure(stackID, trigger, "", errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	isOnline := r.dispatcher != nil && r.dispatcher.IsConnected(workerID)
	if !isOnline {
		log.Printf("[reconciler] worker %s is offline, queueing pending reconcile for local stack %s", workerID, stackID)
		if err := r.queuePendingReconcile(stackID, trigger, ""); err != nil {
			_ = r.logFailure(stackID, trigger, "", err.Error())
			_ = r.markError(stack, "stacks")
			return err
		}
		if err := r.saveRecordStatus(stack, "stacks", "pending", "mark local stack pending after offline queue"); err != nil {
			return err
		}
		return nil
	}

	prevStatus := stack.GetString("status")
	if err := r.saveRecordStatus(stack, "stacks", "syncing", fmt.Sprintf("start local reconcile trigger=%s", trigger)); err != nil {
		return err
	}

	// Read the compose file from the worker host via ReadFileCommand.
	// This is this flow's equivalent of the git-based flows' git_fetch phase
	// (fetching the desired compose source), since local stacks have no git
	// repository to clone/fetch from.
	fetchStart := time.Now()
	var composeContent []byte
	var workDir, composeFile string

	cmdID := fmt.Sprintf("readfile-%s", stackID)
	result, dispatchErr := r.dispatcher.Dispatch(ctx, workerID, protocol.ReadFileCommand{
		CommandID: cmdID,
		Path:      importPath,
	})
	if dispatchErr != nil || result.Error != "" {
		errMsg := fmt.Sprintf("failed to read remote compose file %s: %v %s", importPath, dispatchErr, result.Error)
		r.logFailureWithPhase(stackID, trigger, "", errMsg, constants.PhaseGitFetch, fetchStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	data, err := base64.StdEncoding.DecodeString(result.Output)
	if err != nil {
		errMsg := fmt.Sprintf("failed to decode remote compose file: %v", err)
		r.logFailureWithPhase(stackID, trigger, "", errMsg, constants.PhaseGitFetch, fetchStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	composeContent = data

	// Store a local working copy in a temporary directory so the generated .env
	// used for interpolation never lands in persistent stack storage.
	workDir, err = os.MkdirTemp("", "wireops-local-stack-*")
	if err != nil {
		errMsg := fmt.Sprintf("failed to create temp work dir: %v", err)
		r.logFailureWithPhase(stackID, trigger, "", errMsg, constants.PhaseGitFetch, fetchStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(workDir); cleanupErr != nil {
			errMsg := fmt.Sprintf("failed to clean temp work dir for local stack %s (trigger=%s): %v", stackID, trigger, cleanupErr)
			log.Printf("[reconciler] %s", errMsg)
			_ = r.logFailure(stackID, trigger, "", errMsg)
			if retErr == nil {
				retErr = fmt.Errorf("%s", errMsg)
			}
		}
	}()

	sourceFile := filepath.Join(workDir, "source.yml")
	if writeErr := os.WriteFile(sourceFile, composeContent, 0644); writeErr != nil {
		errMsg := fmt.Sprintf("failed to write source file: %v", writeErr)
		r.logFailureWithPhase(stackID, trigger, "", errMsg, constants.PhaseGitFetch, fetchStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	composeFile = "source.yml"
	fetchDuration := time.Since(fetchStart).Milliseconds()

	// Change detection: compare SHA256 of raw file content with stored checksum.
	newChecksum := fmt.Sprintf("%x", sha256bytes(composeContent))
	currentChecksum := stack.GetString("checksum")
	neverSynced := stack.GetString("last_synced_at") == ""
	fileChanged := newChecksum != currentChecksum

	if trigger == "cron" && !neverSynced && !fileChanged {
		if err := r.saveRecordStatus(stack, "stacks", prevStatus, "restore status after unchanged local stack skip"); err != nil {
			return err
		}
		return nil
	}

	renderStart := time.Now()
	envVars, envErr := r.loadEnvVars(ctx, stackID)
	if envErr != nil {
		errMsg := fmt.Sprintf("failed to load env vars: %v", envErr)
		r.logFailureWithPhase(stackID, trigger, "", errMsg, constants.PhaseRender, renderStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	// Write .env to workDir so that compose config (called inside
	// GenerateRevision) can resolve ${VAR} interpolations.
	if envWriteErr := WriteEnvFile(workDir, envVars); envWriteErr != nil {
		log.Printf("[reconciler] warning: failed to write .env to work dir for stack %s (local sync): %v", stackID, envWriteErr)
	} else if gitignoreErr := EnsureGitignoreHasEnv(workDir); gitignoreErr != nil {
		log.Printf("[reconciler] warning: failed to ensure .gitignore for stack %s (local sync): %v", stackID, gitignoreErr)
	}

	renderRes, err := r.renderer.GenerateRevision(ctx, stack, nil, workDir, composeFile, envVars, "imported", false, workerID, workerFingerprint)
	if err != nil {
		errMsg := fmt.Sprintf("failed to generate label revision: %v", err)
		r.logFailureWithPhase(stackID, trigger, "", errMsg, constants.PhaseRender, renderStart)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	renderDuration := time.Since(renderStart).Milliseconds()

	syncLog, err := r.createSyncLog(stackID, trigger, "imported", "local stack sync")
	if err != nil {
		return fmt.Errorf("failed to create sync log: %w", err)
	}

	pt := newPhaseTracker(r.app, syncLog.Id)
	defer pt.finishCurrentAsError("local sync aborted")
	_ = pt.recordCompleted(constants.PhaseGitFetch, constants.PhaseStatusSuccess, fetchStart, fetchDuration, "")
	_ = pt.recordSkipped(constants.PhasePolicyCheck, "n/a: local stack sync")
	_ = pt.recordCompleted(constants.PhaseRender, constants.PhaseStatusSuccess, renderStart, renderDuration, fmt.Sprintf("checksum=%s version=%d", renderRes.Checksum, renderRes.Version))

	renderedFilePath := r.renderer.GetRevisionFilePath(stackID, renderRes.Version)
	recreateContainers := neverSynced
	recreateVolumes := false
	if neverSynced {
		recreateVolumes = stack.GetBool("import_recreate_volumes")
	}

	var output string
	var runErr error
	var composeUpMs int64
	start := time.Now()
	_ = pt.start(constants.PhaseDispatch)

	composeBytes, err := r.readRenderedCompose(stack, stackID, trigger, "", renderedFilePath)
	if err != nil {
		return err
	}
	b64 := base64.StdEncoding.EncodeToString(composeBytes)

	envFileB64, b64Err := buildEnvFileB64(envVars)
	if b64Err != nil {
		runErr = fmt.Errorf("failed to serialize env vars for remote local-sync: %w", b64Err)
	} else if recreateContainers {
		forcePull, removeOrphans := resolveComposeRuntimeFlags(stack)
		dispatchCtx, cancelDispatch := withDeployTimeout(ctx, stack)
		defer cancelDispatch()
		result, dispatchErr := r.dispatcher.Dispatch(dispatchCtx, workerID, protocol.RedeployCommand{
			DeployCommand: protocol.DeployCommand{
				CommandID:      syncLog.Id,
				StackID:        stackID,
				CommitSHA:      "imported",
				Trigger:        trigger,
				ComposeFileB64: b64,
				EnvFileB64:     envFileB64,
				ForcePull:      forcePull,
				RemoveOrphans:  removeOrphans,
			},
			RecreateContainers: true,
			RecreateVolumes:    recreateVolumes,
		})
		composeUpMs = result.ComposeUpMs
		output, runErr = extractDispatchResult(result, dispatchErr)
	} else {
		forcePull, removeOrphans := resolveComposeRuntimeFlags(stack)
		dispatchCtx, cancelDispatch := withDeployTimeout(ctx, stack)
		defer cancelDispatch()
		result, dispatchErr := r.dispatcher.Dispatch(dispatchCtx, workerID, protocol.DeployCommand{
			CommandID:      syncLog.Id,
			StackID:        stackID,
			CommitSHA:      "imported",
			Trigger:        trigger,
			ComposeFileB64: b64,
			EnvFileB64:     envFileB64,
			ForcePull:      forcePull,
			RemoveOrphans:  removeOrphans,
		})
		composeUpMs = result.ComposeUpMs
		output, runErr = extractDispatchResult(result, dispatchErr)
	}

	duration := time.Since(start).Milliseconds()

	if runErr != nil {
		_ = pt.finish(constants.PhaseDispatch, constants.PhaseStatusError, runErr.Error())
		errOutput := buildErrorOutput(output, runErr)
		if err := r.updateSyncLog(syncLog.Id, "error", errOutput, duration); err != nil {
			_ = r.markError(stack, "stacks")
			return err
		}
		if err := r.markError(stack, "stacks"); err != nil {
			return err
		}
		return runErr
	}
	_ = pt.finish(constants.PhaseDispatch, constants.PhaseStatusSuccess, "")
	r.recordWorkerAckAndComposeUpPhases(pt, syncLog.Id, start, composeUpMs)

	_ = pt.start(constants.PhasePostCheck)
	check := r.postDeployCheck(ctx, workerID, stackID, workDir, composeBytes)
	output = output + "\n\n[post-check] " + check.Detail
	postCheckStatus := constants.PhaseStatusSuccess
	if check.Status == "error" || check.Status == "degraded" {
		postCheckStatus = constants.PhaseStatusError
	}
	_ = pt.finish(constants.PhasePostCheck, postCheckStatus, check.Detail)

	// Update the stack's raw-file checksum after a successful deploy.
	stack.Set("checksum", newChecksum)
	stack.Set("last_synced_at", time.Now().UTC().Format(time.RFC3339))
	stack.Set("status", check.Status)
	stack.Set("deployed_version", renderRes.Version)
	stack.Set("deployed_commit", "imported")
	stack.Set("deployed_checksum", newChecksum)
	stack.Set("deployed_at", time.Now().UTC().Format(time.RFC3339))
	if err := r.saveRecord(stack, "stacks", "complete local reconcile"); err != nil {
		_ = r.updateSyncLog(syncLog.Id, "error", "local deploy succeeded but failed to persist stack success: "+err.Error(), duration)
		return err
	}
	logStatus := "success"
	if check.Status != "active" {
		logStatus = check.Status
	}
	if err := r.updateSyncLog(syncLog.Id, logStatus, output, duration); err != nil {
		_ = r.markError(stack, "stacks")
		return err
	}
	// reconcileLocalStack has no notifier calls (local stacks aren't wired
	// to the notify pipeline yet) — record the phase as skipped so the
	// timeline's 8-phase shape stays consistent across every flow.
	_ = pt.recordSkipped(constants.PhaseNotify, "n/a: local stack sync has no notifications")

	return nil
}

func sha256bytes(data []byte) []byte {
	h := sha256.New()
	h.Write(data)
	return h.Sum(nil)
}

func (r *Reconciler) queuePendingReconcile(stackID, trigger, commitSHA string) error {
	col, err := r.app.FindCollectionByNameOrId("stack_pending_reconciles")
	if err != nil {
		return fmt.Errorf("queue pending reconcile stack=%s trigger=%s: %w", stackID, trigger, err)
	}

	// Delete any existing pending reconcile for this stack to avoid duplicates
	existing, err := r.app.FindAllRecords("stack_pending_reconciles", dbx.HashExp{"stack": stackID})
	if err != nil {
		return fmt.Errorf("queue pending reconcile stack=%s trigger=%s list existing: %w", stackID, trigger, err)
	}
	for _, rec := range existing {
		if err := r.app.Delete(rec); err != nil {
			return fmt.Errorf("queue pending reconcile stack=%s trigger=%s delete existing=%s: %w", stackID, trigger, rec.Id, err)
		}
	}

	record := core.NewRecord(col)
	record.Set("stack", stackID)
	record.Set("trigger", trigger)
	record.Set("commit_sha", commitSHA)

	if err := r.app.Save(record); err != nil {
		return fmt.Errorf("queue pending reconcile stack=%s trigger=%s save: %w", stackID, trigger, err)
	}

	queueLog, err := r.createSyncLog(stackID, "queue", commitSHA, "Added to offline queue (original trigger: "+trigger+")")
	if err != nil {
		return err
	}
	if err := r.updateSyncLog(queueLog.Id, "queued", "Worker is offline. Sync will proceed when worker reconnects.", 0); err != nil {
		return err
	}
	return nil
}

func (r *Reconciler) inspectStackCommit(ctx context.Context, workerID, stackID string) string {
	result, err := r.dispatcher.Dispatch(ctx, workerID, protocol.InspectCommand{
		CommandID: "inspect-" + stackID + "-" + fmt.Sprint(time.Now().UnixNano()),
		StackID:   stackID,
	})
	if err != nil {
		log.Printf("[reconciler] failed to dispatch inspect command for stack %s: %v", stackID, err)
		return ""
	}
	if result.Error != "" {
		log.Printf("[reconciler] inspect command returned error for stack %s: %s", stackID, result.Error)
		return ""
	}

	var inspectRes protocol.InspectResult
	if err := json.Unmarshal([]byte(result.Output), &inspectRes); err != nil {
		log.Printf("[reconciler] failed to unmarshal inspect result for stack %s: %v", stackID, err)
		return ""
	}

	return inspectRes.CommitSHA
}

func (r *Reconciler) reposWorkspace() string {
	return config.GetReposWorkspace()
}

func (r *Reconciler) resolveGitAuth(repoID string) (transport.AuthMethod, error) {
	cred, err := r.loadCredential(repoID)
	if err != nil {
		return nil, err
	}
	return gitpkg.ResolveTransportAuth(*cred)
}

func (r *Reconciler) loadCredential(repoID string) (*gitpkg.Credential, error) {
	return gitpkg.LoadRepositoryCredential(r.app, repoID)
}

func (r *Reconciler) cloneOrFetchWithRetry(ctx context.Context, repoID, gitURL, branch string, auth transport.AuthMethod, workspace string) (*gogit.Repository, error) {
	const maxAttempts = 3

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		repo, err := gitpkg.CloneOrFetchContext(ctx, repoID, gitURL, branch, auth, workspace)
		if err == nil {
			if attempt > 1 {
				log.Printf("[reconciler] git operation recovered for repo %s on attempt %d", repoID, attempt)
			}
			return repo, nil
		}
		lastErr = err
		if attempt == maxAttempts || !isTransientGitError(err) {
			break
		}

		delay := time.Duration(attempt*attempt) * time.Second
		log.Printf("[reconciler] transient git operation failure for repo %s on attempt %d/%d: %v; retrying in %s", repoID, attempt, maxAttempts, err, delay)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	return nil, lastErr
}

func isTransientGitError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	msg := strings.ToLower(err.Error())
	transientMarkers := []string{
		"connection reset",
		"connection timed out",
		"context deadline exceeded",
		"deadline exceeded",
		"handshake failed",
		"i/o timeout",
		"network is unreachable",
		"no route to host",
		"temporary",
		"timeout",
		"timed out",
		"unexpected packet",
		"eof",
	}
	for _, marker := range transientMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func (r *Reconciler) loadEnvVars(ctx context.Context, stackID string) ([]string, error) {
	return envvars.LoadStack(ctx, r.app, r.secretsRegistry, stackID)
}

// loadSopsEnv looks for a secrets.yaml/secrets.yml next to the stack's
// wireops.yaml (workDir) and, if found, decrypts it with the repository's
// age key. Returns (nil, nil) when no secrets file is present — the common
// case, and a no-op for stacks not using SOPS. repo may be nil (e.g. local
// stacks, which have no repository record and no wireops.yaml) — SOPS is a
// GitOps-only feature, so that also returns (nil, nil).
func (r *Reconciler) loadSopsEnv(ctx context.Context, repo *core.Record, workDir string) (map[string]string, error) {
	if repo == nil {
		return nil, nil
	}

	path, err := secrets.FindSecretsFile(workDir)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil
	}

	encryptedKey := repo.GetString("sops_age_key")
	if encryptedKey == "" {
		return nil, fmt.Errorf("found %q but repository %q has no SOPS age key configured", filepath.Base(path), repo.GetString("name"))
	}

	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	ageKey, err := crypto.Decrypt(encryptedKey, secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt repository SOPS age key: %w", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %q: %w", path, err)
	}

	return secrets.DecryptSecretsFile(ctx, content, string(ageKey))
}

// overlaySopsEnv merges SOPS-decrypted values on top of envVars ("KEY=VALUE"
// list from global/stack env vars), with SOPS values taking precedence on
// key collisions. Returns envVars unchanged when overlay is empty.
func overlaySopsEnv(envVars []string, overlay map[string]string) []string {
	if len(overlay) == 0 {
		return envVars
	}

	values := make(map[string]string, len(envVars)+len(overlay))
	for _, kv := range envVars {
		if idx := strings.Index(kv, "="); idx >= 0 {
			values[kv[:idx]] = kv[idx+1:]
		}
	}
	for k, v := range overlay {
		values[k] = v
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	merged := make([]string, 0, len(keys))
	for _, k := range keys {
		merged = append(merged, k+"="+values[k])
	}
	return merged
}

// buildEnvFileB64 renders envVars as a .env file using the canonical
// serializeEnvContent serializer (same quoting and validation as WriteEnvFile)
// and returns the base64-encoded result. If envVars is empty, returns ("", nil)
// which signals the worker to remove the .env file.
func buildEnvFileB64(envVars []string) (string, error) {
	if len(envVars) == 0 {
		return "", nil
	}
	content, err := serializeEnvContent(envVars)
	if err != nil {
		return "", fmt.Errorf("buildEnvFileB64: %w", err)
	}
	return base64.StdEncoding.EncodeToString([]byte(content)), nil
}

func (r *Reconciler) createSyncLog(stackID, trigger, commitSHA, commitMsg string) (*core.Record, error) {
	collection, err := r.app.FindCollectionByNameOrId("sync_logs")
	if err != nil {
		return nil, fmt.Errorf("create sync log stack=%s trigger=%s: %w", stackID, trigger, err)
	}
	record := core.NewRecord(collection)
	record.Set("stack", stackID)
	record.Set("trigger", trigger)
	record.Set("status", "running")
	record.Set("commit_sha", commitSHA)
	record.Set("commit_message", commitMsg)
	if err := r.app.Save(record); err != nil {
		return nil, fmt.Errorf("create sync log stack=%s trigger=%s status=running: %w", stackID, trigger, err)
	}
	return record, nil
}

func (r *Reconciler) updateSyncLog(id, status, output string, durationMs int64) error {
	record, err := r.app.FindRecordById("sync_logs", id)
	if err != nil {
		return fmt.Errorf("update sync log id=%s status=%s: %w", id, status, err)
	}
	record.Set("status", status)

	// Truncate output to prevent database bloat
	const maxOutputLength = 1000000
	if len(output) > maxOutputLength {
		marker := "\n\n... [OUTPUT TRUNCATED FOR SIZE] ...\n\n"
		prefixLen := (maxOutputLength - len(marker)) / 2
		suffixLen := maxOutputLength - len(marker) - prefixLen
		output = output[:prefixLen] + marker + output[len(output)-suffixLen:]
	}

	record.Set("output", output)
	record.Set("duration_ms", durationMs)
	if err := r.app.Save(record); err != nil {
		return fmt.Errorf("update sync log id=%s status=%s: %w", id, status, err)
	}
	return nil
}

func (r *Reconciler) logNoopSync(ctx context.Context, stack *core.Record, stackID, trigger, commitSHA, commitMsg, output string) error {
	syncLog, err := r.createSyncLog(stackID, trigger, commitSHA, commitMsg)
	if err != nil {
		return fmt.Errorf("failed to create no-op sync log: %w", err)
	}
	if r.notifier != nil {
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:     notify.SyncStarted,
			StackID:   stackID,
			StackName: stack.GetString("name"),
			SyncLogID: syncLog.Id,
			Trigger:   trigger,
			CommitSHA: commitSHA,
		})
	}
	if err := r.updateSyncLog(syncLog.Id, "noop", output, 0); err != nil {
		return err
	}
	if r.notifier != nil {
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:     notify.SyncDone,
			StackID:   stackID,
			StackName: stack.GetString("name"),
			SyncLogID: syncLog.Id,
			Trigger:   trigger,
			CommitSHA: commitSHA,
		})
	}
	return nil
}

// logNoopSyncWithPhases behaves like logNoopSync but also backfills the
// git_fetch/render/policy_check phases that already ran before the noop
// decision was made, so a noop deploy's timeline is complete instead of
// empty. If reused is non-nil (waitForRunningJobs already created a sync log
// for this attempt while waiting on jobs), that row is reused instead of
// creating a second one.
func (r *Reconciler) logNoopSyncWithPhases(
	ctx context.Context, stack *core.Record, stackID, trigger, commitSHA, commitMsg, output string,
	reused *core.Record,
	gitFetchStart time.Time, gitFetchDuration int64,
	renderStart time.Time, renderDuration int64,
) error {
	syncLog := reused
	if syncLog == nil {
		rec, err := r.createSyncLog(stackID, trigger, commitSHA, commitMsg)
		if err != nil {
			return fmt.Errorf("failed to create no-op sync log: %w", err)
		}
		syncLog = rec
	}

	pt := newPhaseTracker(r.app, syncLog.Id)
	_ = pt.recordCompleted(constants.PhaseGitFetch, constants.PhaseStatusSuccess, gitFetchStart, gitFetchDuration, "")
	if reused == nil {
		_ = pt.recordSkipped(constants.PhasePolicyCheck, "no wait needed")
	}
	_ = pt.recordCompleted(constants.PhaseRender, constants.PhaseStatusSkipped, renderStart, renderDuration, "compose unchanged, deploy skipped")

	if reused == nil && r.notifier != nil {
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:     notify.SyncStarted,
			StackID:   stackID,
			StackName: stack.GetString("name"),
			SyncLogID: syncLog.Id,
			Trigger:   trigger,
			CommitSHA: commitSHA,
		})
	}
	if err := r.updateSyncLog(syncLog.Id, "noop", output, 0); err != nil {
		return err
	}
	if r.notifier != nil {
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:     notify.SyncDone,
			StackID:   stackID,
			StackName: stack.GetString("name"),
			SyncLogID: syncLog.Id,
			Trigger:   trigger,
			CommitSHA: commitSHA,
		})
	}
	return nil
}

func (r *Reconciler) saveRecord(rec *core.Record, collection, op string) error {
	if err := r.app.Save(rec); err != nil {
		return fmt.Errorf("%s persistence failed collection=%s record=%s status=%s: %w", op, collection, rec.Id, rec.GetString("status"), err)
	}
	return nil
}

func (r *Reconciler) saveRecordStatus(rec *core.Record, collection, status, op string) error {
	rec.Set("status", status)
	return r.saveRecord(rec, collection, op)
}

func (r *Reconciler) markError(rec *core.Record, collection string) error {
	rec.Set("status", "error")
	if err := r.saveRecord(rec, collection, "mark error"); err != nil {
		log.Printf("[reconciler] failed to mark error collection=%s record=%s: %v", collection, rec.Id, err)
		return err
	}
	log.Printf("[reconciler] %s/%s status=error", collection, rec.Id)
	return nil
}

// logFailure creates a sync log entry for early failures (before the normal sync log is created).
func (r *Reconciler) logFailure(stackID, trigger, commitSHA, errMsg string) error {
	log.Printf("[reconciler] stack %s failure: %s", stackID, errMsg)
	syncLog, err := r.createSyncLog(stackID, trigger, commitSHA, "")
	if err != nil {
		log.Printf("[reconciler] failed to create failure sync log: %v", err)
		return err
	}
	if err := r.updateSyncLog(syncLog.Id, "error", errMsg, 0); err != nil {
		log.Printf("[reconciler] failed to persist failure sync log stack=%s trigger=%s: %v", stackID, trigger, err)
		return err
	}
	return nil
}

// logFailureWithPhase behaves like logFailure but also records which
// pipeline phase the failure occurred in (git_fetch or render, for the
// failures that happen before the normal deploy sync log exists), so the
// deploy timeline has a concrete failing step for pre-dispatch failures too.
func (r *Reconciler) logFailureWithPhase(stackID, trigger, commitSHA, errMsg, phase string, phaseStart time.Time) error {
	log.Printf("[reconciler] stack %s failure: %s", stackID, errMsg)
	syncLog, err := r.createSyncLog(stackID, trigger, commitSHA, "")
	if err != nil {
		log.Printf("[reconciler] failed to create failure sync log: %v", err)
		return err
	}
	pt := newPhaseTracker(r.app, syncLog.Id)
	if perr := pt.recordCompleted(phase, "error", phaseStart, time.Since(phaseStart).Milliseconds(), errMsg); perr != nil {
		log.Printf("[reconciler] failed to record failing phase %s for stack %s: %v", phase, stackID, perr)
	}
	if err := r.updateSyncLog(syncLog.Id, "error", errMsg, 0); err != nil {
		log.Printf("[reconciler] failed to persist failure sync log stack=%s trigger=%s: %v", stackID, trigger, err)
		return err
	}
	return nil
}

// checkSecretBackends is a fast pre-flight gate: it rejects the deploy
// immediately if any of the stack's secret env vars reference a
// vault/infisical backend that is currently disabled or unconfigured,
// before any git fetch/render work happens. Tagged as PhasePolicyCheck —
// otherwise unused in most of these flows — so it shows up as a clean
// "Policy Check" failure in the deploy timeline instead of a deep,
// confusing PhaseRender error from env var resolution.
func (r *Reconciler) checkSecretBackends(stackID, trigger, commitSHA string, stack *core.Record) error {
	if err := envvars.CheckStackSecretBackends(r.app, stackID); err != nil {
		errMsg := fmt.Sprintf("cannot deploy: %v", err)
		_ = r.logFailureWithPhase(stackID, trigger, commitSHA, errMsg, constants.PhasePolicyCheck, time.Now())
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	return nil
}

// recordWorkerAckAndComposeUpPhases backfills the worker_ack and compose_up
// phases for a deploy/redeploy command once it has completed successfully.
// worker_ack duration is reconstructed from worker_commands.acked_at (set by
// internal/worker.LogCommandAck when the worker acknowledges receipt) minus
// dispatchStart, since Dispatch() itself only blocks until the final result
// and doesn't expose the intermediate ack to its caller. compose_up duration
// comes directly from the worker's own report (protocol.CommandResult.ComposeUpMs),
// populated by worker/executor around the actual `docker compose up` call.
// Either phase is recorded as skipped when the underlying data isn't
// available (older worker, ack raced with the result, non-durable dispatch).
func (r *Reconciler) recordWorkerAckAndComposeUpPhases(pt *phaseTracker, commandID string, dispatchStart time.Time, composeUpMs int64) {
	ackedAt := time.Time{}
	if commandID != "" {
		if records, err := r.app.FindAllRecords("worker_commands", dbx.HashExp{"command_id": commandID}); err == nil && len(records) > 0 {
			if t := records[0].GetDateTime("acked_at").Time(); !t.IsZero() {
				ackedAt = t
			}
		}
	}

	if ackedAt.IsZero() {
		_ = pt.recordSkipped(constants.PhaseWorkerAck, "ack timestamp not observed")
	} else {
		ackMs := ackedAt.Sub(dispatchStart).Milliseconds()
		if ackMs < 0 {
			ackMs = 0
		}
		_ = pt.recordCompleted(constants.PhaseWorkerAck, constants.PhaseStatusSuccess, dispatchStart, ackMs, "")
	}

	if composeUpMs <= 0 {
		_ = pt.recordSkipped(constants.PhaseComposeUp, "not reported by worker")
		return
	}
	composeUpStart := dispatchStart
	if !ackedAt.IsZero() {
		composeUpStart = ackedAt
	}
	_ = pt.recordCompleted(constants.PhaseComposeUp, constants.PhaseStatusSuccess, composeUpStart, composeUpMs, "")
}

func buildErrorOutput(output string, runErr error) string {
	var b strings.Builder
	if output != "" {
		b.WriteString(output)
		if output[len(output)-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	if runErr != nil {
		fmt.Fprintf(&b, "\nerror: %v", runErr)
	}
	return b.String()
}

// extractDispatchResult unpacks a dispatcher response into (output, error).
// dispatchErr takes precedence over a non-empty result.Error field.
func extractDispatchResult(result protocol.CommandResult, dispatchErr error) (string, error) {
	var runErr error
	if result.Error != "" {
		runErr = fmt.Errorf("%s", result.Error)
	}
	if dispatchErr != nil {
		runErr = dispatchErr
	}
	return result.Output, runErr
}

// readRenderedCompose reads the rendered compose file at path. On failure it logs
// the error, marks the stack as error, and returns a non-nil error.
func (r *Reconciler) readRenderedCompose(stack *core.Record, stackID, trigger, sha, path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		errMsg := fmt.Sprintf("failed to read rendered compose file: %v", err)
		r.logFailure(stackID, trigger, sha, errMsg)
		r.markError(stack, "stacks")
		return nil, fmt.Errorf("%s", errMsg)
	}
	return content, nil
}

// resolveComposeFile returns the validated compose filename for a stack, applying
// the default name, checking path safety, and verifying the file exists.
// On any failure it logs the error, marks the stack as error, and returns a non-nil error.
func (r *Reconciler) resolveComposeFile(stack *core.Record, workDir, stackID, trigger, sha string) (string, error) {
	composeFile := stack.GetString("compose_file")
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}
	if err := safepath.ValidateComposeFile(composeFile); err != nil {
		errMsg := fmt.Sprintf("invalid compose_file: %v", err)
		r.logFailure(stackID, trigger, sha, errMsg)
		r.markError(stack, "stacks")
		return "", fmt.Errorf("%s", errMsg)
	}
	if _, statErr := os.Stat(filepath.Join(workDir, composeFile)); os.IsNotExist(statErr) {
		errMsg := fmt.Sprintf("compose file not found: %s (workdir: %s)", composeFile, workDir)
		r.logFailure(stackID, trigger, sha, errMsg)
		r.markError(stack, "stacks")
		return "", fmt.Errorf("%s", errMsg)
	}
	return composeFile, nil
}

// TransferStack provisions the stack on targetWorkerID, then tears it down on the
// original worker, and updates the stack record to point to the new worker.
// Data (volumes, container state) is NOT preserved — this is by design for v1.
func (r *Reconciler) TransferStack(ctx context.Context, stackID, targetWorkerID string) error {
	mu := r.stackMutex(stackID)
	if !mu.TryLock() {
		log.Printf("[transfer] stack=%s skipped: already syncing", stackID)
		return fmt.Errorf("stack %s already syncing", stackID)
	}
	defer mu.Unlock()

	stack, err := r.app.FindRecordById("stacks", stackID)
	if err != nil {
		return fmt.Errorf("stack not found: %w", err)
	}

	if err := r.checkSecretBackends(stackID, "transfer", "", stack); err != nil {
		return err
	}

	sourceWorkerID := stack.GetString("worker")
	if sourceWorkerID == "" {
		return fmt.Errorf("stack has no worker assigned")
	}
	if sourceWorkerID == targetWorkerID {
		return fmt.Errorf("target worker is the same as the current worker")
	}

	log.Printf("[transfer] START stack=%s source_worker=%s target_worker=%s", stackID, sourceWorkerID, targetWorkerID)

	// Read the current rendered compose file for both deploy and teardown.
	var composeContent []byte
	var composeFilePath string
	currentVersion := stack.GetInt("current_version")
	if currentVersion > 0 {
		composeFilePath = r.renderer.GetRevisionFilePath(stackID, currentVersion)
		composeContent, err = os.ReadFile(composeFilePath)
		if err != nil {
			return fmt.Errorf("failed to read rendered compose file: %w", err)
		}
	}
	if len(composeContent) == 0 || composeFilePath == "" {
		return fmt.Errorf("stack has no rendered compose file — sync the stack at least once before transferring")
	}

	envVars, envErr := r.loadEnvVars(ctx, stackID)
	if envErr != nil {
		return fmt.Errorf("failed to load env vars: %w", envErr)
	}

	if repoID := stack.GetString("repository"); repoID != "" {
		repo, repoErr := r.app.FindRecordById("repositories", repoID)
		if repoErr != nil {
			return fmt.Errorf("failed to load repository for SOPS secrets: %w", repoErr)
		}
		workDir, workDirErr := r.stackWorkDir(stack, repoID)
		if workDirErr != nil {
			return fmt.Errorf("failed to resolve work dir for SOPS secrets: %w", workDirErr)
		}
		sopsValues, sopsErr := r.loadSopsEnv(ctx, repo, workDir)
		if sopsErr != nil {
			return fmt.Errorf("failed to decrypt SOPS secrets file: %w", sopsErr)
		}
		envVars = overlaySopsEnv(envVars, sopsValues)
	}

	composeB64 := base64.StdEncoding.EncodeToString(composeContent)

	envFileB64, b64Err := buildEnvFileB64(envVars)
	if b64Err != nil {
		return fmt.Errorf("failed to serialize env vars for transfer: %w", b64Err)
	}

	// Resolve worker hostnames and fingerprints for human-friendly sync log messages.
	sourceHostname := sourceWorkerID
	if a, err := r.app.FindRecordById("workers", sourceWorkerID); err != nil {
		return fmt.Errorf("failed to find source worker %s: %w", sourceWorkerID, err)
	} else {
		sourceHostname = a.GetString("hostname")
	}

	var targetHostname string
	if a, err := r.app.FindRecordById("workers", targetWorkerID); err != nil {
		return fmt.Errorf("failed to find target worker %s: %w", targetWorkerID, err)
	} else {
		targetHostname = a.GetString("hostname")
	}

	prevStatus := stack.GetString("status")

	// Mark stack as syncing during the transfer
	if err := r.saveRecordStatus(stack, "stacks", "syncing", "start transfer"); err != nil {
		return err
	}

	syncLog, err := r.createSyncLog(stackID, "transfer", "",
		fmt.Sprintf("%s → %s", sourceHostname, targetHostname))
	if err != nil {
		_ = r.markError(stack, "stacks")
		return err
	}

	syncLogID := syncLog.Id

	pt := newPhaseTracker(r.app, syncLog.Id)
	defer pt.finishCurrentAsError("transfer aborted")
	// A transfer reuses the stack's already-rendered compose file — there's
	// no git fetch, render, or wait_running_jobs step in this flow.
	_ = pt.recordSkipped(constants.PhaseGitFetch, "n/a: transfer reuses last rendered compose")
	_ = pt.recordSkipped(constants.PhaseRender, "n/a: transfer reuses last rendered compose")
	_ = pt.recordSkipped(constants.PhasePolicyCheck, "n/a: transfer")
	_ = pt.start(constants.PhaseDispatch)

	r.notifier.Dispatch(ctx, notify.Payload{
		Event:     notify.SyncStarted,
		StackID:   stackID,
		StackName: stack.GetString("name"),
		SyncLogID: syncLogID,
		Trigger:   "transfer",
	})

	start := time.Now()
	var outputBuf strings.Builder

	// --- Pre-flight 1: check if target agent already has a stack with this name ---
	stackName := stack.GetString("name")
	existingStacks, err := r.app.FindAllRecords("stacks", dbx.HashExp{"name": stackName, "worker": targetWorkerID})
	if err == nil && len(existingStacks) > 0 {
		errMsg := fmt.Sprintf("a stack named '%s' already exists on target agent %s", stackName, targetHostname)
		log.Printf("[transfer] validation error: %s", errMsg)
		outputBuf.WriteString("error: " + errMsg + "\n")

		if err := r.updateSyncLog(syncLog.Id, "error", outputBuf.String(), time.Since(start).Milliseconds()); err != nil {
			_ = r.markError(stack, "stacks")
			return err
		}
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:      notify.SyncError,
			StackID:    stackID,
			StackName:  stackName,
			SyncLogID:  syncLogID,
			Trigger:    "transfer",
			DurationMs: time.Since(start).Milliseconds(),
			Error:      errMsg,
		})
		if err := r.saveRecordStatus(stack, "stacks", prevStatus, "restore status after transfer validation failure"); err != nil {
			return err
		}
		return fmt.Errorf("transfer failed: %s", errMsg)
	}

	// --- Pre-flight 2: probe agent B to detect existing containers ---
	// If containers (any state) already exist for this project on the target host,
	// we abort early to avoid conflicting volumes, networks, or port bindings.
	var probeErrMsg string
	probeID := fmt.Sprintf("probe-%s", stackID)
	log.Printf("[transfer] probe: dispatching to target_agent=%s stack=%s", targetWorkerID, stackID)
	probeResult, probeErr := r.dispatcher.Dispatch(ctx, targetWorkerID, protocol.ProbeCommand{
		CommandID:      probeID,
		StackID:        stackID,
		ComposeFileB64: composeB64,
		EnvFileB64:     envFileB64,
	})
	if probeErr == nil && probeResult.Error == "" && probeResult.Output != "" {
		var probe protocol.ProbeResult
		if jsonErr := json.Unmarshal([]byte(probeResult.Output), &probe); jsonErr == nil {
			log.Printf("[transfer] probe: target_agent=%s containers=%d services=%v", targetWorkerID, probe.ContainerCount, probe.Services)
			if probe.ContainerCount > 0 {
				probeErrMsg = fmt.Sprintf(
					"target agent %s already has %d container(s) for this stack (services: %s) — "+
						"remove them manually before transferring",
					targetHostname, probe.ContainerCount, strings.Join(probe.Services, ", "),
				)
			}
		}
	}
	if probeErr != nil {
		log.Printf("[transfer] probe error target_agent=%s (non-blocking): %v", targetWorkerID, probeErr)
	}

	if probeErrMsg != "" {
		log.Printf("[transfer] validation error: %s", probeErrMsg)
		outputBuf.WriteString("error: " + probeErrMsg + "\n")

		if err := r.updateSyncLog(syncLog.Id, "error", outputBuf.String(), time.Since(start).Milliseconds()); err != nil {
			_ = r.markError(stack, "stacks")
			return err
		}
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:      notify.SyncError,
			StackID:    stackID,
			StackName:  stackName,
			SyncLogID:  syncLogID,
			Trigger:    "transfer",
			DurationMs: time.Since(start).Milliseconds(),
			Error:      probeErrMsg,
		})
		if err := r.saveRecordStatus(stack, "stacks", prevStatus, "restore status after transfer probe failure"); err != nil {
			return err
		}
		return fmt.Errorf("transfer failed: %s", probeErrMsg)
	}
	fmt.Fprintf(&outputBuf, "=== Step 1/2: Deploy on target agent (%s) ===\n", targetHostname)

	// --- Step 1: Deploy on target agent (agent B) ---
	cmdID := ""
	cmdID = syncLog.Id

	var deployOutput string
	var deployErr error
	var dispatchErr error

	log.Printf("[transfer] step 1/2: deploy dispatching to target_agent=%s (%s) stack=%s", targetWorkerID, targetHostname, stackID)
	transferForcePull, transferRemoveOrphans := resolveComposeRuntimeFlags(stack)
	transferDispatchCtx, cancelTransferDispatch := withDeployTimeout(ctx, stack)
	defer cancelTransferDispatch()
	deployDispatchStart := time.Now()
	deployResult, dErr := r.dispatcher.Dispatch(transferDispatchCtx, targetWorkerID, protocol.DeployCommand{
		CommandID:      cmdID,
		StackID:        stackID,
		Trigger:        "transfer",
		ComposeFileB64: composeB64,
		EnvFileB64:     envFileB64,
		ForcePull:      transferForcePull,
		RemoveOrphans:  transferRemoveOrphans,
	})
	deployOutput = deployResult.Output
	dispatchErr = dErr
	if deployResult.Error != "" {
		deployErr = fmt.Errorf("%s", deployResult.Error)
	}

	if dispatchErr != nil || deployErr != nil {
		deployErrMsg := fmt.Sprintf("%v%v", dispatchErr, deployErr)
		log.Printf("[transfer] step 1/2: deploy error target_agent=%s elapsed=%dms: %s", targetWorkerID, time.Since(start).Milliseconds(), deployErrMsg)
		outputBuf.WriteString(deployOutput)
		fmt.Fprintf(&outputBuf, "\nerror: %s\n", deployErrMsg)
		fmt.Fprintf(&outputBuf, "\n=== Step 2/2: Cleanup on target agent (%s) ===\n", targetHostname)

		// Best-effort cleanup on agent B — remove any partial containers it may have started.
		if r.dispatcher != nil && r.dispatcher.IsConnected(targetWorkerID) {
			log.Printf("[transfer] step 2/2: cleanup dispatching to target_agent=%s stack=%s", targetWorkerID, stackID)
			cleanupResult, cleanupErr := r.dispatcher.Dispatch(ctx, targetWorkerID, protocol.TeardownCommand{
				CommandID:      fmt.Sprintf("teardown-cleanup-%s", stackID),
				StackID:        stackID,
				ComposeFileB64: composeB64,
				EnvFileB64:     envFileB64,
			})
			if cleanupErr != nil || cleanupResult.Error != "" {
				log.Printf("[transfer] step 2/2: cleanup error target_agent=%s: %v %s", targetWorkerID, cleanupErr, cleanupResult.Error)
				fmt.Fprintf(&outputBuf, "cleanup teardown failed: %v %s\n", cleanupErr, cleanupResult.Error)
			} else {
				log.Printf("[transfer] step 2/2: cleanup done target_agent=%s", targetWorkerID)
				outputBuf.WriteString(cleanupResult.Output)
				fmt.Fprintf(&outputBuf, "cleanup teardown succeeded.\n")
			}
		} else {
			log.Printf("[transfer] step 2/2: cleanup skipped — target agent offline")
			outputBuf.WriteString("target agent offline — skipping cleanup.\n")
		}

		if err := r.updateSyncLog(syncLog.Id, "error", outputBuf.String(), time.Since(start).Milliseconds()); err != nil {
			_ = r.markError(stack, "stacks")
			return err
		}
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:      notify.SyncError,
			StackID:    stackID,
			StackName:  stack.GetString("name"),
			SyncLogID:  syncLogID,
			Trigger:    "transfer",
			DurationMs: time.Since(start).Milliseconds(),
			Error:      deployErrMsg,
		})
		if err := r.markError(stack, "stacks"); err != nil {
			return err
		}
		return fmt.Errorf("transfer failed: %s", deployErrMsg)
	}

	_ = pt.finish(constants.PhaseDispatch, constants.PhaseStatusSuccess, "")
	r.recordWorkerAckAndComposeUpPhases(pt, cmdID, deployDispatchStart, deployResult.ComposeUpMs)
	outputBuf.WriteString(deployOutput)
	fmt.Fprintf(&outputBuf, "deploy on %s: done.\n", targetHostname)
	log.Printf("[transfer] step 1/2: deploy done target_agent=%s elapsed=%dms", targetWorkerID, time.Since(start).Milliseconds())

	// --- Step 2: Teardown on source agent (agent A) ---
	fmt.Fprintf(&outputBuf, "\n=== Step 2/2: Teardown on source agent (%s) ===\n", sourceHostname)
	if sourceWorkerID != "" && r.dispatcher != nil && r.dispatcher.IsConnected(sourceWorkerID) {
		log.Printf("[transfer] step 2/2: teardown dispatching to source_agent=%s (%s) stack=%s", sourceWorkerID, sourceHostname, stackID)
		teardownResult, teardownErr := r.dispatcher.Dispatch(ctx, sourceWorkerID, protocol.TeardownCommand{
			CommandID:      fmt.Sprintf("teardown-transfer-%s", stackID),
			StackID:        stackID,
			ComposeFileB64: composeB64,
			EnvFileB64:     envFileB64,
		})
		outputBuf.WriteString(teardownResult.Output)
		if teardownErr != nil || teardownResult.Error != "" {
			log.Printf("[transfer] step 2/2: teardown error source_agent=%s: %v %s — containers may be orphaned", sourceWorkerID, teardownErr, teardownResult.Error)
			fmt.Fprintf(&outputBuf, "teardown failed: %v %s — containers may be orphaned.\n", teardownErr, teardownResult.Error)
		} else {
			log.Printf("[transfer] step 2/2: teardown done source_agent=%s", sourceWorkerID)
			fmt.Fprintf(&outputBuf, "teardown on %s: done.\n", sourceHostname)
		}
	} else {
		log.Printf("[transfer] step 2/2: teardown skipped — source agent offline")
		fmt.Fprintf(&outputBuf, "source agent offline — skipping teardown.\n")
	}

	duration := time.Since(start).Milliseconds()

	_ = pt.start(constants.PhasePostCheck)
	transferWorkDir, workDirErr := r.stackWorkDir(stack, stack.GetString("repository"))
	checkStatus := "active"
	checkDetail := "post-check skipped: could not resolve work dir for status query"
	if workDirErr == nil {
		check := r.postDeployCheck(ctx, targetWorkerID, stackID, transferWorkDir, composeContent)
		checkStatus = check.Status
		checkDetail = check.Detail
		postCheckStatus := constants.PhaseStatusSuccess
		if checkStatus == "error" || checkStatus == "degraded" {
			postCheckStatus = constants.PhaseStatusError
		}
		_ = pt.finish(constants.PhasePostCheck, postCheckStatus, checkDetail)
	} else {
		_ = pt.finish(constants.PhasePostCheck, constants.PhaseStatusSkipped, checkDetail)
	}
	fmt.Fprintf(&outputBuf, "\n[post-check] %s\n", checkDetail)

	// --- Step 3: Update stack record to point to the new agent ---
	stack.Set("worker", targetWorkerID)
	stack.Set("status", checkStatus)
	stack.Set("last_synced_at", time.Now().UTC().Format(time.RFC3339))
	if err := r.saveRecord(stack, "stacks", "complete transfer"); err != nil {
		_ = r.updateSyncLog(syncLog.Id, "error", "transfer succeeded but failed to persist stack state: "+err.Error(), duration)
		return err
	}

	logStatus := "success"
	if checkStatus != "active" {
		logStatus = checkStatus
	}
	if err := r.updateSyncLog(syncLog.Id, logStatus, outputBuf.String(), duration); err != nil {
		_ = r.markError(stack, "stacks")
		return err
	}

	notifyEvent := notify.SyncDone
	notifyErr := ""
	if checkStatus == "error" {
		notifyEvent = notify.SyncError
		notifyErr = checkDetail
	}
	_ = pt.start(constants.PhaseNotify)
	r.notifier.Dispatch(ctx, notify.Payload{
		Event:      notifyEvent,
		StackID:    stackID,
		StackName:  stack.GetString("name"),
		SyncLogID:  syncLogID,
		Trigger:    "transfer",
		DurationMs: duration,
		Error:      notifyErr,
	})
	_ = pt.finish(constants.PhaseNotify, constants.PhaseStatusSuccess, "")

	log.Printf("[transfer] DONE stack=%s source_agent=%s target_agent=%s elapsed=%dms", stackID, sourceWorkerID, targetWorkerID, duration)
	return nil
}
