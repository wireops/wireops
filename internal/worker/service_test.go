package worker

import (
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func newWorkerTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	ensureWorkerCollections(t, app)
	t.Cleanup(func() { app.Cleanup() })
	return app
}

func ensureWorkerCollections(t *testing.T, app core.App) {
	t.Helper()

	if _, err := app.FindCollectionByNameOrId("workers"); err != nil {
		col := core.NewBaseCollection("workers")
		col.Fields.Add(&core.TextField{Name: "hostname", Required: true})
		col.Fields.Add(&core.TextField{Name: "fingerprint", Required: true})
		col.Fields.Add(&core.SelectField{
			Name:      "status",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"ACTIVE", "REVOKED"},
		})
		col.Fields.Add(&core.AutodateField{Name: "last_seen", OnCreate: true, OnUpdate: true})
		col.Fields.Add(&core.JSONField{Name: "health_history"})
		if err := app.Save(col); err != nil {
			t.Fatalf("failed to create workers collection: %v", err)
		}
	}

	if _, err := app.FindCollectionByNameOrId("worker_tokens"); err != nil {
		col := core.NewBaseCollection("worker_tokens")
		// Use simplified field definitions to prevent duplication warnings.
		// TextField behaves identically for basic CRUD operations in tests.
		col.Fields.Add(&core.TextField{Name: "token_hash"})
		col.Fields.Add(&core.TextField{Name: "status"})
		col.Fields.Add(&core.TextField{Name: "worker"})
		col.Fields.Add(&core.DateField{Name: "expires_at"})
		col.Fields.Add(&core.DateField{Name: "last_used_at"})
		col.Fields.Add(&core.TextField{Name: "created_by"})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		if err := app.Save(col); err != nil {
			t.Fatalf("failed to create worker_tokens collection: %v", err)
		}
	}

	if _, err := app.FindCollectionByNameOrId("worker_commands"); err != nil {
		col := core.NewBaseCollection("worker_commands")
		col.Fields.Add(&core.TextField{Name: "worker"})
		col.Fields.Add(&core.TextField{Name: "command_id", Required: true})
		col.Fields.Add(&core.TextField{Name: "command_type"})
		col.Fields.Add(&core.TextField{Name: "status"})
		col.Fields.Add(&core.TextField{Name: "message_id"})
		col.Fields.Add(&core.TextField{Name: "idempotency_key"})
		col.Fields.Add(&core.NumberField{Name: "attempt_count"})
		col.Fields.Add(&core.DateField{Name: "next_attempt_at"})
		col.Fields.Add(&core.JSONField{Name: "payload"})
		col.Fields.Add(&core.JSONField{Name: "result"})
		col.Fields.Add(&core.NumberField{Name: "duration_ms"})
		col.Fields.Add(&core.DateField{Name: "expires_at"})
		col.Fields.Add(&core.TextField{Name: "created_by"})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		col.Indexes = append(col.Indexes, "CREATE UNIQUE INDEX IF NOT EXISTS idx_worker_commands_command_id_test ON worker_commands (command_id)")
		if err := app.Save(col); err != nil {
			t.Fatalf("failed to create worker_commands collection: %v", err)
		}
	}
}

func TestIssueTokenCreatesStagingRecord(t *testing.T) {
	app := newWorkerTestApp(t)
	svc := NewService(app)

	token, record, err := svc.IssueToken("admin-1")
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected plaintext token")
	}
	if got := record.GetString("status"); got != TokenStatusStaging {
		t.Fatalf("status = %q, want %q", got, TokenStatusStaging)
	}
	if record.GetString("token_hash") == token {
		t.Fatal("token hash should not match plaintext token")
	}
	if got := record.GetString("created_by"); got != "admin-1" {
		t.Fatalf("created_by = %q, want %q", got, "admin-1")
	}
}

func TestActivateTokenBindsSingleWorker(t *testing.T) {
	app := newWorkerTestApp(t)
	svc := NewService(app)

	token, _, err := svc.IssueToken("admin-1")
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	workerRecord, tokenRecord, err := svc.ActivateToken(token, "worker-a")
	if err != nil {
		t.Fatalf("first ActivateToken failed: %v", err)
	}
	if workerRecord.Id == "" {
		t.Fatal("expected worker id")
	}
	if got := tokenRecord.GetString("status"); got != TokenStatusActive {
		t.Fatalf("token status = %q, want %q", got, TokenStatusActive)
	}
	if got := tokenRecord.GetString("worker"); got != workerRecord.Id {
		t.Fatalf("token worker = %q, want %q", got, workerRecord.Id)
	}

	reconnectedWorker, reconnectedToken, err := svc.ActivateToken(token, "worker-b")
	if err != nil {
		t.Fatalf("second ActivateToken failed: %v", err)
	}
	if reconnectedWorker.Id != workerRecord.Id {
		t.Fatalf("reconnected worker = %q, want %q", reconnectedWorker.Id, workerRecord.Id)
	}
	if reconnectedToken.Id != tokenRecord.Id {
		t.Fatalf("reconnected token = %q, want %q", reconnectedToken.Id, tokenRecord.Id)
	}

	workers, err := app.FindAllRecords("workers", dbx.HashExp{"fingerprint": "remote:" + tokenRecord.Id})
	if err != nil {
		t.Fatalf("failed to query workers: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("expected 1 worker bound to token, got %d", len(workers))
	}
}

func TestExpireStagingTokensMarksExpired(t *testing.T) {
	app := newWorkerTestApp(t)
	svc := NewService(app)

	_, record, err := svc.IssueToken("admin-1")
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	record.Set("expires_at", time.Now().UTC().Add(-time.Minute))
	if err := app.Save(record); err != nil {
		t.Fatalf("failed to update expiry: %v", err)
	}

	if err := svc.ExpireStagingTokens(); err != nil {
		t.Fatalf("ExpireStagingTokens failed: %v", err)
	}

	refreshed, err := app.FindRecordById("worker_tokens", record.Id)
	if err != nil {
		t.Fatalf("failed to reload token: %v", err)
	}
	if got := refreshed.GetString("status"); got != TokenStatusExpired {
		t.Fatalf("status = %q, want %q", got, TokenStatusExpired)
	}
}
