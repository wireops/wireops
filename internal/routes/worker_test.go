package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/protocol"
	wiresync "github.com/wireops/wireops/internal/sync"
	"github.com/wireops/wireops/internal/worker"
)

// mapConnectedDispatcher is a minimal sync.WorkerDispatcher stub whose
// IsConnected answer is controlled per-worker-ID, so a single test app can
// exercise both a connected and a disconnected worker in the same request.
type mapConnectedDispatcher struct {
	connected map[string]bool
}

func (d *mapConnectedDispatcher) Dispatch(ctx context.Context, workerID string, cmd interface{}) (protocol.CommandResult, error) {
	return protocol.CommandResult{}, nil
}

func (d *mapConnectedDispatcher) IsConnected(workerID string) bool {
	return d.connected[workerID]
}

func setupWorkerListTestApp(t *testing.T, dispatcher wiresync.WorkerDispatcher) (core.App, http.Handler) {
	t.Helper()
	app := newSetupTestApp(t)
	admin := createTestUser(t, app, "admin-workerlist@example.com", "password123", "admin")

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:  app,
			Auth: admin,
			Event: router.Event{
				Response: w,
				Request:  req,
			},
		}, nil
	})

	workerSvc := worker.NewService(app)
	workerServer := worker.NewWorkerServer(app, workerSvc)
	RegisterWorkerRoutes(r, app, workerSvc, dispatcher, workerServer)

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return app, mux
}

func createWorkerListTestWorker(t *testing.T, app core.App, hostname string, fields map[string]any) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("workers")
	if err != nil {
		t.Fatalf("find workers collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("hostname", hostname)
	rec.Set("status", "ACTIVE")
	rec.Set("policy_inherit", true)
	rec.Set("fingerprint", "worker-list-fp-"+core.GenerateDefaultRandomId())
	for k, v := range fields {
		rec.Set(k, v)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("create test worker: %v", err)
	}
	return rec
}

func getWorkerListStatus(t *testing.T, mux http.Handler, workerID string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/custom/workers", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var results []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, r := range results {
		if r["id"] == workerID {
			status, _ := r["status"].(string)
			return status
		}
	}
	t.Fatalf("worker %s not found in response", workerID)
	return ""
}

// TestWorkerListStatusDegradedRequiresReportedTelemetry covers the DEGRADED
// gate added alongside "Add DEGRADED worker status for connected-but-
// Docker-unreachable hosts": a connected worker whose last heartbeat
// reported docker_online=false is only DEGRADED once it has reported
// telemetry (os/docker_version) at least once — otherwise a worker that just
// connected and hasn't sent its first heartbeat would be misflagged before
// it had a chance to report DockerOnline=true.
func TestWorkerListStatusDegradedRequiresReportedTelemetry(t *testing.T) {
	dispatcher := &mapConnectedDispatcher{connected: map[string]bool{}}
	app, mux := setupWorkerListTestApp(t, dispatcher)

	withTelemetry := createWorkerListTestWorker(t, app, "worker-with-telemetry", map[string]any{
		"docker_online":  false,
		"os":             "linux",
		"docker_version": "27.0.0",
	})
	dispatcher.connected[withTelemetry.Id] = true

	withoutTelemetry := createWorkerListTestWorker(t, app, "worker-without-telemetry", map[string]any{
		"docker_online": false,
	})
	dispatcher.connected[withoutTelemetry.Id] = true

	if got := getWorkerListStatus(t, mux, withTelemetry.Id); got != "DEGRADED" {
		t.Fatalf("expected connected worker with reported telemetry and docker offline to be DEGRADED, got %q", got)
	}
	if got := getWorkerListStatus(t, mux, withoutTelemetry.Id); got == "DEGRADED" {
		t.Fatal("expected connected worker with no reported telemetry to not be flagged DEGRADED before its first heartbeat")
	}
}

// TestWorkerListStatusDisconnectedActiveWorkerIsOffline covers the other
// branch of the same status derivation: a worker whose DB status is still
// ACTIVE (never explicitly marked otherwise) but whose websocket is not
// currently connected must be reported OFFLINE, not ACTIVE or DEGRADED.
func TestWorkerListStatusDisconnectedActiveWorkerIsOffline(t *testing.T) {
	dispatcher := &mapConnectedDispatcher{connected: map[string]bool{}}
	app, mux := setupWorkerListTestApp(t, dispatcher)

	disconnected := createWorkerListTestWorker(t, app, "worker-disconnected", map[string]any{
		"docker_online":  false,
		"os":             "linux",
		"docker_version": "27.0.0",
	})
	// dispatcher.connected has no entry for this worker, so IsConnected reports false.

	if got := getWorkerListStatus(t, mux, disconnected.Id); got != "OFFLINE" {
		t.Fatalf("expected disconnected worker to be OFFLINE, got %q", got)
	}
}

func TestValidateRetentionDays(t *testing.T) {
	validAudit := 1
	validJob := 7
	validTerminal := 30
	zero := 0
	negative := -1

	tests := []struct {
		name         string
		auditDays    *int
		jobDays      *int
		terminalDays *int
		wantErr      bool
	}{
		{
			name:    "missing values are allowed for backwards compatible partial updates",
			wantErr: false,
		},
		{
			name:         "positive values",
			auditDays:    &validAudit,
			jobDays:      &validJob,
			terminalDays: &validTerminal,
			wantErr:      false,
		},
		{
			name:         "zero audit retention is rejected",
			auditDays:    &zero,
			jobDays:      &validJob,
			terminalDays: &validTerminal,
			wantErr:      true,
		},
		{
			name:         "negative job retention is rejected",
			auditDays:    &validAudit,
			jobDays:      &negative,
			terminalDays: &validTerminal,
			wantErr:      true,
		},
		{
			name:         "negative terminal retention is rejected",
			auditDays:    &validAudit,
			jobDays:      &validJob,
			terminalDays: &negative,
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRetentionDays(tc.auditDays, tc.jobDays, tc.terminalDays)
			if (err != nil) != tc.wantErr {
				t.Fatalf("expected error=%v, got %v", tc.wantErr, err)
			}
		})
	}
}
