// Package jobscheduler manages cron-based execution of one-shot Docker jobs.
// All job configuration is read from job.yaml files committed to git repositories;
// the database stores only a thin reference and runtime state.
package jobscheduler

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	gosync "sync"
	"time"

	"hash/fnv"

	gogit "github.com/go-git/go-git/v5"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/robfig/cron/v3"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/job"
	"github.com/wireops/wireops/internal/protocol"
)

// WorkerDispatcher is the subset of worker.MTLSServer used by the scheduler.
type WorkerDispatcher interface {
	GetWorkersByTags(tags []string) []string
	IsConnected(workerID string) bool
	Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error)
}

// jobRunParams bundles repository/job metadata needed by dispatchToWorker.
// Grouping these avoids exceeding the per-function parameter limit.
type jobRunParams struct {
	repoID     string
	repoBranch string
	jobFile    string
	commitSHA  string
	def        *job.Definition
	envMap     map[string]string
}

// Scheduler manages cron entries for scheduled jobs and dispatches them to workers.
type Scheduler struct {
	app        core.App
	dispatcher WorkerDispatcher
	dataDir    string
	secretKey  []byte

	cron    *cron.Cron
	mu      gosync.Mutex
	entries map[string]cron.EntryID // jobID → cron entry ID

	rootCtx    context.Context
	rootCancel context.CancelFunc
}

// NewScheduler creates a Scheduler. dataDir is the PocketBase data directory
// (used to locate the cloned repositories workspace).
func NewScheduler(app core.App, dispatcher WorkerDispatcher, dataDir string) *Scheduler {
	rootCtx, rootCancel := context.WithCancel(context.Background())
	return &Scheduler{
		app:        app,
		dispatcher: dispatcher,
		dataDir:    dataDir,
		secretKey:  []byte(os.Getenv("SECRET_KEY")),
		cron:       cron.New(),
		entries:    make(map[string]cron.EntryID),
		rootCtx:    rootCtx,
		rootCancel: rootCancel,
	}
}

// Start loads all enabled jobs from the database and registers their cron entries.
func (s *Scheduler) Start() {
	jobs, err := s.app.FindAllRecords("scheduled_jobs", dbx.HashExp{"enabled": true})
	if err != nil {
		log.Printf("[jobscheduler] failed to load jobs on start: %v", err)
		return
	}
	for _, rec := range jobs {
		s.RegisterJob(rec.Id)
	}
	s.cron.Start()
	log.Printf("[jobscheduler] started with %d job(s)", len(jobs))
}

// Shutdown stops the cron runner and cancels all in-flight executions.
func (s *Scheduler) Shutdown() {
	s.cron.Stop()
	s.rootCancel()
	log.Printf("[jobscheduler] shutdown")
}

// RegisterJob reads the job.yaml to get the cron expression, then replaces any
// existing cron entry for this job with a fresh one. Safe to call on create or update.
func (s *Scheduler) RegisterJob(jobID string) {
	rec, err := s.app.FindRecordById("scheduled_jobs", jobID)
	if err != nil {
		log.Printf("[jobscheduler] RegisterJob: job %s not found: %v", jobID, err)
		return
	}

	s.mu.Lock()
	// Remove old entry if present
	if old, ok := s.entries[jobID]; ok {
		s.cron.Remove(old)
		delete(s.entries, jobID)
	}
	s.mu.Unlock()

	if !rec.GetBool("enabled") {
		return
	}

	repoWorkspace := filepath.Join(s.dataDir, "repositories")
	repoID := rec.GetString("repository")
	jobFile := rec.GetString("job_file")

	def, err := job.ParseJobFile(repoWorkspace, repoID, jobFile)
	if err != nil {
		log.Printf("[jobscheduler] RegisterJob: cannot parse job.yaml for job %s: %v", jobID, err)
		return
	}

	entryID, err := s.cron.AddFunc(def.Cron, func() {
		s.executeJob(jobID, "cron", "system")
	})
	if err != nil {
		log.Printf("[jobscheduler] RegisterJob: invalid cron %q for job %s: %v", def.Cron, jobID, err)
		return
	}

	s.mu.Lock()
	s.entries[jobID] = entryID
	s.mu.Unlock()

	log.Printf("[jobscheduler] registered job %s (cron=%q title=%q)", jobID, def.Cron, def.Title)
}

