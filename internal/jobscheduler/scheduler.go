// Package jobscheduler manages cron-based execution of one-shot Docker jobs.
// All job configuration is read from job.yaml files committed to git repositories;
// the database stores only a thin reference and runtime state.
package jobscheduler

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	gosync "sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/robfig/cron/v3"

	"github.com/wireops/wireops/internal/crypto"
	"github.com/wireops/wireops/internal/git"
	"github.com/wireops/wireops/internal/job"
	"github.com/wireops/wireops/internal/protocol"
)

// AgentDispatcher is the subset of agent.MTLSServer used by the scheduler.
type AgentDispatcher interface {
	GetAgentsByTags(tags []string) []string
	IsEmbedded(agentID string) bool
	IsConnected(agentID string) bool
	Dispatch(ctx context.Context, agentID string, cmd interface{}) (protocol.CommandResult, error)
}

// Scheduler manages cron entries for scheduled jobs and dispatches them to agents.
type Scheduler struct {
	app        core.App
	dispatcher AgentDispatcher
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
func NewScheduler(app core.App, dispatcher AgentDispatcher, dataDir string) *Scheduler {
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
		s.executeJob(jobID, "cron")
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

// SyncJobsForRepo re-registers all jobs pointing to the given repository.
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
		log.Printf("[jobscheduler] re-registered %d job(s) after repo %s update", len(records), repoID)
	}
}

// TriggerManual fires an immediate execution of the job outside its cron schedule.
func (s *Scheduler) TriggerManual(jobID string) {
	go s.executeJob(jobID, "manual")
}

// CancelRun stops a running job container. For embedded agents it runs docker stop
// on the server; for remote agents it dispatches KillJobCommand.
func (s *Scheduler) CancelRun(runID string) error {
	rec, err := s.app.FindRecordById("job_runs", runID)
	if err != nil {
		return fmt.Errorf("job run not found: %w", err)
	}
	if rec.GetString("status") != "running" {
		return fmt.Errorf("job run is not running (status: %s)", rec.GetString("status"))
	}
	agentID := rec.GetString("agent")
	if agentID == "" {
		return fmt.Errorf("job run has no agent")
	}

	containerName := "wireops-job-" + runID

	if s.dispatcher.IsEmbedded(agentID) {
		out, runErr := exec.Command("docker", "stop", containerName).CombinedOutput()
		if runErr != nil {
			return fmt.Errorf("docker stop failed: %w\n%s", runErr, string(out))
		}
		log.Printf("[jobscheduler] cancelled embedded run %s", runID)
		return nil
	}

	cmd := protocol.KillJobCommand{
		CommandID: fmt.Sprintf("kill-%s", runID),
		JobRunID:  runID,
	}
	ctx, cancel := context.WithTimeout(s.rootCtx, 15*time.Second)
	defer cancel()
	result, err := s.dispatcher.Dispatch(ctx, agentID, cmd)
	if err != nil {
		return fmt.Errorf("dispatch kill failed: %w", err)
	}
	if result.Error != "" {
		return fmt.Errorf("agent error: %s", result.Error)
	}
	log.Printf("[jobscheduler] cancelled remote run %s on agent %s", runID, agentID)
	return nil
}

// executeJob is the core execution path. It reads the job.yaml, resolves agents,
// creates job_run records, dispatches to agents, and updates the run status.
func (s *Scheduler) executeJob(jobID, trigger string) {
	ctx := s.rootCtx
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
		log.Printf("[jobscheduler] executeJob: cannot parse job.yaml for job %s: %v", jobID, err)
		return
	}

	agents := s.dispatcher.GetAgentsByTags(def.Tags)
	if len(agents) == 0 {
		s.createStalledRun(jobID, trigger)
		return
	}

	envMap, err := s.loadEnvVars(jobID)
	if err != nil {
		log.Printf("[jobscheduler] executeJob: cannot load env vars for job %s: %v", jobID, err)
		return
	}

	switch def.Mode {
	case job.ModeOnceAll:
		for _, agentID := range agents {
			agentID := agentID
			go s.dispatchToAgent(ctx, jobID, trigger, agentID, def, envMap)
		}
	default: // ModeOnce
		agentID := s.pickAgent(jobID, agents)
		go s.dispatchToAgent(ctx, jobID, trigger, agentID, def, envMap)
	}

	// Update last_run_at immediately; run completion is async.
	rec.Set("last_run_at", time.Now())
	_ = s.app.Save(rec)
}

