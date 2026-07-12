package sync

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func newWaitJobsTestApp(t *testing.T) (*tests.TestApp, *core.Record, *core.Record) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	repos := core.NewBaseCollection("repositories")
	repos.Fields.Add(&core.TextField{Name: "name"})
	if err := app.Save(repos); err != nil {
		t.Fatalf("failed to create repositories collection: %v", err)
	}

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name", Required: true})
	stacks.Fields.Add(&core.RelationField{Name: "repository", CollectionId: repos.Id, Required: true, MaxSelect: 1})
	stacks.Fields.Add(&core.SelectField{Name: "status", Values: []string{"active", "syncing", "paused", "error", "pending", "degraded"}})
	stacks.Fields.Add(&core.SelectField{Name: "wait_running_jobs", MaxSelect: 1, Values: []string{"never", "always", "timeout"}})
	stacks.Fields.Add(&core.NumberField{Name: "wait_running_jobs_timeout_seconds"})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("failed to create stacks collection: %v", err)
	}

	syncLogs := core.NewBaseCollection("sync_logs")
	syncLogs.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, Required: true, MaxSelect: 1})
	syncLogs.Fields.Add(&core.SelectField{Name: "trigger", Values: []string{"cron", "webhook", "manual", "redeploy", "rollback", "transfer", "queue"}})
	syncLogs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"running", "success", "error", "queued", "noop", "waiting_jobs", "degraded"}})
	syncLogs.Fields.Add(&core.TextField{Name: "commit_sha"})
	syncLogs.Fields.Add(&core.TextField{Name: "commit_message"})
	syncLogs.Fields.Add(&core.TextField{Name: "output"})
	syncLogs.Fields.Add(&core.NumberField{Name: "duration_ms"})
	if err := app.Save(syncLogs); err != nil {
		t.Fatalf("failed to create sync_logs collection: %v", err)
	}

	phases := core.NewBaseCollection("sync_log_phases")
	phases.Fields.Add(&core.RelationField{Name: "sync_log", CollectionId: syncLogs.Id, Required: true, MaxSelect: 1})
	phases.Fields.Add(&core.SelectField{Name: "phase", Required: true, MaxSelect: 1, Values: deployPhases})
	phases.Fields.Add(&core.SelectField{Name: "status", Required: true, MaxSelect: 1, Values: []string{"running", "success", "error", "skipped"}})
	phases.Fields.Add(&core.DateField{Name: "started_at", Required: true})
	phases.Fields.Add(&core.NumberField{Name: "duration_ms"})
	phases.Fields.Add(&core.TextField{Name: "detail"})
	phases.Fields.Add(&core.NumberField{Name: "seq"})
	if err := app.Save(phases); err != nil {
		t.Fatalf("failed to create sync_log_phases collection: %v", err)
	}

	jobs := core.NewBaseCollection("scheduled_jobs")
	jobs.Fields.Add(&core.RelationField{Name: "repository", CollectionId: repos.Id, Required: true, MaxSelect: 1})
	jobs.Fields.Add(&core.TextField{Name: "job_file", Required: true})
	jobs.Fields.Add(&core.BoolField{Name: "enabled"})
	if err := app.Save(jobs); err != nil {
		t.Fatalf("failed to create scheduled_jobs collection: %v", err)
	}

	runs := core.NewBaseCollection("job_runs")
	runs.Fields.Add(&core.RelationField{Name: "job", CollectionId: jobs.Id, Required: true, MaxSelect: 1})
	runs.Fields.Add(&core.SelectField{Name: "trigger", Values: []string{"cron", "manual"}})
	runs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"pending", "running", "success", "error", "stalled", "forgotten"}})
	runs.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	runs.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
	if err := app.Save(runs); err != nil {
		t.Fatalf("failed to create job_runs collection: %v", err)
	}

	repo := core.NewRecord(repos)
	repo.Set("name", "repo")
	if err := app.Save(repo); err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	stack := core.NewRecord(stacks)
	stack.Set("name", "stack")
	stack.Set("repository", repo.Id)
	stack.Set("status", "syncing")
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to create stack: %v", err)
	}

	return app, repo, stack
}

func createRunningJob(t *testing.T, app core.App, repoID string) *core.Record {
	t.Helper()
	jobsCol, err := app.FindCollectionByNameOrId("scheduled_jobs")
	if err != nil {
		t.Fatalf("failed to find scheduled_jobs: %v", err)
	}
	job := core.NewRecord(jobsCol)
	job.Set("repository", repoID)
	job.Set("job_file", "job.yaml")
	job.Set("enabled", true)
	if err := app.Save(job); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	runsCol, err := app.FindCollectionByNameOrId("job_runs")
	if err != nil {
		t.Fatalf("failed to find job_runs: %v", err)
	}
	run := core.NewRecord(runsCol)
	run.Set("job", job.Id)
	run.Set("trigger", "cron")
	run.Set("status", "running")
	if err := app.Save(run); err != nil {
		t.Fatalf("failed to create job_run: %v", err)
	}
	return run
}

func withFastWaitJobsPoll(t *testing.T) {
	t.Helper()
	orig := waitJobsPollInterval
	waitJobsPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { waitJobsPollInterval = orig })
}

func TestWaitForRunningJobsSkipsWhenPolicyNever(t *testing.T) {
	app, repo, stack := newWaitJobsTestApp(t)
	createRunningJob(t, app, repo.Id)
	r := &Reconciler{app: app}

	if _, err := r.waitForRunningJobs(context.Background(), stack, repo.Id, stack.Id, "cron", "sha1"); err != nil {
		t.Fatalf("waitForRunningJobs failed: %v", err)
	}

	logs, _ := app.FindAllRecords("sync_logs", dbx.HashExp{"stack": stack.Id})
	if len(logs) != 0 {
		t.Fatalf("expected no sync_log entries when policy is never, got %d", len(logs))
	}
}

