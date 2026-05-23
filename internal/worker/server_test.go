package worker

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/tests"
)

func newWorkerServerTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	ensureWorkerCollections(t, app)
	t.Cleanup(func() { app.Cleanup() })
	return app
}

func TestHandleRegisterRejectsMissingToken(t *testing.T) {
	app := newWorkerServerTestApp(t)
	svc := NewService(app)
	server := NewWorkerServer(app, svc)

	body, _ := json.Marshal(map[string]any{
		"hostname": "worker-a",
		"version":  "1.0.0",
		"tags":     []string{"edge"},
	})

	req := httptest.NewRequest(http.MethodPost, "/worker/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleRegisterActivatesStagingToken(t *testing.T) {
	app := newWorkerServerTestApp(t)
	svc := NewService(app)
	server := NewWorkerServer(app, svc)

	token, _, err := svc.IssueToken("admin-1")
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"hostname": "worker-a",
		"version":  "1.0.0",
		"tags":     []string{"edge"},
	})

	req := httptest.NewRequest(http.MethodPost, "/worker/register", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Wireops-Worker-Token", token)
	rec := httptest.NewRecorder()

	server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	records, err := app.FindAllRecords("worker_tokens")
	if err != nil {
		t.Fatalf("failed to query tokens: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 token record, got %d", len(records))
	}
	if got := records[0].GetString("status"); got != TokenStatusActive {
		t.Fatalf("token status = %q, want %q", got, TokenStatusActive)
	}
	if records[0].GetString("worker") == "" {
		t.Fatal("expected token to be bound to a worker")
	}
}