// UnregisterJob removes the cron entry for the given job.
func (s *Scheduler) UnregisterJob(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.entries[jobID]; ok {
		s.cron.Remove(id)
		delete(s.entries, jobID)
		log.Printf("[jobscheduler] unregistered job %s", jobID)
	}
}

// SyncJobsForRepo syncs all jobs pointing to the given repository.
// Called when a repository is git-pulled so the scheduler picks up updated cron expressions.
func (s *Scheduler) SyncJobsForRepo(repoID string) {
	records, err := s.app.FindAllRecords("scheduled_jobs", dbx.HashExp{"repository": repoID})
	if err != nil {
		log.Printf("[jobscheduler] SyncJobsForRepo: query failed for repo %s: %v", repoID, err)
		return
	}
	for _, rec := range records {
		s.RegisterJob(rec.Id)
	}
	if len(records) > 0 {
		log.Printf("[jobscheduler] synced %d job(s) after repo %s update", len(records), repoID)
	}
}

// TriggerManual fires an immediate execution of the job outside its cron schedule.
func (s *Scheduler) TriggerManual(jobID string, userID string) {
	go s.executeJob(jobID, "manual", userID)
}

// CancelRun stops a running job container on the assigned remote worker.
func (s *Scheduler) CancelRun(runID string) error {
	rec, err := s.app.FindRecordById("job_runs", runID)
	if err != nil {
		return fmt.Errorf("job run not found: %w", err)
	}
	if rec.GetString("status") != "running" {
		return fmt.Errorf("job run is not running (status: %s)", rec.GetString("status"))
	}
	workerID := rec.GetString("worker")
	if workerID == "" {
		return fmt.Errorf("job run has no worker")
	}

	cmd := protocol.KillJobCommand{
		CommandID: fmt.Sprintf("kill-%s", runID),
		JobRunID:  runID,
	}
	ctx, cancel := context.WithTimeout(s.rootCtx, 15*time.Second)
	defer cancel()
	result, err := s.dispatcher.Dispatch(ctx, workerID, cmd)
	if err != nil {
		return fmt.Errorf("dispatch kill failed: %w", err)
	}
	if result.Error != "" {
		return fmt.Errorf("worker error: %s", result.Error)
	}
	log.Printf("[jobscheduler] cancelled remote run %s on worker %s", runID, workerID)
	return nil
}

func (s *Scheduler) executeJob(jobID, trigger string, userID string) {
	ctx := context.WithValue(s.rootCtx, "userID", userID)
	if ctx.Err() != nil {
		return
	}

	rec, err := s.app.FindRecordById("scheduled_jobs", jobID)
	if err != nil {
		log.Printf("[jobscheduler] executeJob: job %s not found: %v", jobID, err)
		return
	}

	repoWorkspace := filepath.Join(s.dataDir, "repositories")
	repoID := rec.GetString("repository")
	jobFile := rec.GetString("job_file")

	def, err := job.ParseJobFile(repoWorkspace, repoID, jobFile)
	if err != nil {
		msg := fmt.Sprintf("cannot parse job.yaml %s for job %s: %v", jobFile, jobID, err)
		log.Printf("[jobscheduler] executeJob: %s", msg)
		if _, saveErr := s.createJobRun(jobID, "", trigger, "error", msg); saveErr != nil {
			log.Printf("[jobscheduler] executeJob: failed to persist definition error job=%s: %v", jobID, saveErr)
		}
		if saveErr := s.setScheduledJobStatus(jobID, "error"); saveErr != nil {
			log.Printf("[jobscheduler] executeJob: failed to mark job error job=%s: %v", jobID, saveErr)
		}
		return
	}

	workers := s.dispatcher.GetWorkersByTags(def.Tags)
	if len(workers) == 0 {
		if err := s.createStalledRun(jobID, trigger, def.Tags); err != nil {
			log.Printf("[jobscheduler] executeJob: failed to persist stalled job=%s: %v", jobID, err)
		}
		return
	}

	envMap, err := s.loadEnvVars(jobID)
	if err != nil {
		msg := fmt.Sprintf("cannot load env vars for job %s: %v", jobID, err)
		log.Printf("[jobscheduler] executeJob: %s", msg)
		if _, saveErr := s.createJobRun(jobID, "", trigger, "error", msg); saveErr != nil {
			log.Printf("[jobscheduler] executeJob: failed to persist env error job=%s: %v", jobID, saveErr)
		}
		if saveErr := s.setScheduledJobStatus(jobID, "error"); saveErr != nil {
			log.Printf("[jobscheduler] executeJob: failed to mark job error job=%s: %v", jobID, saveErr)
		}
		return
	}

	repoBranch := func() string {
		rec, _ := s.app.FindRecordById("repositories", repoID)
		if rec != nil {
			return rec.GetString("branch")
		}
		return ""
	}()
	commitSHA := s.repoHeadSHA(repoID)

	params := jobRunParams{
		repoID:     repoID,
		repoBranch: repoBranch,
		jobFile:    jobFile,
		commitSHA:  commitSHA,
		def:        def,
		envMap:     envMap,
	}

	switch def.Mode {
	case job.ModeOnceAll:
		for _, workerID := range workers {
			workerID := workerID
			go s.dispatchToWorker(ctx, jobID, trigger, workerID, params)
		}
	default: // ModeOnce
		workerID := s.pickWorker(jobID, workers)
		go s.dispatchToWorker(ctx, jobID, trigger, workerID, params)
	}

	// Update last_run_at and status immediately; run completion is async.
	rec.Set("last_run_at", time.Now())
	if rec.GetString("status") == "stalled" || rec.GetString("status") == "error" {
		rec.Set("status", "active")
	}
	if err := s.app.Save(rec); err != nil {
		log.Printf("[jobscheduler] executeJob: failed to persist last_run_at job=%s: %v", jobID, err)
	}
}

