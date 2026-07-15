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
	"strings"
	gosync "sync"
	"time"
	"unicode"

	"hash/fnv"

	gogit "github.com/go-git/go-git/v5"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/robfig/cron/v3"

	"github.com/wireops/wireops/internal/audit"
	"github.com/wireops/wireops/internal/contextutil"
	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/envvars"
	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/job"
	"github.com/wireops/wireops/internal/policy"
	"github.com/wireops/wireops/internal/protocol"
	"github.com/wireops/wireops/internal/secrets"
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
	app             core.App
	dispatcher      WorkerDispatcher
	repoWorkspace   string
	secretsRegistry *secrets.Registry

	cron    *cron.Cron
	mu      gosync.Mutex            // protects entries map
	runMu   gosync.Mutex            // protects against read-modify-write status race conditions on job_runs
	entries map[string]cron.EntryID // jobID → cron entry ID

	rootCtx    context.Context
	rootCancel context.CancelFunc
}

// NewScheduler creates a Scheduler. repoWorkspace is the base path used to
// locate cloned repositories and job definitions.
func NewScheduler(app core.App, dispatcher WorkerDispatcher, repoWorkspace string) *Scheduler {
	rootCtx, rootCancel := context.WithCancel(context.Background())
	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	return &Scheduler{
		app:             app,
		dispatcher:      dispatcher,
		repoWorkspace:   repoWorkspace,
		secretsRegistry: secrets.NewDefaultRegistry(app, secretKey),
		entries:         make(map[string]cron.EntryID),
		rootCtx:         rootCtx,
		rootCancel:      rootCancel,
	}
}

