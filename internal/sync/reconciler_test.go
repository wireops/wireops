package sync

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/constants"
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

func TestLogNoopSyncPersistsNoopStatus(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)
	r := &Reconciler{app: app}

	if err := r.logNoopSync(context.Background(), stack, stack.Id, "manual", "abc123", "No changes", "No changes detected."); err != nil {
		t.Fatalf("logNoopSync failed: %v", err)
	}

	logs, err := app.FindAllRecords("sync_logs", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		t.Fatalf("failed to query sync logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("sync logs = %d, want 1", len(logs))
	}
	if got := logs[0].GetString("status"); got != "noop" {
		t.Fatalf("sync log status = %q, want noop", got)
	}
	if got := logs[0].GetString("output"); got != "No changes detected." {
		t.Fatalf("sync log output = %q, want no-op message", got)
	}
}

func TestLogFailureWithPhaseRecordsFailingPhase(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)
	r := &Reconciler{app: app}

	start := time.Now().Add(-2 * time.Second)
	if err := r.logFailureWithPhase(stack.Id, "manual", "abc123", "git clone failed: connection refused", constants.PhaseGitFetch, start); err != nil {
		t.Fatalf("logFailureWithPhase failed: %v", err)
	}

	logs, err := app.FindAllRecords("sync_logs", dbx.HashExp{"stack": stack.Id, "status": "error"})
	if err != nil || len(logs) != 1 {
		t.Fatalf("sync logs = %d, err=%v, want 1 error log", len(logs), err)
	}

	phases, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": logs[0].Id})
	if err != nil {
		t.Fatalf("failed to query phases: %v", err)
	}
	if len(phases) != 1 {
		t.Fatalf("phase rows = %d, want 1", len(phases))
	}
	if got := phases[0].GetString("phase"); got != constants.PhaseGitFetch {
		t.Fatalf("phase = %q, want %q", got, constants.PhaseGitFetch)
	}
	if got := phases[0].GetString("status"); got != constants.PhaseStatusError {
		t.Fatalf("phase status = %q, want error", got)
	}
	if got := phases[0].GetString("detail"); !strings.Contains(got, "connection refused") {
		t.Fatalf("phase detail = %q, want it to contain the error message", got)
	}
	if got := phases[0].GetInt("duration_ms"); got < 1900 {
		t.Fatalf("phase duration_ms = %d, want roughly >= 2000 (measured from the passed-in start)", got)
	}
}

func TestLogNoopSyncWithPhasesBackfillsGitFetchAndRender(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)
	r := &Reconciler{app: app}

	gitFetchStart := time.Now().Add(-3 * time.Second)
	renderStart := time.Now().Add(-1 * time.Second)
	err := r.logNoopSyncWithPhases(context.Background(), stack, stack.Id, "manual", "abc123", "no changes", "No changes detected.", nil, gitFetchStart, 1500, renderStart, 500)
	if err != nil {
		t.Fatalf("logNoopSyncWithPhases failed: %v", err)
	}

	logs, err := app.FindAllRecords("sync_logs", dbx.HashExp{"stack": stack.Id, "status": "noop"})
	if err != nil || len(logs) != 1 {
		t.Fatalf("sync logs = %d, err=%v, want 1 noop log", len(logs), err)
	}

	phases, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": logs[0].Id})
	if err != nil {
		t.Fatalf("failed to query phases: %v", err)
	}
	byPhase := map[string]*core.Record{}
	for _, p := range phases {
		byPhase[p.GetString("phase")] = p
	}
	if len(byPhase) != 3 {
		t.Fatalf("phase rows = %d, want 3 (git_fetch, policy_check, render), got %+v", len(byPhase), byPhase)
	}
	if got := byPhase[constants.PhaseGitFetch].GetInt("duration_ms"); got != 1500 {
		t.Fatalf("git_fetch duration_ms = %d, want 1500", got)
	}
	if got := byPhase[constants.PhasePolicyCheck].GetString("status"); got != constants.PhaseStatusSkipped {
		t.Fatalf("policy_check status = %q, want skipped", got)
	}
	if got := byPhase[constants.PhaseRender].GetString("status"); got != constants.PhaseStatusSkipped {
		t.Fatalf("render status = %q, want skipped", got)
	}
}