// dispatchToAgent creates a job_run record and sends RunJobCommand to the agent.
// For remote agents it only waits for the start ack (≤30s), not job completion.
// Completion is delivered via HandleJobCompleted when the agent pushes MsgJobCompleted.
// For the embedded agent the container runs in a goroutine inside this call.
func (s *Scheduler) dispatchToAgent(ctx context.Context, jobID, trigger, agentID string, def *job.Definition, envMap map[string]string) {
	repoID := func() string {
		rec, _ := s.app.FindRecordById("scheduled_jobs", jobID)
		if rec != nil {
			return rec.GetString("repository")
		}
		return ""
	}()

	runID, err := s.createJobRun(jobID, agentID, trigger, "pending")
	if err != nil {
		log.Printf("[jobscheduler] dispatchToAgent: failed to create job_run job=%s agent=%s: %v", jobID, agentID, err)
		return
	}

	containerName := "wireops-job-" + runID
	commitSHA := s.repoHeadSHA(repoID)
	s.patchJobRunMeta(runID, containerName, commitSHA)

	repoBranch := func() string {
		rec, _ := s.app.FindRecordById("repositories", repoID)
		if rec != nil {
			return rec.GetString("branch")
		}
		return ""
	}()

	jobFile := func() string {
		rec, _ := s.app.FindRecordById("scheduled_jobs", jobID)
		if rec != nil {
			return rec.GetString("job_file")
		}
		return ""
	}()

	cmd := protocol.RunJobCommand{
		CommandID:        fmt.Sprintf("job-%s", runID),
		JobRunID:         runID,
		JobName:          def.Title,
		Image:            def.Image,
		Command:          []string(def.Command),
		Env:              envMap,
		RepositoryID:     repoID,
		RepositoryBranch: repoBranch,
		RepositoryFile:   jobFile,
		CommitSHA:        commitSHA,
		Volumes:          def.Volumes,
		Network:          def.Network,
	}

	if s.dispatcher.IsEmbedded(agentID) {
		s.setJobRunStatus(runID, "running")
		s.runJobEmbedded(ctx, runID, cmd)
		return
	}

	// Remote agent: wait only for the start ack (agent immediately returns "started").
	// Actual completion arrives later via HandleJobCompleted.
	ackCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, dispatchErr := s.dispatcher.Dispatch(ackCtx, agentID, cmd)
	if dispatchErr != nil {
		log.Printf("[jobscheduler] dispatchToAgent: ack error job=%s run=%s: %v", jobID, runID, dispatchErr)
		s.updateJobRun(runID, "error", dispatchErr.Error(), 0)
		return
	}
	if result.Error != "" {
		log.Printf("[jobscheduler] dispatchToAgent: agent error job=%s run=%s: %s", jobID, runID, result.Error)
		s.updateJobRun(runID, "error", result.Error, 0)
		return
	}

	// Ack received — container is starting.
	s.setJobRunStatus(runID, "running")
	log.Printf("[jobscheduler] job %s run %s started on agent %s", jobID, runID, agentID)
}

