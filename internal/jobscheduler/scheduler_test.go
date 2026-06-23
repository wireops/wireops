package jobscheduler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time"

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

type capturingJobDispatcher struct {
	workers    []string
	onDispatch func(cmd interface{})
}

func (f capturingJobDispatcher) GetWorkersByTags(_ []string) []string {
	return f.workers
}

func (f capturingJobDispatcher) IsConnected(workerID string) bool {
	return true
}

func (f capturingJobDispatcher) Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error) {
	if f.onDispatch != nil {
		f.onDispatch(cmd)
	}
	return protocol.CommandResult{Output: "started"}, nil
}

func TestExecuteJobPersistsDefinitionError(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	jobRec := createScheduledJobRecord(t, app, repo.Id, "missing.yml")
	s := NewScheduler(app, fakeJobDispatcher{workers: []string{"worker-1"}}, filepath.Join(app.DataDir(), "repositories"))

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
	if runs[0].GetString("output") == "" {
		t.Fatal("expected persisted definition error output")
	}
}

func TestExecuteJobWithoutWorkersPersistsStalledRun(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	writeJobFile(t, app.DataDir(), repo.Id, "job.yaml")
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")
	s := NewScheduler(app, fakeJobDispatcher{}, filepath.Join(app.DataDir(), "repositories"))

	s.executeJob(jobRec.Id, "cron", "system")

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

func TestExecuteJobWithoutWorkersBackfillsLegacyName(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	writeJobFile(t, app.DataDir(), repo.Id, "job.yaml")
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")
	if _, err := app.DB().NewQuery("UPDATE scheduled_jobs SET name = '' WHERE id = {:id}").
		Bind(dbx.Params{"id": jobRec.Id}).
		Execute(); err != nil {
		t.Fatalf("failed to create legacy scheduled job fixture: %v", err)
	}
	s := NewScheduler(app, fakeJobDispatcher{}, filepath.Join(app.DataDir(), "repositories"))

	s.executeJob(jobRec.Id, "cron", "system")

	refreshed, err := app.FindRecordById("scheduled_jobs", jobRec.Id)
	if err != nil {
		t.Fatalf("failed to reload job: %v", err)
	}
	if got := refreshed.GetString("status"); got != "stalled" {
		t.Fatalf("scheduled job status = %q, want stalled", got)
	}
	if got := refreshed.GetString("name"); got != "Test Job" {
		t.Fatalf("scheduled job name = %q, want Test Job", got)
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

// mustSaveCollection saves a collection and fails the test on error.
func mustSaveCollection(t *testing.T, app core.App, col *core.Collection) {
	t.Helper()
	if err := app.Save(col); err != nil {
		t.Fatalf("failed to save collection %q: %v", col.Name, err)
	}
}

// mustCreateRecord finds a collection, creates a record with the given fields, saves it, and returns it.
func mustCreateRecord(t *testing.T, app core.App, collectionName string, fields map[string]any) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		t.Fatalf("failed to find collection %q: %v", collectionName, err)
	}
	rec := core.NewRecord(col)
	for k, v := range fields {
		rec.Set(k, v)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("failed to save record in %q: %v", collectionName, err)
	}
	return rec
}

func ensureJobSchedulerCollections(t *testing.T, app core.App) {
	t.Helper()

	if _, err := app.FindCollectionByNameOrId("repositories"); err == nil {
		return
	}

	repos := core.NewBaseCollection("repositories")
	repos.Fields.Add(&core.TextField{Name: "name"})
	repos.Fields.Add(&core.TextField{Name: "branch"})
	mustSaveCollection(t, app, repos)

	workers := core.NewBaseCollection("workers")
	workers.Fields.Add(&core.TextField{Name: "hostname"})
	workers.Fields.Add(&core.TextField{Name: "fingerprint"})
	workers.Fields.Add(&core.TextField{Name: "status"})
	mustSaveCollection(t, app, workers)

	workerPolicies := core.NewBaseCollection("worker_policies")
	workerPolicies.Fields.Add(&core.BoolField{Name: "enabled"})
	workerPolicies.Fields.Add(&core.JSONField{Name: "allowed_volumes"})
	workerPolicies.Fields.Add(&core.JSONField{Name: "allowed_networks"})
	workerPolicies.Fields.Add(&core.JSONField{Name: "allowed_images"})
	workerPolicies.Fields.Add(&core.BoolField{Name: "prevent_latest_images"})
	workerPolicies.Fields.Add(&core.BoolField{Name: "block_host_volumes"})
	mustSaveCollection(t, app, workerPolicies)

	jobs := core.NewBaseCollection("scheduled_jobs")
	jobs.Fields.Add(&core.RelationField{Name: "repository", CollectionId: repos.Id, Required: true, MaxSelect: 1})
	jobs.Fields.Add(&core.TextField{Name: "name", Required: true, Pattern: `^[a-zA-Z0-9\p{L}_ -]+$`})
	jobs.Fields.Add(&core.TextField{Name: "description"})
	jobs.Fields.Add(&core.TextField{Name: "job_file", Required: true})
	jobs.Fields.Add(&core.BoolField{Name: "enabled"})
	jobs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"active", "paused", "stalled", "error"}})
	jobs.Fields.Add(&core.DateField{Name: "last_run_at"})
	mustSaveCollection(t, app, jobs)

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
	runs.Fields.Add(&core.DateField{Name: "started_at"})
	runs.Fields.Add(&core.NumberField{Name: "queue_time_ms"})
	runs.Fields.Add(&core.NumberField{Name: "execution_time_ms"})
	runs.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	runs.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
	mustSaveCollection(t, app, runs)

	env := core.NewBaseCollection("job_env_vars")
	env.Fields.Add(&core.RelationField{Name: "job", CollectionId: jobs.Id, Required: true, MaxSelect: 1})
	env.Fields.Add(&core.TextField{Name: "key"})
	env.Fields.Add(&core.TextField{Name: "value"})
	env.Fields.Add(&core.BoolField{Name: "secret"})
	mustSaveCollection(t, app, env)

}