// dispatchToWorker creates a job_run record and sends RunJobCommand to the worker.
// It only waits for the start ack (≤30s), not job completion.
// Completion is delivered via HandleJobCompleted when the worker pushes MsgJobCompleted.
func (s *Scheduler) dispatchToWorker(ctx context.Context, jobID, trigger, workerID string, p jobRunParams) {
	runID, err := s.createJobRun(jobID, workerID, trigger, "pending")
	if err != nil {
		log.Printf("[jobscheduler] dispatchToWorker: failed to create job_run job=%s worker=%s: %v", jobID, workerID, err)
		return
	}

	containerName := "wireops-job-" + runID
	if err := s.patchJobRunMeta(runID, containerName, p.commitSHA); err != nil {
		log.Printf("[jobscheduler] dispatchToWorker: failed to persist metadata job=%s run=%s: %v", jobID, runID, err)
		if updateErr := s.updateJobRun(runID, "error", err.Error(), 0); updateErr != nil {
			log.Printf("[jobscheduler] dispatchToWorker: failed to mark metadata error run=%s: %v", runID, updateErr)
		}
		return
	}

	var timeoutSecs int
	if p.def.Resources.Timeout != "" {
		d, err := time.ParseDuration(p.def.Resources.Timeout)
		if err == nil {
			timeoutSecs = int(d.Seconds())
		}
	}

	cmd := protocol.RunJobCommand{
		CommandID:        fmt.Sprintf("job-%s", runID),
		JobRunID:         runID,
		JobName:          p.def.Title,
		Image:            p.def.Image,
		Command:          []string(p.def.Command),
		Env:              p.envMap,
		RepositoryID:     p.repoID,
		RepositoryBranch: p.repoBranch,
		RepositoryFile:   p.jobFile,
		CommitSHA:        p.commitSHA,
		Volumes:          p.def.Volumes,
		Network:          p.def.Network,
		CPUs:             p.def.Resources.CPU,
		MemoryLimit:      p.def.Resources.Memory,
		TimeoutSeconds:   timeoutSecs,
	}

	// Remote worker: wait only for the start ack (worker immediately returns "started").
	// Actual completion arrives later via HandleJobCompleted.
	ackCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, dispatchErr := s.dispatcher.Dispatch(ackCtx, workerID, cmd)
	if dispatchErr != nil {
		log.Printf("[jobscheduler] dispatchToWorker: ack error job=%s run=%s: %v", jobID, runID, dispatchErr)
		if err := s.updateJobRun(runID, "error", dispatchErr.Error(), 0); err != nil {
			log.Printf("[jobscheduler] dispatchToWorker: failed to persist ack error run=%s: %v", runID, err)
		}
		return
	}
	if result.Error != "" {
		log.Printf("[jobscheduler] dispatchToWorker: worker error job=%s run=%s: %s", jobID, runID, result.Error)
		if err := s.updateJobRun(runID, "error", result.Error, 0); err != nil {
			log.Printf("[jobscheduler] dispatchToWorker: failed to persist worker error run=%s: %v", runID, err)
		}
		return
	}

	// Ack received — container is starting.
	if err := s.setJobRunStatus(runID, "running"); err != nil {
		log.Printf("[jobscheduler] dispatchToWorker: failed to persist running status run=%s worker=%s: %v", runID, workerID, err)
		killCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		killResult, killErr := s.dispatcher.Dispatch(killCtx, workerID, protocol.KillJobCommand{
			CommandID: fmt.Sprintf("kill-%s", runID),
			JobRunID:  runID,
		})
		if killErr != nil || killResult.Error != "" {
			log.Printf("[jobscheduler] dispatchToWorker: best-effort kill failed run=%s worker=%s error=%v worker_error=%s", runID, workerID, killErr, killResult.Error)
		}
		return
	}
	log.Printf("[jobscheduler] job run=%s worker=%s job=%s started", runID, workerID, jobID)
}

