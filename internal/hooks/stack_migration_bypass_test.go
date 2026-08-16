package hooks

import "testing"

func TestWireopsManagedStackMigrationBypassAllowsComposeFieldEdits(t *testing.T) {
	app, stacks := newWireopsImmutabilityTestApp(t)
	workers, err := app.FindAllRecords("workers")
	if err != nil || len(workers) == 0 {
		t.Fatalf("expected a worker record: %v", err)
	}
	stack := newWireopsManagedStack(t, app, stacks, workers[0].Id)

	stack.Set("compose_path", "apps/other")
	err = WithMigrationBypass(stack.Id, func() error {
		return app.Save(stack)
	})
	if err != nil {
		t.Fatalf("expected compose_path edit under migration bypass to succeed, got: %v", err)
	}
}

func TestWireopsManagedStackMigrationBypassClearedAfterUse(t *testing.T) {
	app, stacks := newWireopsImmutabilityTestApp(t)
	workers, _ := app.FindAllRecords("workers")
	stack := newWireopsManagedStack(t, app, stacks, workers[0].Id)

	if err := WithMigrationBypass(stack.Id, func() error { return nil }); err != nil {
		t.Fatalf("bypass fn returned error: %v", err)
	}

	if isMigrationBypass(stack.Id) {
		t.Fatal("expected bypass to be cleared after WithMigrationBypass returns")
	}

	stack.Set("compose_path", "apps/other")
	if err := app.Save(stack); err == nil {
		t.Fatal("expected compose_path edit to be rejected once bypass is cleared")
	}
}