// runJobEmbedded starts the container asynchronously on the server (embedded agent).
// When the container exits it updates the job_run directly without WebSocket round-trips.
func (s *Scheduler) runJobEmbedded(ctx context.Context, runID string, cmd protocol.RunJobCommand) {
	go func() {
		start := time.Now()
		args := buildDockerRunArgs(cmd)
		out, runErr := exec.Command("docker", args...).CombinedOutput()
		elapsed := time.Since(start).Milliseconds()

		output := string(out)
		status := "success"
		if runErr != nil {
			status = "error"
			if output != "" {
				output += "\n"
			}
			output += runErr.Error()
			log.Printf("[jobscheduler] embedded run %s error elapsed=%dms: %v", runID, elapsed, runErr)
		} else {
			log.Printf("[jobscheduler] embedded run %s done elapsed=%dms", runID, elapsed)
		}
		s.updateJobRun(runID, status, output, elapsed)
	}()
}

// buildDockerRunArgs assembles the docker run argument list (shared with agent executor).
func buildDockerRunArgs(cmd protocol.RunJobCommand) []string {
	args := []string{"run"}
	// Force ephemeral containers: always remove after execution.
	args = append(args, "--rm")
	args = append(args, "--name", "wireops-job-"+cmd.JobRunID)

	// Inject standard labels
	args = append(args, "-l", "dev.wireops.managed=true")
	if cmd.RepositoryID != "" {
		args = append(args, "-l", "dev.wireops.repository.id="+cmd.RepositoryID)
	}
	if cmd.RepositoryBranch != "" {
		args = append(args, "-l", "dev.wireops.repository.branch="+cmd.RepositoryBranch)
	}
	if cmd.RepositoryFile != "" {
		args = append(args, "-l", "dev.wireops.repository.file="+cmd.RepositoryFile)
	}
	if cmd.CommitSHA != "" {
		args = append(args, "-l", "dev.wireops.repository.commit_sha="+cmd.CommitSHA)
	}
	if cmd.JobName != "" {
		args = append(args, "-l", "dev.wireops.job.name="+cmd.JobName)
	}

	for k, v := range cmd.Env {
		args = append(args, "-e", k+"="+v)
	}
	for _, v := range cmd.Volumes {
		args = append(args, "-v", v)
	}
	if cmd.Network != "" {
		args = append(args, "--network", cmd.Network)
	}
	args = append(args, cmd.Image)
	args = append(args, cmd.Command...)
	return args
}

// HandleJobCompleted is called by the MTLSServer when a remote agent pushes
// a MsgJobCompleted message. It updates the job_run record with the final result.
func (s *Scheduler) HandleJobCompleted(msg protocol.JobCompletedMessage) {
	status := "success"
	if !msg.Success {
		status = "error"
	}
	s.updateJobRun(msg.JobRunID, status, msg.Output, msg.DurationMs)
	log.Printf("[jobscheduler] HandleJobCompleted run=%s status=%s elapsed=%dms", msg.JobRunID, status, msg.DurationMs)
}

// maxJobRunDuration is the wall-clock ceiling for a single job run.
// Any run still in "running" after this duration is considered lost.
const maxJobRunDuration = time.Hour

// MarkForgottenRuns finds every job_run that has been in "running" state for
// longer than maxJobRunDuration and marks it as "forgotten". This is a safety
// net for short-execution jobs: a 1-hour wall-clock limit signals something
// went wrong silently (agent crash with no reconnect, zombie container, etc.).
func (s *Scheduler) MarkForgottenRuns() {
	cutoff := time.Now().Add(-maxJobRunDuration)

	records, err := s.app.FindAllRecords("job_runs",
		dbx.HashExp{"status": "running"},
	)
	if err != nil {
		log.Printf("[jobscheduler] MarkForgottenRuns: query failed: %v", err)
		return
	}

	for _, rec := range records {
		if rec.GetDateTime("updated").Time().Before(cutoff) {
			rec.Set("status", "forgotten")
			rec.Set("output", "job forgotten: still running after 1 hour with no completion signal")
			if err := s.app.Save(rec); err != nil {
				log.Printf("[jobscheduler] MarkForgottenRuns: failed to save run %s: %v", rec.Id, err)
			} else {
				log.Printf("[jobscheduler] run %s marked forgotten (started >1h ago)", rec.Id)
			}
		}
	}
}