// HandleJobCompleted is called by the MTLSServer when a remote worker pushes
// a MsgJobCompleted message. It updates the job_run record with the final result.
func (s *Scheduler) HandleJobCompleted(msg protocol.JobCompletedMessage) {
	status := "success"
	if !msg.Success {
		status = "error"
	}
	if err := s.updateJobRun(msg.JobRunID, status, msg.Output, msg.DurationMs); err != nil {
		log.Printf("[jobscheduler] HandleJobCompleted: failed to persist completion run=%s status=%s: %v", msg.JobRunID, status, err)
		return
	}
	log.Printf("[jobscheduler] job_completed run=%s status=%s elapsed=%dms", msg.JobRunID, status, msg.DurationMs)
}

// maxJobRunDuration is the wall-clock ceiling for a single job run.
// Any run still in "running" after this duration is considered lost.
const maxJobRunDuration = time.Hour

// maxJobRunPendingDuration is the time limit for a job to remain in "pending" status.
// If it remains "pending" longer than this, it is assumed the dispatch failed.
const maxJobRunPendingDuration = 15 * time.Minute

// MarkForgottenRuns finds every job_run that has been in "running" state for
// longer than maxJobRunDuration and marks it as "forgotten". It also finds
// any job_run stuck in "pending" for more than maxJobRunPendingDuration
// and marks it as "error".
func (s *Scheduler) MarkForgottenRuns() error {
	var firstErr error

	if err := s.reconcileRunningRuns(&firstErr); err != nil {
		return err
	}

	s.reconcilePendingRuns(&firstErr)

	return firstErr
}

func (s *Scheduler) reconcileRunningRuns(firstErr *error) error {
	runningCutoff := time.Now().Add(-maxJobRunDuration)
	runningRecords, err := s.app.FindAllRecords("job_runs",
		dbx.HashExp{"status": "running"},
	)
	if err != nil {
		return fmt.Errorf("MarkForgottenRuns running query failed: %w", err)
	}

	for _, rec := range runningRecords {
		s.reconcileSingleRunningRun(rec, runningCutoff, firstErr)
	}
	return nil
}

func (s *Scheduler) reconcileSingleRunningRun(rec *core.Record, cutoff time.Time, firstErr *error) {
	if !rec.GetDateTime("updated").Time().Before(cutoff) {
		return
	}
	rec.Set("status", "forgotten")
	rec.Set("output", "job forgotten: still running after 1 hour with no completion signal")
	if err := s.app.Save(rec); err != nil {
		log.Printf("[jobscheduler] MarkForgottenRuns: failed to save running run %s: %v", rec.Id, err)
		if *firstErr == nil {
			*firstErr = fmt.Errorf("mark forgotten running run=%s: %w", rec.Id, err)
		}
	} else {
		log.Printf("[jobscheduler] run %s marked forgotten (started >1h ago)", rec.Id)
	}
}

func (s *Scheduler) reconcilePendingRuns(firstErr *error) {
	pendingCutoff := time.Now().Add(-maxJobRunPendingDuration)
	pendingRecords, err := s.app.FindAllRecords("job_runs",
		dbx.HashExp{"status": "pending"},
	)
	if err != nil {
		if *firstErr == nil {
			*firstErr = fmt.Errorf("MarkForgottenRuns pending query failed: %w", err)
		}
		return
	}

	for _, rec := range pendingRecords {
		s.reconcileSinglePendingRun(rec, pendingCutoff, firstErr)
	}
}