// Start loads all enabled jobs from the database and registers their cron entries.
func (s *Scheduler) Start() {
	var loc *time.Location
	settings, err := s.app.FindAllRecords("app_settings")
	if err == nil && len(settings) > 0 {
		if tz := settings[0].GetString("timezone"); tz != "" {
			if parsedLoc, err := time.LoadLocation(tz); err == nil {
				loc = parsedLoc
			} else {
				log.Printf("[jobscheduler] invalid timezone %q in settings, falling back to local time: %v", tz, err)
			}
		}
	}

	if loc != nil {
		s.cron = cron.New(cron.WithLocation(loc))
	} else {
		s.cron = cron.New()
	}

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

func (s *Scheduler) Shutdown() {
	if s.cron != nil {
		s.cron.Stop()
	}
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

	repoID := rec.GetString("repository")
	jobFile := rec.GetString("job_file")

	def, err := job.ParseJobFile(s.repoWorkspace, repoID, jobFile)
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

	log.Printf("[jobscheduler] registered job %s (cron=%q name=%q)", jobID, def.Cron, def.Name)
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
	ctx := contextutil.WithUserID(s.rootCtx, userID)
	if ctx.Err() != nil {
		return
	}

	rec, err := s.app.FindRecordById("scheduled_jobs", jobID)
	if err != nil {
		log.Printf("[jobscheduler] executeJob: job %s not found: %v", jobID, err)
		return
	}

	// Fast pre-flight gate: reject immediately if a referenced vault/infisical
	// backend is disabled, before parsing job.yaml or picking a worker —
	// otherwise this is only discovered later inside loadEnvVars.
	if err := envvars.CheckJobSecretBackends(s.app, jobID); err != nil {
		msg := fmt.Sprintf("secret backend unavailable for job %s: %v", jobID, err)
		log.Printf("[jobscheduler] executeJob: %s", msg)
		if _, saveErr := s.createJobRun(jobID, "", trigger, "error", msg); saveErr != nil {
			log.Printf("[jobscheduler] executeJob: failed to persist secret backend error job=%s: %v", jobID, saveErr)
		}
		if saveErr := s.setScheduledJobStatus(jobID, "error"); saveErr != nil {
			log.Printf("[jobscheduler] executeJob: failed to mark job error job=%s: %v", jobID, saveErr)
		}
		return
	}

	repoID := rec.GetString("repository")
	jobFile := rec.GetString("job_file")

	def, err := job.ParseJobFile(s.repoWorkspace, repoID, jobFile)
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

	s.ensureScheduledJobDisplayFields(rec, def)

	workers := s.dispatcher.GetWorkersByTags(def.Tags)
	if len(workers) == 0 {
		if err := s.createStalledRun(jobID, trigger, def.Tags, def); err != nil {
			log.Printf("[jobscheduler] executeJob: failed to persist stalled job=%s: %v", jobID, err)
		}
		return
	}

	envMap, err := s.loadEnvVars(ctx, jobID)
	if err != nil {
		// Detail (err) may originate from secret-provider resolution (decrypt
		// failures, Vault/Infisical errors) — keep it out of process logs and
		// persist it only to the job_run record, which is access-controlled.
		log.Printf("[jobscheduler] executeJob: cannot load env vars for job %s", jobID)
		msg := fmt.Sprintf("cannot load env vars for job %s: %v", jobID, err)
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
		if updateErr := s.updateJobRun(runID, "error", err.Error(), 0, 0, 0); updateErr != nil {
			log.Printf("[jobscheduler] dispatchToWorker: failed to mark metadata error run=%s: %v", runID, updateErr)
		}
		return
	}

	// --- Policy enforcement ---
	// Load the effective policy for this worker and validate the job parameters
	// before dispatching. Violations are hard errors (fail-closed).
	wp, policyErr := policy.Load(s.app, workerID)
	if policyErr != nil {
		log.Printf("[jobscheduler] dispatchToWorker: failed to load policy job=%s run=%s worker=%s: %v", jobID, runID, workerID, policyErr)
		if updateErr := s.updateJobRun(runID, "error", fmt.Sprintf("policy load error: %v", policyErr), 0, 0, 0); updateErr != nil {
			log.Printf("[jobscheduler] dispatchToWorker: failed to persist policy load error run=%s: %v", runID, updateErr)
		}
		return
	}
	if err := wp.ValidateImages([]string{p.def.Image}); err != nil {
		msg := fmt.Sprintf("policy violation: %v", err)
		log.Printf("[jobscheduler] dispatchToWorker: %s job=%s run=%s worker=%s", msg, jobID, runID, workerID)
		if updateErr := s.updateJobRun(runID, "error", msg, 0, 0, 0); updateErr != nil {
			log.Printf("[jobscheduler] dispatchToWorker: failed to persist image policy error run=%s: %v", runID, updateErr)
		}
		return
	}
	if err := wp.ValidateVolumes(p.def.Volumes); err != nil {
		msg := fmt.Sprintf("policy violation: %v", err)
		log.Printf("[jobscheduler] dispatchToWorker: %s job=%s run=%s worker=%s", msg, jobID, runID, workerID)
		if updateErr := s.updateJobRun(runID, "error", msg, 0, 0, 0); updateErr != nil {
			log.Printf("[jobscheduler] dispatchToWorker: failed to persist volume policy error run=%s: %v", runID, updateErr)
		}
		return
	}
	if err := wp.ValidateNetwork(p.def.Network); err != nil {
		msg := fmt.Sprintf("policy violation: %v", err)
		log.Printf("[jobscheduler] dispatchToWorker: %s job=%s run=%s worker=%s", msg, jobID, runID, workerID)
		if updateErr := s.updateJobRun(runID, "error", msg, 0, 0, 0); updateErr != nil {
			log.Printf("[jobscheduler] dispatchToWorker: failed to persist network policy error run=%s: %v", runID, updateErr)
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

	dispatchStart := time.Now()

	cmd := protocol.RunJobCommand{
		CommandID:        fmt.Sprintf("job-%s", runID),
		JobRunID:         runID,
		JobName:          p.def.Name,
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
		DispatchedAt:     dispatchStart.UnixMilli(),
	}

	// Remote worker: wait only for the start ack (worker immediately returns "started").
	// Actual completion arrives later via HandleJobCompleted.
	ackCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, dispatchErr := s.dispatcher.Dispatch(ackCtx, workerID, cmd)
	if dispatchErr != nil {
		log.Printf("[jobscheduler] dispatchToWorker: ack error job=%s run=%s: %v", jobID, runID, dispatchErr)
		if err := s.updateJobRun(runID, "error", dispatchErr.Error(), 0, 0, 0); err != nil {
			log.Printf("[jobscheduler] dispatchToWorker: failed to persist ack error run=%s: %v", runID, err)
		}
		return
	}
	if result.Error != "" {
		log.Printf("[jobscheduler] dispatchToWorker: worker error job=%s run=%s: %s", jobID, runID, result.Error)
		if err := s.updateJobRun(runID, "error", result.Error, 0, 0, 0); err != nil {
			log.Printf("[jobscheduler] dispatchToWorker: failed to persist worker error run=%s: %v", runID, err)
		}
		return
	}

	// Ack received — container is starting.
	if err := s.setJobRunStarted(runID); err != nil {
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
	if err := s.updateJobRun(msg.JobRunID, status, msg.Output, msg.DurationMs, msg.QueueTimeMs, msg.ExecutionTimeMs); err != nil {
		log.Printf("[jobscheduler] HandleJobCompleted: failed to persist completion run=%s status=%s: %v", msg.JobRunID, status, err)
		return
	}
	if !msg.Success {
		log.Printf("[jobscheduler] job_completed run=%s status=error: %s", msg.JobRunID, msg.Output)
	} else {
		log.Printf("[jobscheduler] job_completed run=%s status=success elapsed=%dms", msg.JobRunID, msg.DurationMs)
	}
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
	s.runMu.Lock()
	defer s.runMu.Unlock()

	// Re-fetch to prevent overwriting concurrent completion
	freshRec, err := s.app.FindRecordById("job_runs", rec.Id)
	if err != nil {
		return
	}
	if freshRec.GetString("status") != "running" {
		return
	}
	rec = freshRec

	rec.Set("status", "forgotten")
	rec.Set("output", "job forgotten: still running after 1 hour with no completion signal")

	timeoutMs := s.getJobTimeoutMs(rec)
	rec.Set("execution_time_ms", timeoutMs)

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
	s.runMu.Lock()
	defer s.runMu.Unlock()

	// Re-fetch to prevent overwriting concurrent start/completion
	freshRec, err := s.app.FindRecordById("job_runs", rec.Id)
	if err != nil {
		return
	}
	if freshRec.GetString("status") != "pending" {
		return
	}
	rec = freshRec

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

		s.runMu.Lock()
		// Re-fetch to prevent overwriting concurrent completion
		freshRec, err := s.app.FindRecordById("job_runs", runID)
		if err != nil {
			s.runMu.Unlock()
			continue
		}
		if freshRec.GetString("status") != "running" {
			s.runMu.Unlock()
			continue
		}
		rec = freshRec

		rec.Set("status", "error")
		rec.Set("output", "job lost: worker disconnected and job is no longer running")
		timeoutMs := s.getJobTimeoutMs(rec)
		rec.Set("execution_time_ms", timeoutMs)
		if err := s.app.Save(rec); err != nil {
			log.Printf("[jobscheduler] ReconcileActiveJobs: failed to save run %s worker=%s: %v", rec.Id, workerID, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("reconcile run=%s worker=%s: %w", rec.Id, workerID, err)
			}
		} else {
			log.Printf("[jobscheduler] run %s marked error (not found in worker %s active list)", rec.Id, workerID)
		}
		s.runMu.Unlock()
	}
	return firstErr
}

// createStalledRun writes a job_run with status=stalled when no workers are available.
// It also updates the scheduled_jobs record's status to "stalled".
func (s *Scheduler) createStalledRun(jobID, trigger string, tags []string, def *job.Definition) error {
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
		s.ensureScheduledJobDisplayFields(rec, def)
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
	rec.Set("expires_at", time.Now().AddDate(0, 0, audit.JobRunRetentionDays(s.app)))
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
	s.ensureScheduledJobDisplayFields(rec, nil)
	rec.Set("status", status)
	if err := s.app.Save(rec); err != nil {
		return fmt.Errorf("set scheduled job status job=%s status=%s: %w", jobID, status, err)
	}
	return nil
}

// ensureScheduledJobDisplayFields populates the display fields (name, description) for a scheduled job.
// Git is the source of truth, so if a job.Definition is provided, its fields will always overwrite
// the current database values.
func (s *Scheduler) ensureScheduledJobDisplayFields(rec *core.Record, def *job.Definition) {
	if def != nil {
		// Git is the source of truth: sync from job.yaml
		name := def.Name
		if name == "" {
			name = fallbackScheduledJobName(rec.GetString("job_file"), rec.Id)
		}
		rec.Set("name", sanitizeScheduledJobName(name, rec.Id))
		rec.Set("description", def.Description)
	} else if rec.GetString("name") == "" {
		// Fallback for status updates where def is not parsed
		name := fallbackScheduledJobName(rec.GetString("job_file"), rec.Id)
		rec.Set("name", sanitizeScheduledJobName(name, rec.Id))
	}
}

func fallbackScheduledJobName(jobFile, id string) string {
	name := strings.TrimSuffix(filepath.Base(jobFile), filepath.Ext(jobFile))
	if name == "" || name == "." {
		name = "job_" + id
	}
	return name
}

func sanitizeScheduledJobName(name, id string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	previousSeparator := false
	for _, r := range name {
		allowed := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == ' '
		if allowed {
			b.WriteRune(r)
			previousSeparator = r == '-' || r == ' '
			continue
		}
		if !previousSeparator {
			b.WriteRune('-')
			previousSeparator = true
		}
	}

	sanitized := strings.Trim(b.String(), " -_")
	if sanitized == "" {
		sanitized = "job_" + id
	}
	return sanitized
}

// setJobRunStarted transitions status to "running", sets started_at, and calculates queue time.
func (s *Scheduler) setJobRunStarted(runID string) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	rec, err := s.app.FindRecordById("job_runs", runID)
	if err != nil {
		return fmt.Errorf("set job_run started run=%s: %w", runID, err)
	}
	currentStatus := rec.GetString("status")
	if currentStatus == "success" || currentStatus == "error" || currentStatus == "stalled" || currentStatus == "forgotten" {
		return nil
	}
	now := time.Now()
	rec.Set("status", "running")
	rec.Set("started_at", now)

	createdTime := rec.GetDateTime("created").Time()
	queueMs := now.Sub(createdTime).Milliseconds()
	if queueMs < 0 {
		queueMs = 0
	}
	rec.Set("queue_time_ms", queueMs)

	if err := s.app.Save(rec); err != nil {
		return fmt.Errorf("set job_run started run=%s: %w", runID, err)
	}
	return nil
}

// setJobRunStatus updates only the status field of a job_run record.
// It is protected by s.runMu to prevent a race condition with updateJobRun when a job run
// starts and completes/fails almost instantly.
func (s *Scheduler) setJobRunStatus(runID, status string) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	rec, err := s.app.FindRecordById("job_runs", runID)
	if err != nil {
		return fmt.Errorf("set job_run status run=%s status=%s: %w", runID, status, err)
	}
	currentStatus := rec.GetString("status")
	// If the status is "running" but the run has already completed or failed (terminal state),
	// do not overwrite the completed status back to "running".
	if status == "running" && (currentStatus == "success" || currentStatus == "error" || currentStatus == "stalled" || currentStatus == "forgotten") {
		return nil
	}
	rec.Set("status", status)
	if err := s.app.Save(rec); err != nil {
		return fmt.Errorf("set job_run status run=%s status=%s: %w", runID, status, err)
	}
	return nil
}

// updateJobRun sets the final status, output, and duration on a job_run record.
// It is protected by s.runMu to prevent a race condition with setJobRunStatus.
func (s *Scheduler) updateJobRun(runID, status, output string, durationMs int64, queueTimeMs, execTimeMs int64) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	rec, err := s.app.FindRecordById("job_runs", runID)
	if err != nil {
		return fmt.Errorf("update job_run run=%s status=%s: %w", runID, status, err)
	}
	rec.Set("status", status)

	// Truncate output to prevent database bloat
	const maxOutputLength = 1000000
	if len(output) > maxOutputLength {
		marker := "\n\n... [OUTPUT TRUNCATED FOR SIZE] ...\n\n"
		available := maxOutputLength - len(marker)
		if available < 0 {
			available = 0
		}
		head := available / 2
		tail := available - head
		output = output[:head] + marker + output[len(output)-tail:]
	}

	rec.Set("output", output)
	rec.Set("duration_ms", durationMs)

	if queueTimeMs > 0 {
		rec.Set("queue_time_ms", queueTimeMs)
	}
	if execTimeMs > 0 {
		rec.Set("execution_time_ms", execTimeMs)
	} else if status == "success" || status == "error" {
		startedAt := rec.GetDateTime("started_at").Time()
		if !startedAt.IsZero() {
			rec.Set("execution_time_ms", time.Since(startedAt).Milliseconds())
		}
	}

	if err := s.app.Save(rec); err != nil {
		return fmt.Errorf("update job_run run=%s status=%s: %w", runID, status, err)
	}
	return nil
}

