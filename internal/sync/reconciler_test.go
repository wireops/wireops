package sync

import (
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func TestUpdateSyncLogPersistsSuccessStatus(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)
	r := &Reconciler{app: app}

	logRec, err := r.createSyncLog(stack.Id, "manual", "abc123", "test sync")
	if err != nil {
		t.Fatalf("createSyncLog failed: %v", err)
	}
	if err := r.updateSyncLog(logRec.Id, "success", "ok", 42); err != nil {
		t.Fatalf("updateSyncLog failed: %v", err)
	}

	refreshed, err := app.FindRecordById("sync_logs", logRec.Id)
	if err != nil {
		t.Fatalf("failed to reload sync log: %v", err)
	}
	if got := refreshed.GetString("status"); got != "success" {
		t.Fatalf("sync log status = %q, want success", got)
	}
	if got := refreshed.GetInt("duration_ms"); got != 42 {
		t.Fatalf("duration_ms = %d, want 42", got)
	}
}

func TestQueuePendingReconcilePersistsRecordAndQueuedLog(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)
	r := &Reconciler{app: app}

	if err := r.queuePendingReconcile(stack.Id, "manual", "abc123"); err != nil {
		t.Fatalf("queuePendingReconcile failed: %v", err)
	}

	pending, err := app.FindAllRecords("stack_pending_reconciles", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		t.Fatalf("failed to query pending reconciles: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending reconciles = %d, want 1", len(pending))
	}

	logs, err := app.FindAllRecords("sync_logs", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		t.Fatalf("failed to query sync logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("sync logs = %d, want 1", len(logs))
	}
	if got := logs[0].GetString("status"); got != "queued" {
		t.Fatalf("sync log status = %q, want queued", got)
	}
}

func newReconcilerPhase1TestApp(t *testing.T) (*tests.TestApp, *core.Record) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name", Required: true})
	stacks.Fields.Add(&core.SelectField{Name: "status", Values: []string{"active", "syncing", "paused", "error", "pending"}})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("failed to create stacks collection: %v", err)
	}

	syncLogs := core.NewBaseCollection("sync_logs")
	syncLogs.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, Required: true, MaxSelect: 1})
	syncLogs.Fields.Add(&core.SelectField{Name: "trigger", Values: []string{"cron", "webhook", "manual", "redeploy", "rollback", "transfer", "queue"}})
	syncLogs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"running", "success", "error", "queued"}})
	syncLogs.Fields.Add(&core.TextField{Name: "commit_sha"})
	syncLogs.Fields.Add(&core.TextField{Name: "commit_message"})
	syncLogs.Fields.Add(&core.TextField{Name: "output"})
	syncLogs.Fields.Add(&core.NumberField{Name: "duration_ms"})
	if err := app.Save(syncLogs); err != nil {
		t.Fatalf("failed to create sync_logs collection: %v", err)
	}

	pending := core.NewBaseCollection("stack_pending_reconciles")
	pending.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, Required: true, MaxSelect: 1})
	pending.Fields.Add(&core.SelectField{Name: "trigger", Values: []string{"cron", "webhook", "manual", "queue"}})
	pending.Fields.Add(&core.TextField{Name: "commit_sha"})
	pending.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	if err := app.Save(pending); err != nil {
		t.Fatalf("failed to create stack_pending_reconciles collection: %v", err)
	}

	stack := core.NewRecord(stacks)
	stack.Set("name", "stack")
	stack.Set("status", "active")
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to create stack: %v", err)
	}
	return app, stack
}
