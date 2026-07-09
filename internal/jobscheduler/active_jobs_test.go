package jobscheduler

import (
	"testing"
)

func TestActiveJobRunsForRepositoryFindsRunningJobsForRepo(t *testing.T) {
	app := newJobSchedulerTestApp(t)

	repoA := mustCreateRecord(t, app, "repositories", map[string]any{"name": "repo-a"})
	repoB := mustCreateRecord(t, app, "repositories", map[string]any{"name": "repo-b"})

	jobA := mustCreateRecord(t, app, "scheduled_jobs", map[string]any{
		"repository": repoA.Id, "name": "job-a", "job_file": "job.yaml", "enabled": true,
	})
	jobB := mustCreateRecord(t, app, "scheduled_jobs", map[string]any{
		"repository": repoB.Id, "name": "job-b", "job_file": "job.yaml", "enabled": true,
	})

	mustCreateRecord(t, app, "job_runs", map[string]any{"job": jobA.Id, "trigger": "cron", "status": "running"})
	mustCreateRecord(t, app, "job_runs", map[string]any{"job": jobA.Id, "trigger": "cron", "status": "success"})
	mustCreateRecord(t, app, "job_runs", map[string]any{"job": jobB.Id, "trigger": "cron", "status": "running"})

	active, err := ActiveJobRunsForRepository(app, repoA.Id)
	if err != nil {
		t.Fatalf("ActiveJobRunsForRepository failed: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active job_runs for repoA = %d, want 1", len(active))
	}
	if active[0].GetString("status") != "running" {
		t.Fatalf("status = %q, want running", active[0].GetString("status"))
	}
}

func TestActiveJobRunsForRepositoryEmptyWhenNoneRunning(t *testing.T) {
	app := newJobSchedulerTestApp(t)

	repo := mustCreateRecord(t, app, "repositories", map[string]any{"name": "repo-a"})
	job := mustCreateRecord(t, app, "scheduled_jobs", map[string]any{
		"repository": repo.Id, "name": "job-a", "job_file": "job.yaml", "enabled": true,
	})
	mustCreateRecord(t, app, "job_runs", map[string]any{"job": job.Id, "trigger": "cron", "status": "success"})

	active, err := ActiveJobRunsForRepository(app, repo.Id)
	if err != nil {
		t.Fatalf("ActiveJobRunsForRepository failed: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active job_runs = %d, want 0", len(active))
	}
}

func TestActiveJobRunsForRepositoryEmptyRepoID(t *testing.T) {
	app := newJobSchedulerTestApp(t)

	active, err := ActiveJobRunsForRepository(app, "")
	if err != nil {
		t.Fatalf("ActiveJobRunsForRepository failed: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active job_runs = %d, want 0 for empty repoID", len(active))
	}
}