func (s *Scheduler) reconcileSinglePendingRun(rec *core.Record, cutoff time.Time, firstErr *error) {
	if !rec.GetDateTime("updated").Time().Before(cutoff) {
		return
	}
	rec.Set("status", "error")
	rec.Set("output", "job failed: stuck in pending for more than 15 minutes (failed to dispatch to worker)")
	if err := s.app.Save(rec); err != nil {
		log.Printf("[jobscheduler] MarkForgottenRuns: failed to save pending run %s: %v", rec.Id, err)
		if *firstErr == nil {
			*firstErr = fmt.Errorf("mark error pending run=%s: %w", rec.Id, err)
		}
	} else {
		log.Printf("[jobscheduler] run %s marked error (stuck in pending >15m)", rec.Id)
	}
}

// ReconcileActiveJobs is called when a worker connects or sends a heartbeat.
// It checks all job_runs that are currently in "running" status for this worker.
// If a job run is NOT in the worker's active list, it must have finished/terminated
// while the connection was offline. If the job run was updated more than 1 minute ago,
// we mark it as "error" (since we missed its completion message).
func (s *Scheduler) ReconcileActiveJobs(workerID string, activeIDs []string) error {
	records, err := s.app.FindAllRecords("job_runs", dbx.HashExp{
		"worker": workerID,
		"status": "running",
	})
	if err != nil {
		return fmt.Errorf("ReconcileActiveJobs query failed worker=%s: %w", workerID, err)
	}

	activeSet := make(map[string]bool, len(activeIDs))
	for _, id := range activeIDs {
		activeSet[id] = true
	}

	cutoff := time.Now().Add(-1 * time.Minute)
	var firstErr error

	for _, rec := range records {
		runID := rec.Id
		if activeSet[runID] {
			// Job is still running on the worker, leave it alone
			continue
		}

		// If the job run was updated/started very recently, don't mark it as error yet
		// (allows time for the worker to receive the dispatch and register the container)
		if rec.GetDateTime("updated").Time().After(cutoff) {
			continue
		}

		rec.Set("status", "error")
		rec.Set("output", "job lost: worker disconnected and job is no longer running")
		if err := s.app.Save(rec); err != nil {
			log.Printf("[jobscheduler] ReconcileActiveJobs: failed to save run %s worker=%s: %v", rec.Id, workerID, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("reconcile run=%s worker=%s: %w", rec.Id, workerID, err)
			}
		} else {
			log.Printf("[jobscheduler] run %s marked error (not found in worker %s active list)", rec.Id, workerID)
		}
	}
	return firstErr
}

// createStalledRun writes a job_run with status=stalled when no workers are available.
// It also updates the scheduled_jobs record's status to "stalled".
func (s *Scheduler) createStalledRun(jobID, trigger string, tags []string) error {
	reason := "no matching workers available for the specified tags"
	if len(tags) > 0 {
		reason = fmt.Sprintf("no matching workers available for the required tags: %v", tags)
	}
	runID, err := s.createJobRun(jobID, "", trigger, "stalled", reason)
	if err != nil {
		return fmt.Errorf("create stalled run job=%s: %w", jobID, err)
	}
	log.Printf("[jobscheduler] job job=%s run=%s stalled (no matching workers)", jobID, runID)

	rec, err := s.app.FindRecordById("scheduled_jobs", jobID)
	if err != nil {
		return fmt.Errorf("create stalled run load job=%s: %w", jobID, err)
	}
	if rec.GetString("status") != "paused" {
		rec.Set("status", "stalled")
		if err := s.app.Save(rec); err != nil {
			return fmt.Errorf("create stalled run update job=%s status=stalled: %w", jobID, err)
		}
	}
	return nil
}

// createJobRun inserts a job_run record and returns its ID.
func (s *Scheduler) createJobRun(jobID, workerID, trigger, status string, output ...string) (string, error) {
	col, err := s.app.FindCollectionByNameOrId("job_runs")
	if err != nil {
		return "", err
	}
	rec := core.NewRecord(col)
	rec.Set("job", jobID)
	rec.Set("trigger", trigger)
	rec.Set("status", status)
	if len(output) > 0 {
		rec.Set("output", output[0])
	}
	rec.Set("expires_at", time.Now().AddDate(0, 0, 30))
	if workerID != "" {
		rec.Set("worker", workerID)
	}
	if err := s.app.Save(rec); err != nil {
		return "", err
	}
	return rec.Id, nil
}