func TestRecordWorkerAckAndComposeUpPhasesWithAckedCommand(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)
	r := &Reconciler{app: app}

	syncLog, err := r.createSyncLog(stack.Id, "manual", "abc123", "test")
	if err != nil {
		t.Fatalf("createSyncLog failed: %v", err)
	}

	wcCol, err := app.FindCollectionByNameOrId("worker_commands")
	if err != nil {
		t.Fatalf("failed to find worker_commands: %v", err)
	}
	dispatchStart := time.Now().Add(-2 * time.Second)
	ackedAt := dispatchStart.Add(300 * time.Millisecond)
	wc := core.NewRecord(wcCol)
	wc.Set("command_id", syncLog.Id)
	wc.Set("status", "success")
	wc.Set("acked_at", ackedAt)
	if err := app.Save(wc); err != nil {
		t.Fatalf("failed to create worker_commands row: %v", err)
	}

	pt := newPhaseTracker(app, syncLog.Id)
	r.recordWorkerAckAndComposeUpPhases(pt, syncLog.Id, dispatchStart, 1200)

	phases, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": syncLog.Id})
	if err != nil {
		t.Fatalf("failed to query phases: %v", err)
	}
	byPhase := map[string]*core.Record{}
	for _, p := range phases {
		byPhase[p.GetString("phase")] = p
	}
	if len(byPhase) != 2 {
		t.Fatalf("phase rows = %d, want 2 (worker_ack, compose_up)", len(byPhase))
	}
	ackPhase := byPhase[constants.PhaseWorkerAck]
	if ackPhase.GetString("status") != constants.PhaseStatusSuccess {
		t.Fatalf("worker_ack status = %q, want success", ackPhase.GetString("status"))
	}
	if got := ackPhase.GetInt("duration_ms"); got < 250 || got > 450 {
		t.Fatalf("worker_ack duration_ms = %d, want ~300", got)
	}
	composePhase := byPhase[constants.PhaseComposeUp]
	if got := composePhase.GetInt("duration_ms"); got != 1200 {
		t.Fatalf("compose_up duration_ms = %d, want 1200", got)
	}
}

func TestRecordWorkerAckAndComposeUpPhasesSkipsWhenDataMissing(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)
	r := &Reconciler{app: app}

	syncLog, err := r.createSyncLog(stack.Id, "manual", "abc123", "test")
	if err != nil {
		t.Fatalf("createSyncLog failed: %v", err)
	}

	pt := newPhaseTracker(app, syncLog.Id)
	r.recordWorkerAckAndComposeUpPhases(pt, syncLog.Id, time.Now(), 0)

	phases, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": syncLog.Id})
	if err != nil {
		t.Fatalf("failed to query phases: %v", err)
	}
	byPhase := map[string]*core.Record{}
	for _, p := range phases {
		byPhase[p.GetString("phase")] = p
	}
	if got := byPhase[constants.PhaseWorkerAck].GetString("status"); got != constants.PhaseStatusSkipped {
		t.Fatalf("worker_ack status = %q, want skipped (no worker_commands row)", got)
	}
	if got := byPhase[constants.PhaseComposeUp].GetString("status"); got != constants.PhaseStatusSkipped {
		t.Fatalf("compose_up status = %q, want skipped (composeUpMs=0)", got)
	}
}

func TestIsTransientGitError(t *testing.T) {
	cases := []error{
		context.DeadlineExceeded,
		errors.New("failed to list remote refs: ssh: handshake failed: read tcp 172.18.0.4:51034->4.228.31.150:22: read: connection timed out"),
		errors.New("git fetch failed: ssh: unexpected packet in response to channel open: <nil>"),
	}

	for _, err := range cases {
		if !isTransientGitError(err) {
			t.Fatalf("expected %q to be treated as transient", err)
		}
	}

	if isTransientGitError(errors.New("authentication required")) {
		t.Fatal("authentication errors should not be treated as transient")
	}
}

