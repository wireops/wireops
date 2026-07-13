package jobscheduler

import (
	"strings"
	"testing"

	"github.com/pocketbase/dbx"

	"github.com/wireops/wireops/internal/config"
)

// TestExecuteJobBlockedByDisabledSecretBackend guards the fast pre-flight
// gate in executeJob: a job referencing a disabled vault backend must be
// rejected as an error job_run without ever reaching job.yaml parsing or a
// worker dispatch — mirroring the equivalent stack-deploy gate in
// internal/sync. No job.yaml is written to disk for this test, so a
// passing result also proves the gate fired before ParseJobFile would have
// (that call would otherwise fail with a different, file-not-found error).
func TestExecuteJobBlockedByDisabledSecretBackend(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")
	mustCreateRecord(t, app, "job_env_vars", map[string]any{
		"job":             jobRec.Id,
		"key":             "DB_PASS",
		"value":           "secret/data/myapp#DB_PASS",
		"secret":          true,
		"secret_provider": "vault",
	})
	mustCreateRecord(t, app, "integrations", map[string]any{
		"slug":    "vault",
		"enabled": false,
	})
	s := NewScheduler(app, fakeJobDispatcher{workers: []string{"worker-1"}}, config.GetReposWorkspace())

	s.executeJob(jobRec.Id, "manual", "test-user")

	refreshed, err := app.FindRecordById("scheduled_jobs", jobRec.Id)
	if err != nil {
		t.Fatalf("failed to reload job: %v", err)
	}
	if got := refreshed.GetString("status"); got != "error" {
		t.Fatalf("scheduled job status = %q, want error", got)
	}

	runs, err := app.FindAllRecords("job_runs", dbx.HashExp{"job": jobRec.Id})
	if err != nil {
		t.Fatalf("failed to query job runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("job runs = %d, want 1", len(runs))
	}
	if got := runs[0].GetString("status"); got != "error" {
		t.Fatalf("job run status = %q, want error", got)
	}
	output := runs[0].GetString("output")
	if !strings.Contains(output, "vault") || !strings.Contains(output, "DB_PASS") {
		t.Fatalf("job run output = %q, does not name the disabled backend/env var", output)
	}
	// worker not empty would indicate a dispatch was attempted.
	if got := runs[0].GetString("worker"); got != "" {
		t.Fatalf("job run worker = %q, want empty (must not dispatch when gate blocks)", got)
	}
}

func TestExecuteJobProceedsWhenSecretBackendEnabled(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	writeJobFile(t, repo.Id, "job.yaml")
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")
	mustCreateRecord(t, app, "job_env_vars", map[string]any{
		"job":             jobRec.Id,
		"key":             "DB_PASS",
		"value":           "secret/data/myapp#DB_PASS",
		"secret":          true,
		"secret_provider": "vault",
	})
	mustCreateRecord(t, app, "integrations", map[string]any{
		"slug":    "vault",
		"enabled": true,
	})
	s := NewScheduler(app, fakeJobDispatcher{workers: []string{"worker-1"}}, config.GetReposWorkspace())

	s.executeJob(jobRec.Id, "manual", "test-user")

	runs, err := app.FindAllRecords("job_runs", dbx.HashExp{"job": jobRec.Id})
	if err != nil {
		t.Fatalf("failed to query job runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("job runs = %d, want 1", len(runs))
	}
	// Resolve() still fails since there's no real Vault to talk to — the
	// point of this test is only that it fails *later* (env var
	// resolution), not blocked upfront by the gate.
	output := runs[0].GetString("output")
	if strings.Contains(output, "secret backend unavailable") {
		t.Fatalf("gate incorrectly blocked despite the backend being enabled: %q", output)
	}
}
