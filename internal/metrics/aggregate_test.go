package metrics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/wireops/wireops/internal/protocol"
)

type fakeDispatcher struct {
	connected map[string]bool
	outputs   map[string]string
	errors    map[string]error
}

func (f *fakeDispatcher) Dispatch(_ context.Context, workerID string, _ interface{}) (protocol.CommandResult, error) {
	if err, ok := f.errors[workerID]; ok {
		return protocol.CommandResult{}, err
	}
	if out, ok := f.outputs[workerID]; ok {
		return protocol.CommandResult{Output: out}, nil
	}
	return protocol.CommandResult{}, errors.New("no output configured")
}

func (f *fakeDispatcher) IsConnected(workerID string) bool {
	return f.connected[workerID]
}

func TestInjectWorkerLabelsAddsLabelsToBareMetric(t *testing.T) {
	input := "wireops_worker_active_tasks 2\n"
	got := InjectWorkerLabels(input, "wk1", "node-a")

	if !strings.Contains(got, `wireops_worker_active_tasks{worker="wk1",hostname="node-a"} 2`) {
		t.Fatalf("expected labels injected, got:\n%s", got)
	}
}

func TestInjectWorkerLabelsAppendsToExistingLabels(t *testing.T) {
	input := `wireops_worker_tasks_total{type="deploy"} 5` + "\n"
	got := InjectWorkerLabels(input, "wk2", "node-b")

	want := `wireops_worker_tasks_total{type="deploy",worker="wk2",hostname="node-b"} 5`
	if !strings.Contains(got, want) {
		t.Fatalf("expected appended labels, got:\n%s", got)
	}
}

func TestInjectWorkerLabelsSkipsCommentsAndBlankLines(t *testing.T) {
	input := "# HELP wireops_worker_connected test\n\nwireops_worker_connected 1\n"
	got := InjectWorkerLabels(input, "wk3", "node-c")

	if strings.Contains(got, "# HELP") {
		t.Fatal("comments should not appear in output")
	}
	if !strings.Contains(got, `wireops_worker_connected{worker="wk3",hostname="node-c"} 1`) {
		t.Fatalf("expected sample line with labels, got:\n%s", got)
	}
}

func newMetricsTestApp(t *testing.T) core.App {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	if _, err := app.FindCollectionByNameOrId("workers"); err != nil {
		col := core.NewBaseCollection("workers")
		col.Fields.Add(&core.TextField{Name: "hostname", Required: true})
		col.Fields.Add(&core.TextField{Name: "fingerprint", Required: true})
		if err := app.Save(col); err != nil {
			t.Fatalf("save workers collection: %v", err)
		}
	}
	return app
}

func TestCollectAllOfflineWorkerEmitsZeroConnected(t *testing.T) {
	app := newMetricsTestApp(t)

	workersCol, _ := app.FindCollectionByNameOrId("workers")
	rec := core.NewRecord(workersCol)
	rec.Set("hostname", "offline-node")
	rec.Set("fingerprint", "fp-offline")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save worker: %v", err)
	}

	dispatcher := &fakeDispatcher{
		connected: map[string]bool{rec.Id: false},
	}

	out, err := CollectAll(context.Background(), app, dispatcher)
	if err != nil {
		t.Fatalf("CollectAll: %v", err)
	}

	want := `wireops_worker_connected{worker="` + rec.Id + `",hostname="offline-node"} 0`
	if !strings.Contains(out, want) {
		t.Fatalf("expected offline connected gauge, got:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE wireops_worker_connected gauge") {
		t.Fatal("expected aggregate metric headers")
	}
}

func TestCollectAllConnectedWorkerInjectsLabels(t *testing.T) {
	app := newMetricsTestApp(t)

	workersCol, _ := app.FindCollectionByNameOrId("workers")
	rec := core.NewRecord(workersCol)
	rec.Set("hostname", "online-node")
	rec.Set("fingerprint", "fp-online")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save worker: %v", err)
	}

	dispatcher := &fakeDispatcher{
		connected: map[string]bool{rec.Id: true},
		outputs: map[string]string{
			rec.Id: "wireops_worker_active_jobs 1\n",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := CollectAll(ctx, app, dispatcher)
	if err != nil {
		t.Fatalf("CollectAll: %v", err)
	}

	want := `wireops_worker_active_jobs{worker="` + rec.Id + `",hostname="online-node"} 1`
	if !strings.Contains(out, want) {
		t.Fatalf("expected rewritten worker metrics, got:\n%s", out)
	}
}

func TestCollectWorkerOffline(t *testing.T) {
	dispatcher := &fakeDispatcher{connected: map[string]bool{"wk1": false}}
	_, err := CollectWorker(context.Background(), dispatcher, "wk1")
	if err == nil {
		t.Fatal("expected error for offline worker")
	}
}