func createJobRepoRecord(t *testing.T, app core.App) *core.Record {
	t.Helper()
	return mustCreateRecord(t, app, "repositories", map[string]any{
		"name":   "repo",
		"branch": "main",
	})
}

func createScheduledJobRecord(t *testing.T, app core.App, repoID, jobFile string) *core.Record {
	t.Helper()
	return mustCreateRecord(t, app, "scheduled_jobs", map[string]any{
		"repository": repoID,
		"name":       "Test Job",
		"job_file":   jobFile,
		"enabled":    true,
		"status":     "active",
	})
}

func writeJobFile(t *testing.T, dataDir, repoID, name string) {
	t.Helper()
	dir := filepath.Join(dataDir, "repositories", repoID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	content := []byte("name: Test Job\ndescription: Test job\nimage: alpine\ncron: \"* * * * *\"\ncommand: echo ok\nresources:\n  cpu: \"0.5\"\n  memory: \"512m\"\n  timeout: \"10m\"\n")
	if err := os.WriteFile(filepath.Join(dir, name), content, 0644); err != nil {
		t.Fatalf("failed to write job file: %v", err)
	}
}

func updateJobRunTime(t *testing.T, app core.App, id string, offset time.Duration) {
	t.Helper()
	pastStr := time.Now().Add(offset).UTC().Format("2006-01-02 15:04:05.000Z")
	if _, err := app.DB().NewQuery("UPDATE job_runs SET updated = {:past} WHERE id = {:id}").
		Bind(dbx.Params{
			"past": pastStr,
			"id":   id,
		}).Execute(); err != nil {
		t.Fatalf("failed to update job run %s time: %v", id, err)
	}
}

func TestReconcileActiveJobs(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")

	worker := mustCreateRecord(t, app, "workers", map[string]any{
		"hostname":    "worker-1",
		"fingerprint": "fp1",
		"status":      "ACTIVE",
	})

	// Job run 1: still running (included in heartbeat)
	run1 := mustCreateRecord(t, app, "job_runs", map[string]any{
		"job":     jobRec.Id,
		"worker":  worker.Id,
		"status":  "running",
		"trigger": "manual",
	})
	updateJobRunTime(t, app, run1.Id, -5*time.Minute)

	// Job run 2: lost (not included in heartbeat)
	run2 := mustCreateRecord(t, app, "job_runs", map[string]any{
		"job":     jobRec.Id,
		"worker":  worker.Id,
		"status":  "running",
		"trigger": "manual",
	})
	updateJobRunTime(t, app, run2.Id, -5*time.Minute)

	// Job run 3: newly started, not in heartbeat yet, but within cutoff
	run3 := mustCreateRecord(t, app, "job_runs", map[string]any{
		"job":     jobRec.Id,
		"worker":  worker.Id,
		"status":  "running",
		"trigger": "manual",
	})

	s := NewScheduler(app, fakeJobDispatcher{workers: []string{worker.Id}}, filepath.Join(app.DataDir(), "repositories"))

	// Reconcile with activeIDs containing only run1.Id
	if err := s.ReconcileActiveJobs(worker.Id, []string{run1.Id}); err != nil {
		t.Fatalf("ReconcileActiveJobs failed: %v", err)
	}

	// Run 1 should still be running
	ref1, _ := app.FindRecordById("job_runs", run1.Id)
	if got := ref1.GetString("status"); got != "running" {
		t.Errorf("run1 status = %q, want running", got)
	}

	// Run 2 should be marked error (lost)
	ref2, _ := app.FindRecordById("job_runs", run2.Id)
	if got := ref2.GetString("status"); got != "error" {
		t.Errorf("run2 status = %q, want error", got)
	}
	if got := ref2.GetString("output"); !strings.Contains(got, "job lost") {
		t.Errorf("run2 output = %q, want it to explain it was lost", got)
	}

	// Run 3 should still be running because it was updated within the last 1 minute
	ref3, _ := app.FindRecordById("job_runs", run3.Id)
	if got := ref3.GetString("status"); got != "running" {
		t.Errorf("run3 status = %q, want running", got)
	}
}

func TestMarkForgottenRunsStuckPending(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")

	worker := mustCreateRecord(t, app, "workers", map[string]any{
		"hostname":    "worker-1",
		"fingerprint": "fp1",
		"status":      "ACTIVE",
	})

	// Run 1: running, 2 hours old -> should become forgotten
	run1 := mustCreateRecord(t, app, "job_runs", map[string]any{
		"job":     jobRec.Id,
		"worker":  worker.Id,
		"status":  "running",
		"trigger": "manual",
	})
	updateJobRunTime(t, app, run1.Id, -2*time.Hour)

	// Run 2: pending, 20 minutes old -> should become error
	run2 := mustCreateRecord(t, app, "job_runs", map[string]any{
		"job":     jobRec.Id,
		"status":  "pending",
		"trigger": "manual",
	})
	updateJobRunTime(t, app, run2.Id, -20*time.Minute)

	// Run 3: pending, 5 minutes old -> should remain pending
	run3 := mustCreateRecord(t, app, "job_runs", map[string]any{
		"job":     jobRec.Id,
		"status":  "pending",
		"trigger": "manual",
	})

	s := NewScheduler(app, fakeJobDispatcher{workers: []string{worker.Id}}, filepath.Join(app.DataDir(), "repositories"))

	if err := s.MarkForgottenRuns(); err != nil {
		t.Fatalf("MarkForgottenRuns failed: %v", err)
	}

	// Run 1 -> forgotten
	ref1, _ := app.FindRecordById("job_runs", run1.Id)
	if got := ref1.GetString("status"); got != "forgotten" {
		t.Errorf("run1 status = %q, want forgotten", got)
	}

	// Run 2 -> error
	ref2, _ := app.FindRecordById("job_runs", run2.Id)
	if got := ref2.GetString("status"); got != "error" {
		t.Errorf("run2 status = %q, want error", got)
	}

	// Run 3 -> pending
	ref3, _ := app.FindRecordById("job_runs", run3.Id)
	if got := ref3.GetString("status"); got != "pending" {
		t.Errorf("run3 status = %q, want pending", got)
	}
}

func TestExecuteJobConvertsResources(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	writeJobFile(t, app.DataDir(), repo.Id, "job.yaml")
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")

	worker := mustCreateRecord(t, app, "workers", map[string]any{
		"hostname":    "worker-1",
		"fingerprint": "fp1",
		"status":      "ACTIVE",
	})

	capturedCmdChan := make(chan protocol.RunJobCommand, 1)
	dispatcher := capturingJobDispatcher{
		workers: []string{worker.Id},
		onDispatch: func(cmd interface{}) {
			if runJobCmd, ok := cmd.(protocol.RunJobCommand); ok {
				capturedCmdChan <- runJobCmd
			}
		},
	}

	s := NewScheduler(app, dispatcher, filepath.Join(app.DataDir(), "repositories"))
	s.executeJob(jobRec.Id, "manual", "test-user")

	var capturedCmd protocol.RunJobCommand
	select {
	case capturedCmd = <-capturedCmdChan:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for job to be dispatched")
	}

	if capturedCmd.CPUs != "0.5" {
		t.Errorf("expected CPUs '0.5', got %q", capturedCmd.CPUs)
	}

	if capturedCmd.MemoryLimit != "512m" {
		t.Errorf("expected MemoryLimit '512m', got %q", capturedCmd.MemoryLimit)
	}

	if capturedCmd.TimeoutSeconds != 600 {
		t.Errorf("expected TimeoutSeconds 600 (10m), got %d", capturedCmd.TimeoutSeconds)
	}

	// Wait until the background goroutine writes the "running" status to DB to avoid panic on cleanup
	pollStart := time.Now()
	for {
		run, err := app.FindRecordById("job_runs", capturedCmd.JobRunID)
		if err == nil && run.GetString("status") == "running" {
			break
		}
		if time.Since(pollStart) > 2*time.Second {
			t.Fatal("timed out waiting for job_run status to become running")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUpdateJobRunTruncatesOutput(t *testing.T) {
	app := newJobSchedulerTestApp(t)
	repo := createJobRepoRecord(t, app)
	jobRec := createScheduledJobRecord(t, app, repo.Id, "job.yaml")
	s := NewScheduler(app, fakeJobDispatcher{}, filepath.Join(app.DataDir(), "repositories"))

	// Increase the output validation limit in the database schema for this test
	col, err := app.FindCollectionByNameOrId("job_runs")
	if err != nil {
		t.Fatalf("failed to find collection: %v", err)
	}
	outputField := col.Fields.GetByName("output").(*core.TextField)
	outputField.Max = 2000000
	if err := app.Save(col); err != nil {
		t.Fatalf("failed to update schema: %v", err)
	}

	runID, err := s.createJobRun(jobRec.Id, "", "manual", "running")
	if err != nil {
		t.Fatalf("failed to create job run: %v", err)
	}

	// Create output larger than 1,000,000 characters with distinct head and tail
	headStr := "HEAD-JOBRUN123"
	tailStr := "TAIL-JOBRUN123"
	var sb strings.Builder
	sb.WriteString(headStr)
	for i := 0; i < 1200000-len(headStr)-len(tailStr); i++ {
		sb.WriteByte('A')
	}
	sb.WriteString(tailStr)
	largeOutput := sb.String()

	err = s.updateJobRun(runID, "success", largeOutput, 100, 10, 90)
	if err != nil {
		t.Fatalf("updateJobRun failed: %v", err)
	}

	// Reload the run and assert that it was truncated
	refreshed, err := app.FindRecordById("job_runs", runID)
	if err != nil {
		t.Fatalf("failed to reload job run: %v", err)
	}

	truncatedOutput := refreshed.GetString("output")
	if len(truncatedOutput) != 1000000 {
		t.Errorf("expected output length 1000000, got %d", len(truncatedOutput))
	}

	marker := "\n\n... [OUTPUT TRUNCATED FOR SIZE] ...\n\n"
	if strings.Count(truncatedOutput, marker) != 1 {
		t.Errorf("expected truncation marker to appear exactly once, but count was %d", strings.Count(truncatedOutput, marker))
	}

	if !strings.HasPrefix(truncatedOutput, headStr) {
		t.Errorf("expected truncated output to start with %q", headStr)
	}

	if !strings.HasSuffix(truncatedOutput, tailStr) {
		t.Errorf("expected truncated output to end with %q", tailStr)
	}

	headIdx := strings.Index(truncatedOutput, headStr)
	markerIdx := strings.Index(truncatedOutput, marker)
	tailIdx := strings.LastIndex(truncatedOutput, tailStr)
	if headIdx < 0 || markerIdx < 0 || tailIdx < 0 || !(headIdx < markerIdx && markerIdx < tailIdx) {
		t.Error("expected marker to be present between preserved head and tail")
	}
}
