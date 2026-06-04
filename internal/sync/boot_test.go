package sync

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func TestRecoverOrphanState(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	// Setup workers collection
	workers := core.NewBaseCollection("workers")
	workers.Fields.Add(&core.TextField{Name: "hostname", Required: true})
	workers.Fields.Add(&core.TextField{Name: "fingerprint", Required: true})
	workers.Fields.Add(&core.SelectField{Name: "status", Values: []string{"ACTIVE", "REVOKED"}, Required: true})
	if err := app.Save(workers); err != nil {
		t.Fatalf("failed to create workers collection: %v", err)
	}

	// Setup stacks collection
	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name", Required: true})
	stacks.Fields.Add(&core.SelectField{Name: "status", Values: []string{"active", "syncing", "paused", "error", "pending"}})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("failed to create stacks collection: %v", err)
	}

	// Setup sync_logs collection
	syncLogs := core.NewBaseCollection("sync_logs")
	syncLogs.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, Required: true, MaxSelect: 1})
	syncLogs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"running", "success", "error", "queued"}})
	syncLogs.Fields.Add(&core.TextField{Name: "output"})
	syncLogs.Fields.Add(&core.NumberField{Name: "duration_ms"})
	if err := app.Save(syncLogs); err != nil {
		t.Fatalf("failed to create sync_logs collection: %v", err)
	}

	// Setup job_runs collection
	jobRuns := core.NewBaseCollection("job_runs")
	jobRuns.Fields.Add(&core.SelectField{Name: "status", Values: []string{"pending", "running", "success", "error", "stalled", "forgotten"}})
	jobRuns.Fields.Add(&core.TextField{Name: "output"})
	jobRuns.Fields.Add(&core.NumberField{Name: "duration_ms"})
	jobRuns.Fields.Add(&core.DateField{Name: "started_at"})
	jobRuns.Fields.Add(&core.NumberField{Name: "queue_time_ms"})
	jobRuns.Fields.Add(&core.NumberField{Name: "execution_time_ms"})
	if err := app.Save(jobRuns); err != nil {
		t.Fatalf("failed to create job_runs collection: %v", err)
	}

	// Setup worker_commands collection
	workerCmds := core.NewBaseCollection("worker_commands")
	workerCmds.Fields.Add(&core.RelationField{Name: "worker", CollectionId: workers.Id, Required: true, MaxSelect: 1})
	workerCmds.Fields.Add(&core.TextField{Name: "command_id", Required: true})
	workerCmds.Fields.Add(&core.SelectField{Name: "status", Values: []string{"dispatched", "acked", "success", "error", "timed_out", "cancelled"}})
	workerCmds.Fields.Add(&core.JSONField{Name: "result"})
	if err := app.Save(workerCmds); err != nil {
		t.Fatalf("failed to create worker_commands collection: %v", err)
	}

	// Seed worker
	worker := core.NewRecord(workers)
	worker.Set("hostname", "worker1")
	worker.Set("fingerprint", "fp1")
	worker.Set("status", "ACTIVE")
	if err := app.Save(worker); err != nil {
		t.Fatalf("failed to save worker: %v", err)
	}

	// Seed stack stuck in "syncing"
	stuckStack := core.NewRecord(stacks)
	stuckStack.Set("name", "stuck-stack")
	stuckStack.Set("status", "syncing")
	if err := app.Save(stuckStack); err != nil {
		t.Fatalf("failed to save stack: %v", err)
	}

	// Seed sync_log stuck in "running"
	stuckSyncLog := core.NewRecord(syncLogs)
	stuckSyncLog.Set("stack", stuckStack.Id)
	stuckSyncLog.Set("status", "running")
	if err := app.Save(stuckSyncLog); err != nil {
		t.Fatalf("failed to save sync log: %v", err)
	}

	// Seed job_run stuck in "pending"
	stuckJobRun := core.NewRecord(jobRuns)
	stuckJobRun.Set("status", "pending")
	if err := app.Save(stuckJobRun); err != nil {
		t.Fatalf("failed to save job run: %v", err)
	}

	// Seed worker_command stuck in "dispatched"
	stuckWorkerCmd := core.NewRecord(workerCmds)
	stuckWorkerCmd.Set("worker", worker.Id)
	stuckWorkerCmd.Set("command_id", "cmd1")
	stuckWorkerCmd.Set("status", "dispatched")
	if err := app.Save(stuckWorkerCmd); err != nil {
		t.Fatalf("failed to save worker command: %v", err)
	}

	// Run recovery
	if err := RecoverOrphanState(app); err != nil {
		t.Fatalf("RecoverOrphanState failed: %v", err)
	}

	// Verify stack updated
	{
		refreshed, err := app.FindRecordById("stacks", stuckStack.Id)
		if err != nil {
			t.Fatalf("failed to reload stack: %v", err)
		}
		if got := refreshed.GetString("status"); got != "error" {
			t.Errorf("stack status = %q, want error", got)
		}
	}

	// Verify sync log updated
	{
		refreshed, err := app.FindRecordById("sync_logs", stuckSyncLog.Id)
		if err != nil {
			t.Fatalf("failed to reload sync log: %v", err)
		}
		if got := refreshed.GetString("status"); got != "error" {
			t.Errorf("sync log status = %q, want error", got)
		}
		if got := refreshed.GetString("output"); got == "" {
			t.Errorf("sync log output is empty, want error explanation")
		}
	}

	// Verify job run updated
	{
		refreshed, err := app.FindRecordById("job_runs", stuckJobRun.Id)
		if err != nil {
			t.Fatalf("failed to reload job run: %v", err)
		}
		if got := refreshed.GetString("status"); got != "error" {
			t.Errorf("job run status = %q, want error", got)
		}
	}

	// Verify worker command updated
	{
		refreshed, err := app.FindRecordById("worker_commands", stuckWorkerCmd.Id)
		if err != nil {
			t.Fatalf("failed to reload worker command: %v", err)
		}
		if got := refreshed.GetString("status"); got != "error" {
			t.Errorf("worker command status = %q, want error", got)
		}
	}
}
