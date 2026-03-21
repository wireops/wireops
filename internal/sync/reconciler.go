package sync

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/docker"
	gitpkg "github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/notify"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/safepath"
)

// WorkerDispatcher defines how the reconciler sends compose commands to workers.
// The MTLSServer implements this interface.
type WorkerDispatcher interface {
	Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error)
	IsEmbedded(workerID string) bool
	// IsConnected reports whether the worker currently has an active WebSocket connection.
	// Always returns true for the embedded worker.
	IsConnected(workerID string) bool
}

type Reconciler struct {
	app          core.App
	dockerClient *docker.Client
	mu           sync.Map
	secretKey    []byte
	notifier     *notify.Notifier
	renderer     *Renderer
	dispatcher   WorkerDispatcher
}

func NewReconciler(app core.App, dockerClient *docker.Client, notifier *notify.Notifier, dispatcher WorkerDispatcher) *Reconciler {
	key := []byte(os.Getenv("SECRET_KEY"))
	return &Reconciler{
		app:          app,
		dockerClient: dockerClient,
		secretKey:    key,
		notifier:     notifier,
		renderer:     NewRenderer(app),
		dispatcher:   dispatcher,
	}
}

// resolveWorker returns the worker record for a stack, plus whether it's embedded.
func (r *Reconciler) resolveWorker(stack *core.Record) (workerID, fingerprint string, err error) {
	workerID = stack.GetString("worker")
	if workerID == "" {
		return "", "embedded", nil
	}
	worker, err := r.app.FindRecordById("workers", workerID)
	if err != nil {
		return "", "", fmt.Errorf("failed to find worker %s: %w", workerID, err)
	}
	return workerID, worker.GetString("fingerprint"), nil
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

	prevStatus := stack.GetString("status")
	stack.Set("status", "syncing")
	_ = r.app.Save(stack)

	repoID := stack.GetString("repository")
	repo, err := r.app.FindRecordById("repositories", repoID)
	if err != nil {
		errMsg := fmt.Sprintf("repository %s not found for stack %s", repoID, stackID)
		r.logFailure(stackID, trigger, "", errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s: %w", errMsg, err)
	}

	// --- git fetch ---
	gitAuth, err := r.resolveGitAuth(repoID)
	if err != nil {
		log.Printf("[reconciler] no auth for repo %s: %v", repoID, err)
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

	gitRepo, err := gitpkg.CloneOrFetch(repoID, gitURL, branch, gitAuth, workspace)
	if err != nil {
		errMsg := fmt.Sprintf("git operation failed for repo %s (%s): %v", repo.GetString("name"), gitURL, err)
		r.logFailure(stackID, trigger, "", errMsg)
		r.markError(repo, "repositories")
		r.markError(stack, "stacks")
		return fmt.Errorf("git operation failed: %w", err)
	}

	remoteSHA, err := gitpkg.RemoteHeadSHA(gitRepo, branch, gitAuth)
	if err != nil {
		errMsg := fmt.Sprintf("failed to get remote SHA for branch %s: %v", branch, err)
		r.logFailure(stackID, trigger, "", errMsg)
		stack.Set("status", prevStatus)
		_ = r.app.Save(stack)
		return fmt.Errorf("failed to get remote SHA: %w", err)
	}

	lastSHA := repo.GetString("last_commit_sha")

	repo.Set("last_commit_sha", remoteSHA)
	repo.Set("last_fetched_at", time.Now().UTC().Format(time.RFC3339))
	repo.Set("status", "connected")
	_ = r.app.Save(repo)

	workerID, workerFingerprint, err := r.resolveWorker(stack)
	if err != nil {
		errMsg := fmt.Sprintf("worker resolution failed: %v", err)
		r.logFailure(stackID, trigger, "", errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	isOnline := workerFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsConnected(workerID))

	neverSynced := stack.GetString("last_synced_at") == ""
	repoChanged := gitpkg.HasChanged(remoteSHA, lastSHA)

	if !isOnline {
		if repoChanged || trigger != "cron" {
			log.Printf("[reconciler] worker %s is offline, queueing pending reconcile for stack %s", workerID, stackID)
			r.queuePendingReconcile(stackID, trigger, remoteSHA)
			stack.Set("status", prevStatus)
			_ = r.app.Save(stack)
			return nil
		}
		// Worker offline but no changes and it's a cron, just skip quietly.
		stack.Set("status", prevStatus)
		_ = r.app.Save(stack)
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
		containerSHA = r.inspectStackCommit(ctx, workerID, workerFingerprint, stackID)
	}

	if trigger == "cron" && !neverSynced && !repoChanged && containerSHA == remoteSHA {
		stack.Set("status", prevStatus)
		_ = r.app.Save(stack)
		return nil
	}

	commitMsg := ""
	if obj, err := gitRepo.CommitObject(mustParseHash(remoteSHA)); err == nil {
		commitMsg = obj.Message
	}

	// --- compose deploy ---
	workDir, err := r.stackWorkDir(stack, repoID)
	if err != nil {
		errMsg := fmt.Sprintf("invalid compose_path: %v", err)
		r.logFailure(stackID, trigger, remoteSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	composeFile := stack.GetString("compose_file")
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}
	if err := safepath.ValidateComposeFile(composeFile); err != nil {
		errMsg := fmt.Sprintf("invalid compose_file: %v", err)
		r.logFailure(stackID, trigger, remoteSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	composeFullPath := filepath.Join(workDir, composeFile)
	if _, statErr := os.Stat(composeFullPath); os.IsNotExist(statErr) {
		errMsg := fmt.Sprintf("compose file not found: %s (workdir: %s)", composeFile, workDir)
		r.logFailure(stackID, trigger, remoteSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	envVars, envErr := r.loadEnvVars(ctx, stackID)
	if envErr != nil {
		log.Printf("[reconciler] failed to load env vars for stack %s: %v", stackID, envErr)
	}

	// Reload stack after possible checksum/version update by renderer setup.
	// (stack record may have been modified above by markError etc.)
	stack, err = r.app.FindRecordById("stacks", stackID)
	if err != nil {
		return fmt.Errorf("stack vanished mid-reconcile: %w", err)
	}
	prevChecksum := stack.GetString("checksum")
	prevVersion := stack.GetInt("current_version")

	renderRes, err := r.renderer.GenerateRevision(ctx, stack, repo, workDir, composeFile, envVars, remoteSHA, false, workerFingerprint)
	if err != nil {
		errMsg := fmt.Sprintf("failed to generate label revision: %v", err)
		r.logFailure(stackID, trigger, remoteSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	// If the renderer found no changes (same checksum → same version returned),
	// the compose file content is identical to what's already deployed. Skip the
	// deploy regardless of whether the commit SHA changed — a new commit may have
	// only touched other files in the repo (e.g. README, job.yaml, etc.).
	// The compose checksum is the definitive signal, not the commit SHA.
	if trigger == "cron" && !neverSynced &&
		renderRes.Checksum == prevChecksum && renderRes.Version == prevVersion {
		log.Printf("[reconciler] cron skip: compose unchanged for stack %s (checksum=%s)", stackID, renderRes.Checksum)
		stack.Set("status", prevStatus)
		_ = r.app.Save(stack)
		return nil
	}

	syncLog, err := r.createSyncLog(stackID, trigger, remoteSHA, commitMsg)
	if err != nil {
		return fmt.Errorf("failed to create sync log: %w", err)
	}

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

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		start := time.Now()
		if workerFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(workerID)) {
			// Embedded: run compose directly on the server host
			output, runErr = compose.RunUp(ctx, compose.RunOptions{
				WorkDir:     filepath.Dir(renderedFilePath),
				ComposeFile: renderRes.RenderedPath,
				EnvVars:     envVars,
			})
		} else {
			// Remote worker: send command over WebSocket
			composeContent, readErr := os.ReadFile(renderedFilePath)
			if readErr != nil {
				errMsg := fmt.Sprintf("failed to read rendered compose file: %v", readErr)
				r.logFailure(stackID, trigger, remoteSHA, errMsg)
				r.markError(stack, "stacks")
				return fmt.Errorf("%s", errMsg)
			}
			result, dispatchErr := r.dispatcher.Dispatch(ctx, workerID, protocol.DeployCommand{
				CommandID:      syncLog.Id,
				StackID:        stackID,
				CommitSHA:      remoteSHA,
				Trigger:        trigger,
				QueueTotal:     queueTotal,
				ComposeFileB64: base64.StdEncoding.EncodeToString(composeContent),
				EnvVars:        envVars,
			})
			output = result.Output
			runErr = nil
			if result.Error != "" {
				runErr = fmt.Errorf("%s", result.Error)
			}
			if dispatchErr != nil {
				runErr = dispatchErr
			}
		}

		duration += time.Since(start).Milliseconds()

		if runErr == nil {
			break // Success
		}

		if attempt < maxRetries {
			log.Printf("[reconciler] deploy attempt %d of %d failed for stack %s: %v, retrying in 3s...", attempt, maxRetries, stackID, runErr)
			if syncLog != nil {
				r.updateSyncLog(syncLog.Id, "running", fmt.Sprintf("%s\n\n[Attempt %d failed: %v. Retrying in 3s...]\n", output, attempt, runErr), duration)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}
	}

	if runErr != nil {
		errOutput := buildErrorOutput(output, runErr, envErr)
		r.updateSyncLog(syncLog.Id, "error", errOutput, duration)
		r.markError(stack, "stacks")
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
		return runErr
	}

	r.updateSyncLog(syncLog.Id, "done", output, duration)
	r.notifier.Dispatch(ctx, notify.Payload{
		Event:      notify.SyncDone,
		StackID:    stackID,
		StackName:  stack.GetString("name"),
		SyncLogID:  syncLog.Id,
		Trigger:    trigger,
		CommitSHA:  remoteSHA,
		DurationMs: duration,
	})

	stack.Set("last_synced_at", time.Now().UTC().Format(time.RFC3339))
	stack.Set("status", "active")
	_ = r.app.Save(stack)

	if workerFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(workerID)) {
		r.refreshServiceStatus(ctx, stackID, workDir)
	}

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

	stack.Set("status", "syncing")
	_ = r.app.Save(stack)

	repoID := stack.GetString("repository")
	repo, err := r.app.FindRecordById("repositories", repoID)
	if err != nil {
		errMsg := fmt.Sprintf("repository %s not found for stack %s", repoID, stackID)
		r.logFailure(stackID, "manual", commitSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s: %w", errMsg, err)
	}

	workspace := r.reposWorkspace()
	repoDir := filepath.Join(workspace, repoID)

	gogitRepo, err := gogit.PlainOpen(repoDir)
	if err != nil {
		errMsg := fmt.Sprintf("failed to open local repo directory: %s", repoDir)
		r.logFailure(stackID, "manual", commitSHA, errMsg)
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
		r.logFailure(stackID, "manual", commitSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("git reset failed: %w", err)
	}

	workDir, err := r.stackWorkDir(stack, repoID)
	if err != nil {
		errMsg := fmt.Sprintf("invalid compose_path: %v", err)
		r.logFailure(stackID, "manual", commitSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	composeFile := stack.GetString("compose_file")
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}
	if err := safepath.ValidateComposeFile(composeFile); err != nil {
		errMsg := fmt.Sprintf("invalid compose_file: %v", err)
		r.logFailure(stackID, "manual", commitSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	composeFullPath := filepath.Join(workDir, composeFile)
	if _, statErr := os.Stat(composeFullPath); os.IsNotExist(statErr) {
		errMsg := fmt.Sprintf("compose file not found after rollback: %s (workdir: %s)", composeFile, workDir)
		r.logFailure(stackID, "manual", commitSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	envVars, envErr := r.loadEnvVars(ctx, stackID)
	if envErr != nil {
		log.Printf("[reconciler] failed to load env vars for stack %s: %v", stackID, envErr)
	}

	workerID, workerFingerprint, err := r.resolveWorker(stack)
	if err != nil {
		errMsg := fmt.Sprintf("agent resolution failed: %v", err)
		r.logFailure(stackID, "manual", commitSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	renderRes, err := r.renderer.GenerateRevision(ctx, stack, repo, workDir, composeFile, envVars, commitSHA, true, workerFingerprint)
	if err != nil {
		errMsg := fmt.Sprintf("failed to generate label revision on rollback: %v", err)
		r.logFailure(stackID, "manual", commitSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	syncLog, _ := r.createSyncLog(stackID, "manual", commitSHA, "rollback to "+commitSHA)
	if syncLog != nil {
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:     notify.SyncStarted,
			StackID:   stackID,
			StackName: stack.GetString("name"),
			SyncLogID: syncLog.Id,
			Trigger:   "manual",
			CommitSHA: commitSHA,
		})
	}

	start := time.Now()

	renderedFilePath := r.renderer.GetRevisionFilePath(stackID, renderRes.Version)
	var output string
	var runErr error

	if workerFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(workerID)) {
		output, runErr = compose.RunUp(ctx, compose.RunOptions{
			WorkDir:     filepath.Dir(renderedFilePath),
			ComposeFile: renderRes.RenderedPath,
			EnvVars:     envVars,
		})
	} else {
		composeContent, readErr := os.ReadFile(renderedFilePath)
		if readErr != nil {
			errMsg := fmt.Sprintf("failed to read rendered compose file: %v", readErr)
			r.logFailure(stackID, "manual", commitSHA, errMsg)
			r.markError(stack, "stacks")
			return fmt.Errorf("%s", errMsg)
		}
		var cmdID string
		if syncLog != nil {
			cmdID = syncLog.Id
		}
		result, dispatchErr := r.dispatcher.Dispatch(ctx, workerID, protocol.DeployCommand{
			CommandID:      cmdID,
			StackID:        stackID,
			CommitSHA:      commitSHA,
			Trigger:        "rollback",
			ComposeFileB64: base64.StdEncoding.EncodeToString(composeContent),
			EnvVars:        envVars,
		})
		output = result.Output
		if result.Error != "" {
			runErr = fmt.Errorf("%s", result.Error)
		}
		if dispatchErr != nil {
			runErr = dispatchErr
		}
	}

	duration := time.Since(start).Milliseconds()

	if runErr != nil {
		if syncLog != nil {
			errOutput := buildErrorOutput(output, runErr, envErr)
			r.updateSyncLog(syncLog.Id, "error", errOutput, duration)
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
		}
		r.markError(stack, "stacks")
		return runErr
	}

	if syncLog != nil {
		r.updateSyncLog(syncLog.Id, "done", output, duration)
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:      notify.SyncDone,
			StackID:    stackID,
			StackName:  stack.GetString("name"),
			SyncLogID:  syncLog.Id,
			Trigger:    "manual",
			CommitSHA:  commitSHA,
			DurationMs: duration,
		})
	}

	repo.Set("last_commit_sha", commitSHA)
	repo.Set("last_fetched_at", time.Now().UTC().Format(time.RFC3339))
	_ = r.app.Save(repo)

	stack.Set("last_synced_at", time.Now().UTC().Format(time.RFC3339))
	stack.Set("status", "paused")
	_ = r.app.Save(stack)

	if workerFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(workerID)) {
		r.refreshServiceStatus(ctx, stackID, workDir)
	}

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

	stack.Set("status", "syncing")
	_ = r.app.Save(stack)

	repoID := stack.GetString("repository")
	repo, err := r.app.FindRecordById("repositories", repoID)
	if err != nil {
		errMsg := fmt.Sprintf("repository %s not found for stack %s", repoID, stackID)
		r.logFailure(stackID, "redeploy", "", errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s: %w", errMsg, err)
	}

	workDir, err := r.stackWorkDir(stack, repoID)
	if err != nil {
		errMsg := fmt.Sprintf("invalid compose_path: %v", err)
		r.logFailure(stackID, "redeploy", "", errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}
	composeFile := stack.GetString("compose_file")
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}

	lastSHA := repo.GetString("last_commit_sha")
	envVars, envErr := r.loadEnvVars(ctx, stackID)
	if envErr != nil {
		log.Printf("[reconciler] failed to load env vars for stack %s: %v", stackID, envErr)
	}

	workerID, workerFingerprint, err := r.resolveWorker(stack)
	if err != nil {
		errMsg := fmt.Sprintf("agent resolution failed: %v", err)
		r.logFailure(stackID, "redeploy", lastSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	renderRes, err := r.renderer.GenerateRevision(ctx, stack, repo, workDir, composeFile, envVars, lastSHA, true, workerFingerprint)
	if err != nil {
		errMsg := fmt.Sprintf("failed to generate label revision on redeploy: %v", err)
		r.logFailure(stackID, "redeploy", lastSHA, errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	syncLog, _ := r.createSyncLog(stackID, "redeploy", lastSHA, "force redeploy")
	if syncLog != nil {
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:     notify.SyncStarted,
			StackID:   stackID,
			StackName: stack.GetString("name"),
			SyncLogID: syncLog.Id,
			Trigger:   "redeploy",
			CommitSHA: lastSHA,
		})
	}

	start := time.Now()

	renderedFilePath := r.renderer.GetRevisionFilePath(stackID, renderRes.Version)
	var output string
	var runErr error

	if workerFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(workerID)) {
		output, runErr = compose.RunForceUp(ctx, compose.ForceUpOptions{
			RunOptions: compose.RunOptions{
				WorkDir:     filepath.Dir(renderedFilePath),
				ComposeFile: renderRes.RenderedPath,
				EnvVars:     envVars,
			},
			RecreateContainers: recreateContainers,
			RecreateVolumes:    recreateVolumes,
			RecreateNetworks:   recreateNetworks,
		})
	} else {
		composeContent, readErr := os.ReadFile(renderedFilePath)
		if readErr != nil {
			errMsg := fmt.Sprintf("failed to read rendered compose file: %v", readErr)
			r.logFailure(stackID, "redeploy", lastSHA, errMsg)
			r.markError(stack, "stacks")
			return fmt.Errorf("%s", errMsg)
		}
		var cmdID string
		if syncLog != nil {
			cmdID = syncLog.Id
		}
		result, dispatchErr := r.dispatcher.Dispatch(ctx, workerID, protocol.RedeployCommand{
			DeployCommand: protocol.DeployCommand{
				CommandID:      cmdID,
				StackID:        stackID,
				CommitSHA:      lastSHA,
				Trigger:        "force-redeploy",
				ComposeFileB64: base64.StdEncoding.EncodeToString(composeContent),
				EnvVars:        envVars,
			},
			RecreateContainers: recreateContainers,
			RecreateVolumes:    recreateVolumes,
			RecreateNetworks:   recreateNetworks,
		})
		output = result.Output
		if result.Error != "" {
			runErr = fmt.Errorf("%s", result.Error)
		}
		if dispatchErr != nil {
			runErr = dispatchErr
		}
	}

	duration := time.Since(start).Milliseconds()

	if runErr != nil {
		if syncLog != nil {
			errOutput := buildErrorOutput(output, runErr, envErr)
			r.updateSyncLog(syncLog.Id, "error", errOutput, duration)
			r.notifier.Dispatch(ctx, notify.Payload{
				Event:      notify.SyncError,
				StackID:    stackID,
				StackName:  stack.GetString("name"),
				SyncLogID:  syncLog.Id,
				Trigger:    "redeploy",
				CommitSHA:  lastSHA,
				DurationMs: duration,
				Error:      runErr.Error(),
			})
		}
		r.markError(stack, "stacks")
		return runErr
	}

	if syncLog != nil {
		r.updateSyncLog(syncLog.Id, "done", output, duration)
		r.notifier.Dispatch(ctx, notify.Payload{
			Event:      notify.SyncDone,
			StackID:    stackID,
			StackName:  stack.GetString("name"),
			SyncLogID:  syncLog.Id,
			Trigger:    "redeploy",
			CommitSHA:  lastSHA,
			DurationMs: duration,
		})
	}

	stack.Set("last_synced_at", time.Now().UTC().Format(time.RFC3339))
	stack.Set("status", "paused")
	_ = r.app.Save(stack)

	if workerFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(workerID)) {
		r.refreshServiceStatus(ctx, stackID, workDir)
	}

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
func (r *Reconciler) reconcileLocalStack(ctx context.Context, stackID string, stack *core.Record, trigger string) error {
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
	isOnline := workerFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsConnected(workerID))
	if !isOnline {
		log.Printf("[reconciler] worker %s is offline, queueing pending reconcile for local stack %s", workerID, stackID)
		r.queuePendingReconcile(stackID, trigger, "")
		return nil
	}

	prevStatus := stack.GetString("status")
	stack.Set("status", "syncing")
	_ = r.app.Save(stack)

	// Read the compose file from the host (direct for embedded, via ReadFileCommand for remote).
	var composeContent []byte
	var workDir, composeFile string

	isEmbedded := workerFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(workerID))
	if isEmbedded {
		data, err := os.ReadFile(importPath)
		if err != nil {
			errMsg := fmt.Sprintf("failed to read compose file %s: %v", importPath, err)
			r.logFailure(stackID, trigger, "", errMsg)
			r.markError(stack, "stacks")
			return fmt.Errorf("%s", errMsg)
		}
		composeContent = data
		workDir = filepath.Dir(importPath)
		composeFile = filepath.Base(importPath)
	} else {
		cmdID := fmt.Sprintf("readfile-%s", stackID)
		result, dispatchErr := r.dispatcher.Dispatch(ctx, workerID, protocol.ReadFileCommand{
			CommandID: cmdID,
			Path:      importPath,
		})
		if dispatchErr != nil || result.Error != "" {
			errMsg := fmt.Sprintf("failed to read remote compose file %s: %v %s", importPath, dispatchErr, result.Error)
			r.logFailure(stackID, trigger, "", errMsg)
			r.markError(stack, "stacks")
			return fmt.Errorf("%s", errMsg)
		}
		data, err := base64.StdEncoding.DecodeString(result.Output)
		if err != nil {
			errMsg := fmt.Sprintf("failed to decode remote compose file: %v", err)
			r.logFailure(stackID, trigger, "", errMsg)
			r.markError(stack, "stacks")
			return fmt.Errorf("%s", errMsg)
		}
		composeContent = data

		// Store a local copy so compose.Config can run on the server side.
		sourceDir := filepath.Join(r.app.DataDir(), "stacks", stackID)
		if mkErr := os.MkdirAll(sourceDir, 0755); mkErr != nil {
			errMsg := fmt.Sprintf("failed to create source dir: %v", mkErr)
			r.logFailure(stackID, trigger, "", errMsg)
			r.markError(stack, "stacks")
			return fmt.Errorf("%s", errMsg)
		}
		sourceFile := filepath.Join(sourceDir, "source.yml")
		if writeErr := os.WriteFile(sourceFile, composeContent, 0644); writeErr != nil {
			errMsg := fmt.Sprintf("failed to write source file: %v", writeErr)
			r.logFailure(stackID, trigger, "", errMsg)
			r.markError(stack, "stacks")
			return fmt.Errorf("%s", errMsg)
		}
		workDir = sourceDir
		composeFile = "source.yml"
	}

	// Change detection: compare SHA256 of raw file content with stored checksum.
	newChecksum := fmt.Sprintf("%x", sha256bytes(composeContent))
	currentChecksum := stack.GetString("checksum")
	neverSynced := stack.GetString("last_synced_at") == ""
	fileChanged := newChecksum != currentChecksum

	if trigger == "cron" && !neverSynced && !fileChanged {
		stack.Set("status", prevStatus)
		_ = r.app.Save(stack)
		return nil
	}

	envVars, envErr := r.loadEnvVars(ctx, stackID)
	if envErr != nil {
		log.Printf("[reconciler] failed to load env vars for local stack %s: %v", stackID, envErr)
	}

	renderRes, err := r.renderer.GenerateRevision(ctx, stack, nil, workDir, composeFile, envVars, "imported", false, workerFingerprint)
	if err != nil {
		errMsg := fmt.Sprintf("failed to generate label revision: %v", err)
		r.logFailure(stackID, trigger, "", errMsg)
		r.markError(stack, "stacks")
		return fmt.Errorf("%s", errMsg)
	}

	syncLog, err := r.createSyncLog(stackID, trigger, "imported", "local stack sync")
	if err != nil {
		return fmt.Errorf("failed to create sync log: %w", err)
	}

	renderedFilePath := r.renderer.GetRevisionFilePath(stackID, renderRes.Version)
	recreateContainers := neverSynced
	recreateVolumes := false
	if neverSynced {
		recreateVolumes = stack.GetBool("import_recreate_volumes")
	}

	var output string
	var runErr error
	start := time.Now()

	if isEmbedded {
		opts := compose.RunOptions{
			WorkDir:     filepath.Dir(renderedFilePath),
			ComposeFile: renderRes.RenderedPath,
			EnvVars:     envVars,
		}
		if recreateContainers {
			output, runErr = compose.RunForceUp(ctx, compose.ForceUpOptions{
				RunOptions:         opts,
				RecreateContainers: true,
				RecreateVolumes:    recreateVolumes,
			})
		} else {
			output, runErr = compose.RunUp(ctx, opts)
		}
	} else {
		composeBytes, readErr := os.ReadFile(renderedFilePath)
		if readErr != nil {
			errMsg := fmt.Sprintf("failed to read rendered compose file: %v", readErr)
			r.logFailure(stackID, trigger, "", errMsg)
			r.markError(stack, "stacks")
			return fmt.Errorf("%s", errMsg)
		}
		b64 := base64.StdEncoding.EncodeToString(composeBytes)

		if recreateContainers {
			result, dispatchErr := r.dispatcher.Dispatch(ctx, workerID, protocol.RedeployCommand{
				DeployCommand: protocol.DeployCommand{
					CommandID:      syncLog.Id,
					StackID:        stackID,
					CommitSHA:      "imported",
					Trigger:        trigger,
					ComposeFileB64: b64,
					EnvVars:        envVars,
				},
				RecreateContainers: true,
				RecreateVolumes:    recreateVolumes,
			})
			output = result.Output
			if result.Error != "" {
				runErr = fmt.Errorf("%s", result.Error)
			}
			if dispatchErr != nil {
				runErr = dispatchErr
			}
		} else {
			result, dispatchErr := r.dispatcher.Dispatch(ctx, workerID, protocol.DeployCommand{
				CommandID:      syncLog.Id,
				StackID:        stackID,
				CommitSHA:      "imported",
				Trigger:        trigger,
				ComposeFileB64: b64,
				EnvVars:        envVars,
			})
			output = result.Output
			if result.Error != "" {
				runErr = fmt.Errorf("%s", result.Error)
			}
			if dispatchErr != nil {
				runErr = dispatchErr
			}
		}
	}

	duration := time.Since(start).Milliseconds()

	if runErr != nil {
		errOutput := buildErrorOutput(output, runErr, envErr)
		r.updateSyncLog(syncLog.Id, "error", errOutput, duration)
		r.markError(stack, "stacks")
		return runErr
	}

	r.updateSyncLog(syncLog.Id, "done", output, duration)

	// Update the stack's raw-file checksum after a successful deploy.
	stack.Set("checksum", newChecksum)
	stack.Set("last_synced_at", time.Now().UTC().Format(time.RFC3339))
	stack.Set("status", "active")
	_ = r.app.Save(stack)

	if isEmbedded {
		r.refreshServiceStatus(ctx, stackID, workDir)
	}

	return nil
}

func sha256bytes(data []byte) []byte {
	h := sha256.New()
	h.Write(data)
	return h.Sum(nil)
}

func (r *Reconciler) queuePendingReconcile(stackID, trigger, commitSHA string) {
	col, err := r.app.FindCollectionByNameOrId("stack_pending_reconciles")
	if err != nil {
		log.Printf("[reconciler] failed to find stack_pending_reconciles collection: %v", err)
		return
	}

	// Delete any existing pending reconcile for this stack to avoid duplicates
	existing, err := r.app.FindAllRecords("stack_pending_reconciles", dbx.HashExp{"stack": stackID})
	if err == nil {
		for _, rec := range existing {
			_ = r.app.Delete(rec)
		}
	}

	record := core.NewRecord(col)
	record.Set("stack", stackID)
	record.Set("trigger", trigger)
	record.Set("commit_sha", commitSHA)

	if err := r.app.Save(record); err != nil {
		log.Printf("[reconciler] failed to save pending reconcile: %v", err)
	}

	queueLog, err := r.createSyncLog(stackID, "queue", commitSHA, "Added to offline queue (original trigger: "+trigger+")")
	if err == nil {
		r.updateSyncLog(queueLog.Id, "queued", "Agent is offline. Sync will proceed when agent reconnects.", 0)
	}
}

func (r *Reconciler) inspectStackCommit(ctx context.Context, workerID, workerFingerprint, stackID string) string {
	if workerFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(workerID)) {
		if r.dockerClient != nil {
			sha, err := r.dockerClient.GetRunningStackCommit(ctx, stackID)
			if err != nil {
				log.Printf("[reconciler] failed to inspect embedded stack %s: %v", stackID, err)
				return ""
			}
			return sha
		}
		return ""
	}

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
	return filepath.Join(r.app.DataDir(), "repositories")
}

func (r *Reconciler) resolveGitAuth(repoID string) (transport.AuthMethod, error) {
	cred, err := r.loadCredential(repoID)
	if err != nil {
		return nil, err
	}
	auth, err := gitpkg.ResolveAuth(*cred)
	if err != nil {
		return nil, err
	}
	return toTransportAuth(auth), nil
}

func (r *Reconciler) loadCredential(repoID string) (*gitpkg.Credential, error) {
	records, err := r.app.FindAllRecords("repository_keys",
		dbx.HashExp{"repository": repoID},
	)
	if err != nil || len(records) == 0 {
		return nil, fmt.Errorf("no credential found")
	}

	rec := records[0]
	authType := gitpkg.AuthType(rec.GetString("auth_type"))
	cred := &gitpkg.Credential{AuthType: authType}

	switch authType {
	case gitpkg.AuthTypeSSH:
		keyEnc := rec.GetString("ssh_private_key")
		if keyEnc != "" && len(r.secretKey) == 32 {
			if keyBytes, err := crypto.Decrypt(keyEnc, r.secretKey); err == nil {
				cred.SSHPrivateKey = keyBytes
			}
		}
		ppEnc := rec.GetString("ssh_passphrase")
		if ppEnc != "" && len(r.secretKey) == 32 {
			if ppBytes, err := crypto.Decrypt(ppEnc, r.secretKey); err == nil {
				cred.SSHPassphrase = ppBytes
			}
		}
		cred.SSHKnownHost = rec.GetString("ssh_known_host")

	case gitpkg.AuthTypeBasic:
		cred.GitUsername = rec.GetString("git_username")
		pwdEnc := rec.GetString("git_password")
		if pwdEnc != "" && len(r.secretKey) == 32 {
			if pwdBytes, err := crypto.Decrypt(pwdEnc, r.secretKey); err == nil {
				cred.GitPassword = string(pwdBytes)
			}
		}
	}

	return cred, nil
}

func (r *Reconciler) loadEnvVars(ctx context.Context, stackID string) ([]string, error) {
	records, err := r.app.FindAllRecords("stack_env_vars",
		dbx.HashExp{"stack": stackID},
	)
	if err != nil {
		return nil, err
	}

	var envVars []string
	for _, rec := range records {
		key := rec.GetString("key")
		val := rec.GetString("value")
		if key == "" {
			continue
		}

		if rec.GetBool("secret") && len(r.secretKey) == 32 {
			// Handle standard Wires encrypted secrets
			if decrypted, err := crypto.Decrypt(val, r.secretKey); err == nil {
				val = string(decrypted)
			}
		}

		envVars = append(envVars, key+"="+val)
	}
	return envVars, nil
}

func (r *Reconciler) createSyncLog(stackID, trigger, commitSHA, commitMsg string) (*core.Record, error) {
	collection, err := r.app.FindCollectionByNameOrId("sync_logs")
	if err != nil {
		return nil, err
	}
	record := core.NewRecord(collection)
	record.Set("stack", stackID)
	record.Set("trigger", trigger)
	record.Set("status", "running")
	record.Set("commit_sha", commitSHA)
	record.Set("commit_message", commitMsg)
	if err := r.app.Save(record); err != nil {
		return nil, err
	}
	return record, nil
}

func (r *Reconciler) updateSyncLog(id, status, output string, durationMs int64) {
	record, err := r.app.FindRecordById("sync_logs", id)
	if err != nil {
		log.Printf("[reconciler] updateSyncLog: record %s not found: %v", id, err)
		return
	}
	record.Set("status", status)
	record.Set("output", output)
	record.Set("duration_ms", durationMs)
	if err := r.app.Save(record); err != nil {
		log.Printf("[reconciler] updateSyncLog: failed to save record %s: %v", id, err)
	}
}

func (r *Reconciler) markError(rec *core.Record, _ string) {
	rec.Set("status", "error")
	_ = r.app.Save(rec)
	log.Printf("[reconciler] %s error: %s", rec.Id, rec.GetString("status"))
}

// logFailure creates a sync log entry for early failures (before the normal sync log is created).
func (r *Reconciler) logFailure(stackID, trigger, commitSHA, errMsg string) {
	log.Printf("[reconciler] stack %s failure: %s", stackID, errMsg)
	syncLog, err := r.createSyncLog(stackID, trigger, commitSHA, "")
	if err != nil {
		log.Printf("[reconciler] failed to create failure sync log: %v", err)
		return
	}
	r.updateSyncLog(syncLog.Id, "error", errMsg, 0)
}

func buildErrorOutput(output string, runErr, envErr error) string {
	var b strings.Builder
	if envErr != nil {
		fmt.Fprintf(&b, "warning: failed to load env vars: %v\n\n", envErr)
	}
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

func (r *Reconciler) refreshServiceStatus(_ context.Context, stackID, workDir string) {
	if r.dockerClient == nil {
		log.Printf("[reconciler] refreshServiceStatus: docker client is nil")
		return
	}
	projectName := compose.ProjectName(workDir)
	log.Printf("[reconciler] refreshServiceStatus: stack=%s project=%s workDir=%s", stackID, projectName, workDir)
	statuses, err := compose.GetStackStatus(context.Background(), r.dockerClient.Raw(), projectName)
	if err != nil {
		log.Printf("[reconciler] failed to get service status: %v", err)
		return
	}

	collection, err := r.app.FindCollectionByNameOrId("stack_services")
	if err != nil {
		return
	}

	existing, _ := r.app.FindAllRecords("stack_services",
		dbx.HashExp{"stack": stackID},
	)
	existingMap := make(map[string]*core.Record)
	for _, rec := range existing {
		existingMap[rec.GetString("service_name")] = rec
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, s := range statuses {
		var record *core.Record
		if rec, ok := existingMap[s.ServiceName]; ok {
			record = rec
		} else {
			record = core.NewRecord(collection)
			record.Set("stack", stackID)
			record.Set("service_name", s.ServiceName)
		}
		record.Set("container_name", s.ContainerName)
		record.Set("status", s.Status)
		record.Set("container_id", s.ContainerID)
		record.Set("last_checked_at", now)
		_ = r.app.Save(record)
	}
}

// TransferStack provisions the stack on targetWorkerID, then tears it down on the
// original agent, and updates the stack record to point to the new agent.
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

	sourceWorkerID := stack.GetString("worker")
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

	workDir := filepath.Dir(composeFilePath)

	envVars, envErr := r.loadEnvVars(ctx, stackID)
	if envErr != nil {
		log.Printf("[reconciler] failed to load env vars for stack %s: %v", stackID, envErr)
	}

	composeB64 := base64.StdEncoding.EncodeToString(composeContent)

	// Resolve worker hostnames and fingerprints for human-friendly sync log messages.
	sourceHostname := sourceWorkerID
	sourceFingerprint := ""
	if sourceWorkerID == "" {
		sourceFingerprint = "embedded"
		sourceHostname = "Server (Embedded)"
	} else if a, err := r.app.FindRecordById("workers", sourceWorkerID); err != nil {
		return fmt.Errorf("failed to find source worker %s: %w", sourceWorkerID, err)
	} else {
		sourceHostname = a.GetString("hostname")
		sourceFingerprint = a.GetString("fingerprint")
	}

	targetHostname := targetWorkerID
	targetFingerprint := ""
	if targetWorkerID == "" {
		targetFingerprint = "embedded"
		targetHostname = "Server (Embedded)"
	} else if a, err := r.app.FindRecordById("workers", targetWorkerID); err != nil {
		return fmt.Errorf("failed to find target worker %s: %w", targetWorkerID, err)
	} else {
		targetHostname = a.GetString("hostname")
		targetFingerprint = a.GetString("fingerprint")
	}

	prevStatus := stack.GetString("status")

	// Mark stack as syncing during the transfer
	stack.Set("status", "syncing")
	_ = r.app.Save(stack)

	syncLog, _ := r.createSyncLog(stackID, "transfer", "",
		fmt.Sprintf("%s → %s", sourceHostname, targetHostname))

	syncLogID := ""
	if syncLog != nil {
		syncLogID = syncLog.Id
	}

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

		if syncLog != nil {
			r.updateSyncLog(syncLog.Id, "error", outputBuf.String(), time.Since(start).Milliseconds())
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
		stack.Set("status", prevStatus)
		r.app.Save(stack)
		return fmt.Errorf("transfer failed: %s", errMsg)
	}

	// --- Pre-flight 2: probe agent B to detect existing containers ---
	// If containers (any state) already exist for this project on the target host,
	// we abort early to avoid conflicting volumes, networks, or port bindings.
	var probeErrMsg string
	if targetFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(targetWorkerID)) {
		log.Printf("[transfer] probe: running locally for target_agent=%s stack=%s", targetWorkerID, stackID)
		services, _ := compose.RunPs(ctx, compose.RunOptions{
			WorkDir:     workDir,
			ComposeFile: composeFilePath,
		})
		log.Printf("[transfer] probe: target_agent=%s containers=%d services=%v", targetWorkerID, len(services), services)
		if len(services) > 0 {
			probeErrMsg = fmt.Sprintf(
				"target agent %s already has %d container(s) for this stack (services: %s) — "+
					"remove them manually before transferring",
				targetHostname, len(services), strings.Join(services, ", "),
			)
		}
	} else {
		probeID := fmt.Sprintf("probe-%s", stackID)
		log.Printf("[transfer] probe: dispatching to target_agent=%s stack=%s", targetWorkerID, stackID)
		probeResult, probeErr := r.dispatcher.Dispatch(ctx, targetWorkerID, protocol.ProbeCommand{
			CommandID:      probeID,
			StackID:        stackID,
			ComposeFileB64: composeB64,
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
	}

	if probeErrMsg != "" {
		log.Printf("[transfer] validation error: %s", probeErrMsg)
		outputBuf.WriteString("error: " + probeErrMsg + "\n")

		if syncLog != nil {
			r.updateSyncLog(syncLog.Id, "error", outputBuf.String(), time.Since(start).Milliseconds())
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
		stack.Set("status", prevStatus)
		r.app.Save(stack)
		return fmt.Errorf("transfer failed: %s", probeErrMsg)
	}
	fmt.Fprintf(&outputBuf, "=== Step 1/2: Deploy on target agent (%s) ===\n", targetHostname)

	// --- Step 1: Deploy on target agent (agent B) ---
	cmdID := ""
	if syncLog != nil {
		cmdID = syncLog.Id
	}

	var deployOutput string
	var deployErr error
	var dispatchErr error

	if targetFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(targetWorkerID)) {
		log.Printf("[transfer] step 1/2: deploy running locally for target_agent=%s (%s) stack=%s", targetWorkerID, targetHostname, stackID)
		deployOutput, deployErr = compose.RunUp(ctx, compose.RunOptions{
			WorkDir:     workDir,
			ComposeFile: composeFilePath,
			EnvVars:     envVars,
		})
	} else {
		log.Printf("[transfer] step 1/2: deploy dispatching to target_agent=%s (%s) stack=%s", targetWorkerID, targetHostname, stackID)
		deployResult, dErr := r.dispatcher.Dispatch(ctx, targetWorkerID, protocol.DeployCommand{
			CommandID:      cmdID,
			StackID:        stackID,
			Trigger:        "transfer",
			ComposeFileB64: composeB64,
			EnvVars:        envVars,
		})
		deployOutput = deployResult.Output
		dispatchErr = dErr
		if deployResult.Error != "" {
			deployErr = fmt.Errorf("%s", deployResult.Error)
		}
	}

	if dispatchErr != nil || deployErr != nil {
		deployErrMsg := fmt.Sprintf("%v%v", dispatchErr, deployErr)
		log.Printf("[transfer] step 1/2: deploy error target_agent=%s elapsed=%dms: %s", targetWorkerID, time.Since(start).Milliseconds(), deployErrMsg)
		outputBuf.WriteString(deployOutput)
		fmt.Fprintf(&outputBuf, "\nerror: %s\n", deployErrMsg)
		fmt.Fprintf(&outputBuf, "\n=== Step 2/2: Cleanup on target agent (%s) ===\n", targetHostname)

		// Best-effort cleanup on agent B — remove any partial containers it may have started.
		if targetFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(targetWorkerID)) {
			log.Printf("[transfer] step 2/2: cleanup running locally for target_agent=%s stack=%s", targetWorkerID, stackID)
			cleanupOutput, cleanupErr := compose.RunDown(ctx, compose.RunOptions{
				WorkDir:     workDir,
				ComposeFile: composeFilePath,
			})
			if cleanupErr != nil {
				log.Printf("[transfer] step 2/2: cleanup error target_agent=%s: %v", targetWorkerID, cleanupErr)
				fmt.Fprintf(&outputBuf, "cleanup teardown failed: %v\n", cleanupErr)
			} else {
				log.Printf("[transfer] step 2/2: cleanup done target_agent=%s", targetWorkerID)
				outputBuf.WriteString(cleanupOutput)
				fmt.Fprintf(&outputBuf, "cleanup teardown succeeded.\n")
			}
		} else if r.dispatcher != nil && r.dispatcher.IsConnected(targetWorkerID) {
			log.Printf("[transfer] step 2/2: cleanup dispatching to target_agent=%s stack=%s", targetWorkerID, stackID)
			cleanupResult, cleanupErr := r.dispatcher.Dispatch(ctx, targetWorkerID, protocol.TeardownCommand{
				CommandID:      fmt.Sprintf("teardown-cleanup-%s", stackID),
				StackID:        stackID,
				ComposeFileB64: composeB64,
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

		if syncLog != nil {
			r.updateSyncLog(syncLog.Id, "error", outputBuf.String(), time.Since(start).Milliseconds())
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
		stack.Set("status", "error")
		_ = r.app.Save(stack)
		return fmt.Errorf("transfer failed: %s", deployErrMsg)
	}

	outputBuf.WriteString(deployOutput)
	fmt.Fprintf(&outputBuf, "deploy on %s: done.\n", targetHostname)
	log.Printf("[transfer] step 1/2: deploy done target_agent=%s elapsed=%dms", targetWorkerID, time.Since(start).Milliseconds())

	// --- Step 2: Teardown on source agent (agent A) ---
	fmt.Fprintf(&outputBuf, "\n=== Step 2/2: Teardown on source agent (%s) ===\n", sourceHostname)
	if sourceFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(sourceWorkerID)) {
		log.Printf("[transfer] step 2/2: teardown running locally for source_agent=%s (%s) stack=%s", sourceWorkerID, sourceHostname, stackID)
		teardownOutput, teardownErr := compose.RunDown(ctx, compose.RunOptions{
			WorkDir:     workDir,
			ComposeFile: composeFilePath,
		})
		outputBuf.WriteString(teardownOutput)
		if teardownErr != nil {
			log.Printf("[transfer] step 2/2: teardown error source_agent=%s: %v — containers may be orphaned", sourceWorkerID, teardownErr)
			fmt.Fprintf(&outputBuf, "teardown failed: %v — containers may be orphaned.\n", teardownErr)
		} else {
			log.Printf("[transfer] step 2/2: teardown done source_agent=%s", sourceWorkerID)
			fmt.Fprintf(&outputBuf, "teardown on %s: done.\n", sourceHostname)
		}
	} else if sourceWorkerID != "" && r.dispatcher.IsConnected(sourceWorkerID) {
		log.Printf("[transfer] step 2/2: teardown dispatching to source_agent=%s (%s) stack=%s", sourceWorkerID, sourceHostname, stackID)
		teardownResult, teardownErr := r.dispatcher.Dispatch(ctx, sourceWorkerID, protocol.TeardownCommand{
			CommandID:      fmt.Sprintf("teardown-transfer-%s", stackID),
			StackID:        stackID,
			ComposeFileB64: composeB64,
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

	// --- Step 3: Update stack record to point to the new agent ---
	stack.Set("worker", targetWorkerID)
	stack.Set("status", "active")
	stack.Set("last_synced_at", time.Now().UTC().Format(time.RFC3339))
	_ = r.app.Save(stack)

	if targetFingerprint == "embedded" || (r.dispatcher != nil && r.dispatcher.IsEmbedded(targetWorkerID)) {
		r.refreshServiceStatus(ctx, stackID, workDir)
	}

	if syncLog != nil {
		r.updateSyncLog(syncLog.Id, "done", outputBuf.String(), duration)
	}

	r.notifier.Dispatch(ctx, notify.Payload{
		Event:      notify.SyncDone,
		StackID:    stackID,
		StackName:  stack.GetString("name"),
		SyncLogID:  syncLogID,
		Trigger:    "transfer",
		DurationMs: duration,
	})

	log.Printf("[transfer] DONE stack=%s source_agent=%s target_agent=%s elapsed=%dms", stackID, sourceWorkerID, targetWorkerID, duration)
	return nil
}