// HandleAgentReconnect marks any job_runs that were left in "running" state for the
// given agent as "error". This handles the case where the agent restarted mid-job.
func (s *Scheduler) HandleAgentReconnect(agentID string) {
	records, err := s.app.FindAllRecords("job_runs", dbx.HashExp{
		"agent":  agentID,
		"status": "running",
	})
	if err != nil {
		log.Printf("[jobscheduler] HandleAgentReconnect: query failed: %v", err)
		return
	}
	for _, rec := range records {
		rec.Set("status", "error")
		rec.Set("output", "job lost: agent disconnected or restarted during execution")
		_ = s.app.Save(rec)
		log.Printf("[jobscheduler] run %s marked error: agent %s reconnected", rec.Id, agentID)
	}
}

// createStalledRun writes a job_run with status=stalled when no agents are available.
func (s *Scheduler) createStalledRun(jobID, trigger string) {
	runID, err := s.createJobRun(jobID, "", trigger, "stalled")
	if err != nil {
		log.Printf("[jobscheduler] createStalledRun: failed to create stalled run for job %s: %v", jobID, err)
		return
	}
	log.Printf("[jobscheduler] job %s stalled (no matching agents), run %s", jobID, runID)
}

// createJobRun inserts a job_run record and returns its ID.
func (s *Scheduler) createJobRun(jobID, agentID, trigger, status string) (string, error) {
	col, err := s.app.FindCollectionByNameOrId("job_runs")
	if err != nil {
		return "", err
	}
	rec := core.NewRecord(col)
	rec.Set("job", jobID)
	rec.Set("trigger", trigger)
	rec.Set("status", status)
	rec.Set("expires_at", time.Now().AddDate(0, 0, 30))
	if agentID != "" {
		rec.Set("agent", agentID)
	}
	if err := s.app.Save(rec); err != nil {
		return "", err
	}
	return rec.Id, nil
}

// setJobRunStatus updates only the status field of a job_run record.
func (s *Scheduler) setJobRunStatus(runID, status string) {
	rec, err := s.app.FindRecordById("job_runs", runID)
	if err != nil {
		log.Printf("[jobscheduler] setJobRunStatus: run %s not found: %v", runID, err)
		return
	}
	rec.Set("status", status)
	if err := s.app.Save(rec); err != nil {
		log.Printf("[jobscheduler] setJobRunStatus: failed to save run %s: %v", runID, err)
	}
}

// updateJobRun sets the final status, output, and duration on a job_run record.
func (s *Scheduler) updateJobRun(runID, status, output string, durationMs int64) {
	rec, err := s.app.FindRecordById("job_runs", runID)
	if err != nil {
		log.Printf("[jobscheduler] updateJobRun: run %s not found: %v", runID, err)
		return
	}
	rec.Set("status", status)
	rec.Set("output", output)
	rec.Set("duration_ms", durationMs)
	if err := s.app.Save(rec); err != nil {
		log.Printf("[jobscheduler] updateJobRun: failed to save run %s: %v", runID, err)
	}
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
func (s *Scheduler) patchJobRunMeta(runID, containerName, commitSHA string) {
	rec, err := s.app.FindRecordById("job_runs", runID)
	if err != nil {
		return
	}
	rec.Set("container_name", containerName)
	rec.Set("commit_sha", commitSHA)
	if err := s.app.Save(rec); err != nil {
		log.Printf("[jobscheduler] patchJobRunMeta: failed to save run %s: %v", runID, err)
	}
}

// pickAgent selects one agent from the list using a simple round-robin keyed by jobID.
func (s *Scheduler) pickAgent(jobID string, agents []string) string {
	if len(agents) == 1 {
		return agents[0]
	}
	// Use the current unix second modulo length for a cheap stateless round-robin.
	idx := int(time.Now().Unix()) % len(agents)
	return agents[idx]
}
