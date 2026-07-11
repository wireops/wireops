package hooks

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/wireops/wireops/internal/sync"
)

func newWireopsImmutabilityTestApp(t *testing.T) (*tests.TestApp, *core.Collection) {
	t.Helper()
	t.Setenv("SECRET_KEY", "12345678901234567890123456789012")

	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	workers := core.NewBaseCollection("workers")
	if err := app.Save(workers); err != nil {
		t.Fatalf("save workers collection: %v", err)
	}

	stacks := core.NewBaseCollection("stacks")
	stacks.Fields.Add(&core.TextField{Name: "name"})
	stacks.Fields.Add(&core.RelationField{Name: "worker", CollectionId: workers.Id, MaxSelect: 1})
	stacks.Fields.Add(&core.TextField{Name: "compose_path"})
	stacks.Fields.Add(&core.TextField{Name: "compose_file"})
	stacks.Fields.Add(&core.BoolField{Name: "remove_orphans"})
	stacks.Fields.Add(&core.BoolField{Name: "force_pull"})
	stacks.Fields.Add(&core.NumberField{Name: "deploy_timeout_seconds"})
	stacks.Fields.Add(&core.TextField{Name: "wait_running_jobs"})
	stacks.Fields.Add(&core.NumberField{Name: "wait_running_jobs_timeout_seconds"})
	stacks.Fields.Add(&core.JSONField{Name: "worker_tags"})
	stacks.Fields.Add(&core.TextField{Name: "wireops_file_path"})
	stacks.Fields.Add(&core.TextField{Name: "config_source"})
	stacks.Fields.Add(&core.TextField{Name: "webhook_secret"})
	if err := app.Save(stacks); err != nil {
		t.Fatalf("save stacks collection: %v", err)
	}

	scheduler := sync.NewScheduler(app, nil)
	Register(app, scheduler, nil)

	worker := core.NewRecord(workers)
	if err := app.Save(worker); err != nil {
		t.Fatalf("save worker: %v", err)
	}

	return app, stacks
}

func newWireopsManagedStack(t *testing.T, app *tests.TestApp, stacks *core.Collection, workerID string) *core.Record {
	t.Helper()
	stack := core.NewRecord(stacks)
	stack.Set("name", "api")
	stack.Set("worker", workerID)
	stack.Set("compose_path", ".")
	stack.Set("compose_file", "docker-compose.yml")
	stack.Set("remove_orphans", true)
	stack.Set("force_pull", false)
	stack.Set("deploy_timeout_seconds", 300)
	stack.Set("wait_running_jobs", "always")
	stack.Set("worker_tags", []string{"prod"})
	stack.Set("wireops_file_path", "wireops.yaml")
	stack.Set("config_source", "wireops_file")
	if err := app.Save(stack); err != nil {
		t.Fatalf("save wireops-managed stack: %v", err)
	}

	// Reload from DB so Original() reflects the persisted values, matching
	// how a real PATCH request loads the record before applying updates
	// (NewRecord()'s originalData otherwise stays at pre-create defaults).
	reloaded, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload wireops-managed stack: %v", err)
	}
	return reloaded
}

func TestWireopsManagedStackRejectsComposeFieldEdits(t *testing.T) {
	app, stacks := newWireopsImmutabilityTestApp(t)
	workers, err := app.FindAllRecords("workers")
	if err != nil || len(workers) == 0 {
		t.Fatalf("expected a worker record: %v", err)
	}
	stack := newWireopsManagedStack(t, app, stacks, workers[0].Id)

	stack.Set("compose_path", "apps/other")
	err = app.Save(stack)
	if err == nil {
		t.Fatal("expected compose_path edit to be rejected")
	}
	if !strings.Contains(err.Error(), "managed by wireops.yaml") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWireopsManagedStackRejectsFlagEdits(t *testing.T) {
	app, stacks := newWireopsImmutabilityTestApp(t)
	workers, _ := app.FindAllRecords("workers")
	stack := newWireopsManagedStack(t, app, stacks, workers[0].Id)

	stack.Set("remove_orphans", false)
	if err := app.Save(stack); err == nil {
		t.Fatal("expected remove_orphans edit to be rejected")
	}

	stack2 := newWireopsManagedStack(t, app, stacks, workers[0].Id)
	stack2.Set("worker_tags", []string{"staging"})
	if err := app.Save(stack2); err == nil {
		t.Fatal("expected worker_tags edit to be rejected")
	}
}

func TestWireopsManagedStackAllowsUnrelatedFieldEdits(t *testing.T) {
	app, stacks := newWireopsImmutabilityTestApp(t)
	workers, _ := app.FindAllRecords("workers")
	stack := newWireopsManagedStack(t, app, stacks, workers[0].Id)

	stack.Set("name", "api-renamed")
	if err := app.Save(stack); err != nil {
		t.Fatalf("expected unrelated field edits to succeed, got: %v", err)
	}
}

func TestManualStackAllowsComposeFieldEdits(t *testing.T) {
	app, stacks := newWireopsImmutabilityTestApp(t)
	workers, _ := app.FindAllRecords("workers")

	stack := core.NewRecord(stacks)
	stack.Set("name", "manual-stack")
	stack.Set("worker", workers[0].Id)
	stack.Set("compose_path", ".")
	stack.Set("compose_file", "docker-compose.yml")
	stack.Set("config_source", "manual")
	if err := app.Save(stack); err != nil {
		t.Fatalf("save manual stack: %v", err)
	}

	reloaded, err := app.FindRecordById("stacks", stack.Id)
	if err != nil {
		t.Fatalf("reload manual stack: %v", err)
	}
	reloaded.Set("compose_path", "apps/other")
	if err := app.Save(reloaded); err != nil {
		t.Fatalf("expected compose_path edit to succeed for manual stack, got: %v", err)
	}
}