func TestForceRedeployStackAllowsNilNotifierOnEarlyFailure(t *testing.T) {
	app, stack := newForceRedeployNilNotifierTestApp(t)
	r := &Reconciler{app: app}

	err := r.ForceRedeployStack(context.Background(), stack.Id, true, false, false)
	if err == nil {
		t.Fatal("ForceRedeployStack succeeded, want invalid compose_path error")
	}
	if !strings.Contains(err.Error(), "invalid compose_path") {
		t.Fatalf("ForceRedeployStack error = %q, want invalid compose_path", err)
	}

	logs, err := app.FindAllRecords("sync_logs", dbx.HashExp{"stack": stack.Id})
	if err != nil {
		t.Fatalf("failed to query sync logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("sync logs = %d, want 1", len(logs))
	}
	if got := logs[0].GetString("trigger"); got != "redeploy" {
		t.Fatalf("sync log trigger = %q, want redeploy", got)
	}
	if got := logs[0].GetString("status"); got != "error" {
		t.Fatalf("sync log status = %q, want error", got)
	}

	phases, err := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": logs[0].Id})
	if err != nil {
		t.Fatalf("failed to query phases: %v", err)
	}
	byPhase := map[string]*core.Record{}
	for _, p := range phases {
		byPhase[p.GetString("phase")] = p
	}
	if got := byPhase[constants.PhaseGitFetch].GetString("status"); got != constants.PhaseStatusSkipped {
		t.Fatalf("git_fetch status = %q, want skipped (force redeploy has no git fetch step)", got)
	}
	if got := byPhase[constants.PhaseRender].GetString("status"); got != constants.PhaseStatusError {
		t.Fatalf("render status = %q, want error (invalid compose_path failure)", got)
	}
}

func TestForceRedeployStackEarlyFailureLeavesDeployedStateUntouched(t *testing.T) {
	app, stack := newForceRedeployNilNotifierTestApp(t)
	r := &Reconciler{app: app}

	err := r.ForceRedeployStack(context.Background(), stack.Id, true, false, false)
	if err == nil {
		t.Fatal("ForceRedeployStack succeeded, want invalid compose_path error")
	}

	refreshed, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("failed to reload stack: %v", err)
	}
	if got := refreshed.GetInt("deployed_version"); got != 0 {
		t.Fatalf("deployed_version = %d, want 0 (unset) on early failure", got)
	}
	if got := refreshed.GetString("deployed_commit"); got != "" {
		t.Fatalf("deployed_commit = %q, want empty on early failure", got)
	}
	if got := refreshed.GetString("deployed_checksum"); got != "" {
		t.Fatalf("deployed_checksum = %q, want empty on early failure", got)
	}
	if got := refreshed.GetString("status"); got != "error" {
		t.Fatalf("status = %q, want error", got)
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
	syncLogs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"running", "success", "error", "queued", "noop"}})
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

	workerCommands := core.NewBaseCollection("worker_commands")
	workerCommands.Fields.Add(&core.TextField{Name: "command_id", Required: true})
	workerCommands.Fields.Add(&core.TextField{Name: "status"})
	workerCommands.Fields.Add(&core.DateField{Name: "acked_at"})
	if err := app.Save(workerCommands); err != nil {
		t.Fatalf("failed to create worker_commands collection: %v", err)
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

func newForceRedeployNilNotifierTestApp(t *testing.T) (*tests.TestApp, *core.Record) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	repos := core.NewBaseCollection("repositories")
	repos.Fields.Add(&core.TextField{Name: "name"})
	repos.Fields.Add(&core.TextField{Name: "last_commit_sha"})
	if err := app.Save(repos); err != nil {
		t.Fatalf("failed to create repositories collection: %v", err)
	}

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name", Required: true})
	stacks.Fields.Add(&core.RelationField{Name: "repository", CollectionId: repos.Id, Required: true, MaxSelect: 1})
	stacks.Fields.Add(&core.TextField{Name: "compose_path"})
	stacks.Fields.Add(&core.SelectField{Name: "status", Values: []string{"active", "syncing", "paused", "error", "pending"}})
	stacks.Fields.Add(&core.NumberField{Name: "deployed_version"})
	stacks.Fields.Add(&core.TextField{Name: "deployed_commit"})
	stacks.Fields.Add(&core.TextField{Name: "deployed_checksum"})
	stacks.Fields.Add(&core.DateField{Name: "deployed_at"})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("failed to create stacks collection: %v", err)
	}

	syncLogs := core.NewBaseCollection("sync_logs")
	syncLogs.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, Required: true, MaxSelect: 1})
	syncLogs.Fields.Add(&core.SelectField{Name: "trigger", Values: []string{"cron", "webhook", "manual", "redeploy", "rollback", "transfer", "queue"}})
	syncLogs.Fields.Add(&core.SelectField{Name: "status", Values: []string{"running", "success", "error", "queued", "noop"}})
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

	repo := core.NewRecord(repos)
	repo.Set("name", "repo")
	repo.Set("last_commit_sha", "abc123")
	if err := app.Save(repo); err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}

	stack := core.NewRecord(stacks)
	stack.Set("name", "stack")
	stack.Set("repository", repo.Id)
	stack.Set("compose_path", "../invalid")
	stack.Set("status", "active")
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to create stack: %v", err)
	}

	return app, stack
}

func TestUpdateSyncLogTruncatesOutput(t *testing.T) {
	app, stack := newReconcilerPhase1TestApp(t)
	r := &Reconciler{app: app}

	logRec, err := r.createSyncLog(stack.Id, "manual", "abc123", "test sync")
	if err != nil {
		t.Fatalf("createSyncLog failed: %v", err)
	}

	// Make sure schema allows large output during testing
	col, err := app.FindCollectionByNameOrId("sync_logs")
	if err != nil {
		t.Fatalf("failed to find sync_logs collection: %v", err)
	}
	outputField := col.Fields.GetByName("output").(*core.TextField)
	outputField.Max = 2000000
	if err := app.Save(col); err != nil {
		t.Fatalf("failed to update schema: %v", err)
	}

	// Create output larger than 1,000,000 characters with distinct head and tail
	headStr := "HEAD-SYNCLOG123"
	tailStr := "TAIL-SYNCLOG123"
	var sb strings.Builder
	sb.WriteString(headStr)
	for i := 0; i < 1200000-len(headStr)-len(tailStr); i++ {
		sb.WriteByte('A')
	}
	sb.WriteString(tailStr)
	largeOutput := sb.String()

	if err := r.updateSyncLog(logRec.Id, "success", largeOutput, 42); err != nil {
		t.Fatalf("updateSyncLog failed: %v", err)
	}

	// Reload the run and assert that it was truncated
	refreshed, err := app.FindRecordById("sync_logs", logRec.Id)
	if err != nil {
		t.Fatalf("failed to reload sync log: %v", err)
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