func TestWaitForRunningJobsSkipsWhenNoActiveJobs(t *testing.T) {
	app, repo, stack := newWaitJobsTestApp(t)
	stack.Set("wait_running_jobs", "always")
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to update stack: %v", err)
	}
	r := &Reconciler{app: app}

	start := time.Now()
	if _, err := r.waitForRunningJobs(context.Background(), stack, repo.Id, stack.Id, "cron", "sha1"); err != nil {
		t.Fatalf("waitForRunningJobs failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("expected immediate return with no active jobs, took %s", elapsed)
	}
}

func TestWaitForRunningJobsAlwaysBlocksUntilJobFinishes(t *testing.T) {
	withFastWaitJobsPoll(t)
	app, repo, stack := newWaitJobsTestApp(t)
	stack.Set("wait_running_jobs", "always")
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to update stack: %v", err)
	}
	run := createRunningJob(t, app, repo.Id)
	r := &Reconciler{app: app}

	type waitResult struct {
		rec *core.Record
		err error
	}
	done := make(chan waitResult, 1)
	go func() {
		rec, err := r.waitForRunningJobs(context.Background(), stack, repo.Id, stack.Id, "cron", "sha1")
		done <- waitResult{rec, err}
	}()

	select {
	case <-done:
		t.Fatal("waitForRunningJobs returned before the job finished")
	case <-time.After(100 * time.Millisecond):
	}

	run.Set("status", "success")
	if err := app.Save(run); err != nil {
		t.Fatalf("failed to finish job run: %v", err)
	}

	var reused *core.Record
	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("waitForRunningJobs returned error after job finished: %v", res.err)
		}
		reused = res.rec
	case <-time.After(3 * time.Second):
		t.Fatal("waitForRunningJobs did not return after job finished")
	}

	if reused == nil {
		t.Fatal("expected waitForRunningJobs to return the reusable sync log it created")
	}

	// Status stays "running" (not a terminal "success"): the caller reuses
	// this same row as the deploy's own sync log, the deploy isn't done yet.
	logs, _ := app.FindAllRecords("sync_logs", dbx.HashExp{"stack": stack.Id, "status": "running"})
	found := false
	for _, l := range logs {
		if strings.Contains(l.GetString("output"), "proceeded after waiting") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a sync_log entry recording that the deploy proceeded after waiting")
	}

	phases, _ := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": reused.Id, "phase": "policy_check"})
	if len(phases) != 1 {
		t.Fatalf("policy_check phase rows = %d, want 1", len(phases))
	}
	if got := phases[0].GetString("status"); got != "success" {
		t.Fatalf("policy_check phase status = %q, want success", got)
	}
}

func TestWaitForRunningJobsTimeoutBlocksDeploy(t *testing.T) {
	withFastWaitJobsPoll(t)
	app, repo, stack := newWaitJobsTestApp(t)
	stack.Set("wait_running_jobs", "timeout")
	stack.Set("wait_running_jobs_timeout_seconds", 0) // 0 -> treated as immediate-ish via short default, but we still need a fast test
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to update stack: %v", err)
	}
	createRunningJob(t, app, repo.Id)
	r := &Reconciler{app: app}

	// Force a near-immediate timeout deadline for the test without waiting
	// out the real default (300s) by overriding the stored timeout to a
	// value smaller than one poll interval's worth of wall-clock seconds
	// isn't possible via whole seconds, so instead assert the blocking
	// behavior with a short deadline set in whole seconds and a fast poll.
	stack.Set("wait_running_jobs_timeout_seconds", 1)
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to update stack: %v", err)
	}

	start := time.Now()
	reused, err := r.waitForRunningJobs(context.Background(), stack, repo.Id, stack.Id, "cron", "sha1")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected waitForRunningJobs to return a timeout error")
	}
	if !strings.Contains(err.Error(), "timeout waiting") {
		t.Fatalf("error = %q, want timeout message", err.Error())
	}
	if elapsed > 5*time.Second {
		t.Fatalf("timeout took too long: %s", elapsed)
	}

	refreshed, _ := app.FindRecordById("stacks", stack.Id)
	if got := refreshed.GetString("status"); got != "error" {
		t.Fatalf("stack status = %q, want error after wait timeout", got)
	}

	logs, _ := app.FindAllRecords("sync_logs", dbx.HashExp{"stack": stack.Id, "status": "error"})
	if len(logs) == 0 {
		t.Fatal("expected an error sync_log entry for the timed-out wait")
	}

	if reused == nil {
		t.Fatal("expected the timed-out wait to still return the sync log it created")
	}
	phases, _ := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": reused.Id, "phase": "policy_check", "status": "error"})
	if len(phases) != 1 {
		t.Fatalf("policy_check error phase rows = %d, want 1", len(phases))
	}
}

func TestWaitForRunningJobsContextCancelStopsWaiting(t *testing.T) {
	withFastWaitJobsPoll(t)
	app, repo, stack := newWaitJobsTestApp(t)
	stack.Set("wait_running_jobs", "always")
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to update stack: %v", err)
	}
	createRunningJob(t, app, repo.Id)
	r := &Reconciler{app: app}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := r.waitForRunningJobs(ctx, stack, repo.Id, stack.Id, "cron", "sha1")
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected context cancellation error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitForRunningJobs did not return promptly after context cancellation")
	}
}