// loadEnvVars fetches and decrypts job_env_vars for the given job.
func (s *Scheduler) loadEnvVars(ctx context.Context, jobID string) (map[string]string, error) {
	loadCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return envvars.LoadJob(loadCtx, s.app, s.secretsRegistry, jobID)
}

// repoHeadSHA returns the local HEAD commit SHA for the given repository.
// Returns an empty string if the repo hasn't been cloned or the HEAD can't be read.
func (s *Scheduler) repoHeadSHA(repoID string) string {
	if repoID == "" {
		return ""
	}
	repoPath := filepath.Join(s.repoWorkspace, repoID)
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

// HandleWorkerDisconnect is called when a worker disconnects.
// It immediately marks all "running" job runs for this worker as "error" (lost).
func (s *Scheduler) HandleWorkerDisconnect(workerID string) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	records, err := s.app.FindAllRecords("job_runs", dbx.HashExp{
		"worker": workerID,
		"status": "running",
	})
	if err != nil {
		return fmt.Errorf("HandleWorkerDisconnect query failed worker=%s: %w", workerID, err)
	}

	var firstErr error
	for _, rec := range records {
		rec.Set("status", "error")
		rec.Set("output", "job lost: worker disconnected and job is no longer running")

		timeoutMs := s.getJobTimeoutMs(rec)
		rec.Set("execution_time_ms", timeoutMs)

		if err := s.app.Save(rec); err != nil {
			log.Printf("[jobscheduler] HandleWorkerDisconnect: failed to save run %s worker=%s: %v", rec.Id, workerID, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("disconnect run=%s worker=%s: %w", rec.Id, workerID, err)
			}
		} else {
			log.Printf("[jobscheduler] run %s marked error due to worker %s disconnect", rec.Id, workerID)
		}
	}
	return firstErr
}

// getJobTimeoutMs parses job.yaml and extracts the timeout in milliseconds, defaulting to 10 minutes.
func (s *Scheduler) getJobTimeoutMs(rec *core.Record) int64 {
	jobID := rec.GetString("job")
	if jobID == "" {
		return 600000 // default 10m in ms
	}
	jobRec, err := s.app.FindRecordById("scheduled_jobs", jobID)
	if err != nil {
		return 600000
	}
	repoID := jobRec.GetString("repository")
	jobFile := jobRec.GetString("job_file")
	def, err := job.ParseJobFile(s.repoWorkspace, repoID, jobFile)
	if err != nil {
		return 600000
	}
	if def.Resources.Timeout != "" {
		d, err := time.ParseDuration(def.Resources.Timeout)
		if err == nil {
			return d.Milliseconds()
		}
	}
	return 600000 // default 10m in ms
}
