package sync

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/wireops/wireops/internal/protocol"
)

func TestDeployCommandForRenderRequiresMigrationCapability(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	workers := core.NewBaseCollection("workers")
	workers.Fields.Add(&core.TextField{Name: "hostname"})
	workers.Fields.Add(&core.JSONField{Name: "capabilities"})
	if err := app.Save(workers); err != nil {
		t.Fatal(err)
	}
	worker := core.NewRecord(workers)
	worker.Set("hostname", "legacy-worker")
	if err := app.Save(worker); err != nil {
		t.Fatal(err)
	}

	r := &Reconciler{app: app}
	render := &RenderResult{
		ProjectName:         "pihole-red",
		PreviousProjectName: "legacy-project",
		PreviousCompose:     []byte("name: legacy-project\nservices: {}\n"),
	}
	base := protocol.DeployCommand{StackID: "stack-pihole"}
	if _, err := r.deployCommandForRender(worker.Id, base, render, true, false, false, false); err == nil || !strings.Contains(err.Error(), "must be updated") {
		t.Fatalf("error = %v, want worker update requirement", err)
	}

	worker.Set("capabilities", []string{protocol.CapabilityProjectIdentityMigrationV1})
	if err := app.Save(worker); err != nil {
		t.Fatal(err)
	}
	command, err := r.deployCommandForRender(worker.Id, base, render, true, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	redeploy, ok := command.(protocol.RedeployCommand)
	if !ok || redeploy.ProjectMigration == nil || !redeploy.RecreateContainers {
		t.Fatalf("migration command = %#v", command)
	}
}