func (s *Scheduler) setScheduledJobStatus(jobID, status string) error {
	rec, err := s.app.FindRecordById("scheduled_jobs", jobID)
	if err != nil {
		return fmt.Errorf("set scheduled job status job=%s status=%s: %w", jobID, status, err)
	}
	if rec.GetString("status") == "paused" {
		return nil
	}
	rec.Set("status", status)
	if err := s.app.Save(rec); err != nil {
		return fmt.Errorf("set scheduled job status job=%s status=%s: %w", jobID, status, err)
	}
	return nil
}

// setJobRunStatus updates only the status field of a job_run record.
func (s *Scheduler) setJobRunStatus(runID, status string) error {
	rec, err := s.app.FindRecordById("job_runs", runID)
	if err != nil {
		return fmt.Errorf("set job_run status run=%s status=%s: %w", runID, status, err)
	}
	rec.Set("status", status)
	if err := s.app.Save(rec); err != nil {
		return fmt.Errorf("set job_run status run=%s status=%s: %w", runID, status, err)
	}
	return nil
}

// updateJobRun sets the final status, output, and duration on a job_run record.
func (s *Scheduler) updateJobRun(runID, status, output string, durationMs int64) error {
	rec, err := s.app.FindRecordById("job_runs", runID)
	if err != nil {
		return fmt.Errorf("update job_run run=%s status=%s: %w", runID, status, err)
	}
	rec.Set("status", status)
	rec.Set("output", output)
	rec.Set("duration_ms", durationMs)
	if err := s.app.Save(rec); err != nil {
		return fmt.Errorf("update job_run run=%s status=%s: %w", runID, status, err)
	}
	return nil
}

// loadEnvVars fetches and decrypts job_env_vars for the given job.
func (s *Scheduler) loadEnvVars(jobID string) (map[string]string, error) {
	records, err := s.app.FindAllRecords("job_env_vars", dbx.HashExp{"job": jobID})
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(records))
	for _, rec := range records {
		key := rec.GetString("key")
		val := rec.GetString("value")
		if rec.GetBool("secret") && len(s.secretKey) == 32 {
			dec, err := crypto.Decrypt(val, s.secretKey)
			if err != nil {
				log.Printf("[jobscheduler] loadEnvVars: failed to decrypt env var %q for job %s: %v", key, jobID, err)
				continue
			}
			val = string(dec)
		}
		result[key] = val
	}
	return result, nil
}

// repoHeadSHA returns the local HEAD commit SHA for the given repository.
// Returns an empty string if the repo hasn't been cloned or the HEAD can't be read.
func (s *Scheduler) repoHeadSHA(repoID string) string {
	if repoID == "" {
		return ""
	}
	repoPath := filepath.Join(s.dataDir, "repositories", repoID)
	repo, err := gogit.PlainOpen(repoPath)
	if err != nil {
		return ""
	}
	sha, err := git.LocalHeadSHA(repo)
	if err != nil {
		return ""
	}
	return sha
}

// patchJobRunMeta stores the container name and commit SHA on an existing job_run record.
func (s *Scheduler) patchJobRunMeta(runID, containerName, commitSHA string) error {
	rec, err := s.app.FindRecordById("job_runs", runID)
	if err != nil {
		return fmt.Errorf("patch job_run metadata run=%s: %w", runID, err)
	}
	rec.Set("container_name", containerName)
	rec.Set("commit_sha", commitSHA)
	if err := s.app.Save(rec); err != nil {
		return fmt.Errorf("patch job_run metadata run=%s: %w", runID, err)
	}
	return nil
}

// pickWorker selects one worker from the list deterministically by hashing jobID.
// Different job IDs distribute across the worker pool; the selection is stable
// for a given jobID regardless of when it is called.
func (s *Scheduler) pickWorker(jobID string, workers []string) string {
	if len(workers) == 1 {
		return workers[0]
	}
	h := fnv.New32a()
	h.Write([]byte(jobID))
	idx := int(h.Sum32()) % len(workers)
	return workers[idx]
}
