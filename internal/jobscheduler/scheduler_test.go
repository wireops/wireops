package jobscheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/protocol"
)

type fakeJobDispatcher struct {
	workers []string
}

func (f fakeJobDispatcher) GetWorkersByTags(_ []string) []string {
	return f.workers
}

func (f fakeJobDispatcher) IsConnected(workerID string) bool {
	for _, id := range f.workers {
		if id == workerID {
			return true
		}
	}
	return false
}

func (f fakeJobDispatcher) Dispatch(context.Context, string, interface{}) (protocol.CommandResult, error) {
	return protocol.CommandResult{Output: "started"}, nil
}

func TestExecuteJobPersistsDefinitionError(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	jobRec := createScheduledJobRecord(t, app, repo.Id, "missing.yml")
	s := NewScheduler(app, fakeJobDispatcher{workers: []string{"worker-1"}}, app.DataDir())

	s.executeJob(jobRec.Id, "manual")

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
	if runs[0].GetString("output") == "" {
		t.Fatal("expected persisted definition error output")
	}
}

func TestExecuteJobWithoutWorkersPersistsStalledRun(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	writeJobFile(t, app.DataDir(), repo.Id, "job.yaml")
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")
	s := NewScheduler(app, fakeJobDispatcher{}, app.DataDir())

	s.executeJob(jobRec.Id, "cron")

	refreshed, err := app.FindRecordById("scheduled_jobs", jobRec.Id)
	if err != nil {
		t.Fatalf("failed to reload job: %v", err)
	}
	if got := refreshed.GetString("status"); got != "stalled" {
		t.Fatalf("scheduled job status = %q, want stalled", got)
	}

	runs, err := app.FindAllRecords("job_runs", dbx.HashExp{"job": jobRec.Id})
	if err != nil {
		t.Fatalf("failed to query job runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("job runs = %d, want 1", len(runs))
	}
	if got := runs[0].GetString("status"); got != "stalled" {
		t.Fatalf("job run status = %q, want stalled", got)
	}
}

func newJobSchedulerTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	ensureJobSchedulerCollections(t, app)
	t.Cleanup(func() { app.Cleanup() })
	return app
}

func ensureJobSchedulerCollections(t *testing.T, app core.App) {
	t.Helper()

	if _, err := app.FindCollectionByNameOrId("repositories"); err == nil {
		return
	}

	repos := core.NewBaseCollection("repositories")
	repos.Fields.Add(&core.TextField{Name: "name"})
	repos.Fields.Add(&core.TextField{Name: "branch"})
	if err := app.Save(repos); err != nil {
		t.Fatalf("failed to create repositories collection: %v", err)
	}

	workers := core.NewBaseCollection("workers")
	workers.Fields.Add(&core.TextField{Name: "hostname"})
	workers.Fields.Add(&core.TextField{Name: "fingerprint"})
	workers.Fields.Add(&core.TextField{Name: "status"})
	if err := app.Save(workers); err != nil {
		t.Fatalf("failed to create workers collection: %v", err)
	}

	jobs := core.NewBaseCollection("scheduled_jobs")
	jobs.Fields.Add(&core.RelationField{Name: "repository", CollectionId: repos.Id, Required: true, MaxSelect: 1})
	jobs.Fields.Add(&core.TextField{Name: "job_file", Required: true})
	jobs.Fields.Add(&core.BoolField{Name: "enabled"})
	jobs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"active", "paused", "stalled", "error"}})
	jobs.Fields.Add(&core.DateField{Name: "last_run_at"})
	if err := app.Save(jobs); err != nil {
		t.Fatalf("failed to create scheduled_jobs collection: %v", err)
	}

	runs := core.NewBaseCollection("job_runs")
	runs.Fields.Add(&core.RelationField{Name: "job", CollectionId: jobs.Id, Required: true, MaxSelect: 1})
	runs.Fields.Add(&core.RelationField{Name: "worker", CollectionId: workers.Id, Required: false, MaxSelect: 1})
	runs.Fields.Add(&core.SelectField{Name: "trigger", Values: []string{"cron", "manual"}})
	runs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"pending", "running", "success", "error", "stalled", "forgotten"}})
	runs.Fields.Add(&core.TextField{Name: "output"})
	runs.Fields.Add(&core.NumberField{Name: "duration_ms"})
	runs.Fields.Add(&core.DateField{Name: "expires_at"})
	runs.Fields.Add(&core.TextField{Name: "container_name"})
	runs.Fields.Add(&core.TextField{Name: "commit_sha"})
	if err := app.Save(runs); err != nil {
		t.Fatalf("failed to create job_runs collection: %v", err)
	}

	env := core.NewBaseCollection("job_env_vars")
	env.Fields.Add(&core.RelationField{Name: "job", CollectionId: jobs.Id, Required: true, MaxSelect: 1})
	env.Fields.Add(&core.TextField{Name: "key"})
	env.Fields.Add(&core.TextField{Name: "value"})
	env.Fields.Add(&core.BoolField{Name: "secret"})
	if err := app.Save(env); err != nil {
		t.Fatalf("failed to create job_env_vars collection: %v", err)
	}
}

func createJobRepoRecord(t *testing.T, app core.App) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		t.Fatalf("failed to find repositories collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "repo")
	rec.Set("branch", "main")
	if err := app.Save(rec); err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}
	return rec
}

func createScheduledJobRecord(t *testing.T, app core.App, repoID, jobFile string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("scheduled_jobs")
	if err != nil {
		t.Fatalf("failed to find scheduled_jobs collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("repository", repoID)
	rec.Set("job_file", jobFile)
	rec.Set("enabled", true)
	rec.Set("status", "active")
	if err := app.Save(rec); err != nil {
		t.Fatalf("failed to create scheduled job: %v", err)
	}
	return rec
}

func writeJobFile(t *testing.T, dataDir, repoID, name string) {
	t.Helper()
	dir := filepath.Join(dataDir, "repositories", repoID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	content := []byte("title: Test Job\ndescription: Test job\nimage: alpine\ncron: \"* * * * *\"\ncommand: echo ok\n")
	if err := os.WriteFile(filepath.Join(dir, name), content, 0644); err != nil {
		t.Fatalf("failed to write job file: %v", err)
	}
}
