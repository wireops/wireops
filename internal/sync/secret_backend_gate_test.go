package sync

import (
	"context"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/constants"
)

// newSecretBackendGateTestApp builds the minimal fixture needed to exercise
// the checkSecretBackends pre-flight gate. It deliberately omits the
// "repositories" collection — since the gate runs before any repo/git
// lookup, a passing test here also proves the gate actually short-circuits
// before that code is ever reached (a bug that skipped the gate would fail
// with a "collection not found" error instead of the expected message).
func newSecretBackendGateTestApp(t *testing.T) (*tests.TestApp, *core.Record) {
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

	stackEnv := core.NewBaseCollection("stack_env_vars")
	stackEnv.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, Required: true, MaxSelect: 1})
	stackEnv.Fields.Add(&core.TextField{Name: "key"})
	stackEnv.Fields.Add(&core.TextField{Name: "value"})
	stackEnv.Fields.Add(&core.BoolField{Name: "secret"})
	stackEnv.Fields.Add(&core.TextField{Name: "secret_provider"})
	if err := app.Save(stackEnv); err != nil {
		t.Fatalf("failed to create stack_env_vars collection: %v", err)
	}

	integrations := core.NewBaseCollection("integrations")
	integrations.Fields.Add(&core.TextField{Name: "slug", Required: true})
	integrations.Fields.Add(&core.BoolField{Name: "enabled"})
	integrations.Fields.Add(&core.JSONField{Name: "config"})
	if err := app.Save(integrations); err != nil {
		t.Fatalf("failed to create integrations collection: %v", err)
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

	stack := core.NewRecord(stacks)
	stack.Set("name", "stack")
	stack.Set("status", "active")
	if err := app.Save(stack); err != nil {
		t.Fatalf("failed to create stack: %v", err)
	}

	envVarCol, _ := app.FindCollectionByNameOrId("stack_env_vars")
	envVar := core.NewRecord(envVarCol)
	envVar.Set("stack", stack.Id)
	envVar.Set("key", "DB_PASS")
	envVar.Set("value", "secret/data/myapp#DB_PASS")
	envVar.Set("secret", true)
	envVar.Set("secret_provider", "vault")
	if err := app.Save(envVar); err != nil {
		t.Fatalf("failed to create secret env var: %v", err)
	}

	integrationsCol, _ := app.FindCollectionByNameOrId("integrations")
	vaultIntegration := core.NewRecord(integrationsCol)
	vaultIntegration.Set("slug", "vault")
	vaultIntegration.Set("enabled", false)
	if err := app.Save(vaultIntegration); err != nil {
		t.Fatalf("failed to create disabled vault integration: %v", err)
	}

	return app, stack
}

func assertBlockedBySecretBackendGate(t *testing.T, app core.App, stack *core.Record, err error, trigger string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "vault") || !strings.Contains(err.Error(), "DB_PASS") {
		t.Fatalf("error %q does not name the disabled backend/env var", err.Error())
	}

	logs, findErr := app.FindAllRecords("sync_logs", dbx.HashExp{"stack": stack.Id, "trigger": trigger})
	if findErr != nil {
		t.Fatalf("failed to query sync logs: %v", findErr)
	}
	if len(logs) != 1 {
		t.Fatalf("sync logs = %d, want 1", len(logs))
	}
	if got := logs[0].GetString("status"); got != "error" {
		t.Fatalf("sync log status = %q, want error", got)
	}

	phases, findErr := app.FindAllRecords("sync_log_phases", dbx.HashExp{"sync_log": logs[0].Id})
	if findErr != nil {
		t.Fatalf("failed to query phases: %v", findErr)
	}
	if len(phases) != 1 {
		t.Fatalf("phases = %d, want exactly 1 (policy_check only — gate must fire before git_fetch/render)", len(phases))
	}
	if got := phases[0].GetString("phase"); got != constants.PhasePolicyCheck {
		t.Fatalf("phase = %q, want %q", got, constants.PhasePolicyCheck)
	}
	if got := phases[0].GetString("status"); got != constants.PhaseStatusError {
		t.Fatalf("phase status = %q, want error", got)
	}

	refreshed, findErr := app.FindRecordById("stacks", stack.Id)
	if findErr != nil {
		t.Fatalf("failed to reload stack: %v", findErr)
	}
	if got := refreshed.GetString("status"); got != "error" {
		t.Fatalf("stack status = %q, want error", got)
	}
}

func TestReconcileStackBlockedByDisabledSecretBackend(t *testing.T) {
	app, stack := newSecretBackendGateTestApp(t)
	r := &Reconciler{app: app}

	err := r.ReconcileStack(context.Background(), stack.Id, "manual", 1)

	assertBlockedBySecretBackendGate(t, app, stack, err, "manual")
}

func TestForceRedeployStackBlockedByDisabledSecretBackend(t *testing.T) {
	app, stack := newSecretBackendGateTestApp(t)
	r := &Reconciler{app: app}

	err := r.ForceRedeployStack(context.Background(), stack.Id, false, false, false)

	assertBlockedBySecretBackendGate(t, app, stack, err, "redeploy")
}

func TestRollbackStackBlockedByDisabledSecretBackend(t *testing.T) {
	app, stack := newSecretBackendGateTestApp(t)
	r := &Reconciler{app: app}

	err := r.RollbackStack(context.Background(), stack.Id, "deadbeef")

	assertBlockedBySecretBackendGate(t, app, stack, err, "manual")
}

func TestReconcileStackProceedsWhenSecretBackendEnabled(t *testing.T) {
	app, stack := newSecretBackendGateTestApp(t)
	recs, err := app.FindAllRecords("integrations", dbx.HashExp{"slug": "vault"})
	if err != nil || len(recs) == 0 {
		t.Fatalf("failed to find vault integration: %v", err)
	}
	recs[0].Set("enabled", true)
	if err := app.Save(recs[0]); err != nil {
		t.Fatalf("failed to enable vault integration: %v", err)
	}

	r := &Reconciler{app: app}
	err = r.ReconcileStack(context.Background(), stack.Id, "manual", 1)

	// With the backend enabled, the gate must not block — the deploy still
	// fails shortly after (no "repositories" collection in this fixture),
	// but critically NOT with the secret-backend message, and NOT tagged as
	// a clean single-phase policy_check failure.
	if err == nil {
		t.Fatal("expected an error from the missing repositories collection, got nil")
	}
	if strings.Contains(err.Error(), "secret backend") {
		t.Fatalf("gate incorrectly blocked despite the backend being enabled: %v", err)
	}
}
