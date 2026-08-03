package termsession

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// termSessionFixture is a test app plus one valid row in each collection
// terminal_sessions relates to, so CreateRecordParams can reference real
// stack/user/worker IDs.
type termSessionFixture struct {
	app      *tests.TestApp
	stackID  string
	userID   string
	workerID string
}

func newTermSessionTestApp(t *testing.T) termSessionFixture {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	// PocketBase test apps already seed a built-in "users" auth collection,
	// so these use non-colliding names — CreateRecord only cares about the
	// relation field's target collection ID, not what it's called.
	users := core.NewBaseCollection("test_terminal_users")
	mustSave(t, app, users)
	userRec := core.NewRecord(users)
	if err := app.Save(userRec); err != nil {
		t.Fatalf("save user record: %v", err)
	}

	stacks := core.NewBaseCollection("test_terminal_stacks")
	mustSave(t, app, stacks)
	stackRec := core.NewRecord(stacks)
	if err := app.Save(stackRec); err != nil {
		t.Fatalf("save stack record: %v", err)
	}

	workers := core.NewBaseCollection("test_terminal_workers")
	mustSave(t, app, workers)
	workerRec := core.NewRecord(workers)
	if err := app.Save(workerRec); err != nil {
		t.Fatalf("save worker record: %v", err)
	}

	sessions := core.NewBaseCollection(collectionName)
	sessions.Fields.Add(&core.TextField{Name: "session_id", Required: true})
	sessions.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacks.Id, MaxSelect: 1})
	sessions.Fields.Add(&core.TextField{Name: "stack_name"})
	sessions.Fields.Add(&core.RelationField{Name: "user", CollectionId: users.Id, MaxSelect: 1})
	sessions.Fields.Add(&core.TextField{Name: "user_email"})
	sessions.Fields.Add(&core.RelationField{Name: "worker", CollectionId: workers.Id, MaxSelect: 1})
	sessions.Fields.Add(&core.TextField{Name: "worker_hostname"})
	sessions.Fields.Add(&core.TextField{Name: "container_id", Required: true})
	sessions.Fields.Add(&core.TextField{Name: "container_name"})
	sessions.Fields.Add(&core.TextField{Name: "service_name"})
	sessions.Fields.Add(&core.TextField{Name: "transcript_path", Required: true})
	sessions.Fields.Add(&core.DateField{Name: "started_at", Required: true})
	sessions.Fields.Add(&core.DateField{Name: "ended_at"})
	sessions.Fields.Add(&core.NumberField{Name: "exit_code", OnlyInt: true})
	sessions.Fields.Add(&core.SelectField{Name: "status", Required: true, MaxSelect: 1, Values: []string{"open", "closed"}})
	sessions.Fields.Add(&core.DateField{Name: "expires_at", Required: true})
	mustSave(t, app, sessions)

	return termSessionFixture{app: app, stackID: stackRec.Id, userID: userRec.Id, workerID: workerRec.Id}
}

func mustSave(t *testing.T, app core.App, col *core.Collection) {
	t.Helper()
	if err := app.Save(col); err != nil {
		t.Fatalf("save collection %s: %v", col.Name, err)
	}
}

func TestRetentionDaysDefaultsWithoutSettings(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	defer app.Cleanup()

	if got := RetentionDays(app); got != DefaultRetentionDays {
		t.Fatalf("expected default %d, got %d", DefaultRetentionDays, got)
	}
}

func TestRetentionDaysFromSettings(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	defer app.Cleanup()

	settings := core.NewBaseCollection("app_settings")
	settings.Fields.Add(&core.NumberField{Name: "terminal_retention_days", OnlyInt: true})
	if err := app.Save(settings); err != nil {
		t.Fatalf("save app_settings collection: %v", err)
	}
	rec := core.NewRecord(settings)
	rec.Set("terminal_retention_days", 10)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save app_settings record: %v", err)
	}

	if got := RetentionDays(app); got != 10 {
		t.Fatalf("expected 10, got %d", got)
	}
}

func TestCreateRecordWriteAppendAndCloseRoundTrip(t *testing.T) {
	fx := newTermSessionTestApp(t)
	app := fx.app
	t.Setenv("TERMINAL_SESSIONS_STORAGE_PATH", t.TempDir())

	sessionID := "sess-roundtrip"
	CreateRecord(app, CreateRecordParams{
		SessionID:   sessionID,
		StackID:     fx.stackID,
		StackName:   "test-stack",
		UserID:      fx.userID,
		UserEmail:   "user@example.com",
		WorkerID:    fx.workerID,
		ContainerID: "container-1",
		Rows:        24,
		Cols:        80,
	})

	rec, err := app.FindFirstRecordByFilter(collectionName, "session_id = {:id}", map[string]any{"id": sessionID})
	if err != nil {
		t.Fatalf("expected session record to exist: %v", err)
	}
	if rec.GetString("status") != "open" {
		t.Fatalf("expected status open, got %q", rec.GetString("status"))
	}

	AppendTranscript(sessionID, []byte("hello"))

	data, err := os.ReadFile(transcriptPath(sessionID))
	if err != nil {
		t.Fatalf("expected transcript file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty transcript")
	}

	CloseRecord(app, sessionID, 0)

	rec, err = app.FindFirstRecordByFilter(collectionName, "session_id = {:id}", map[string]any{"id": sessionID})
	if err != nil {
		t.Fatalf("expected session record after close: %v", err)
	}
	if rec.GetString("status") != "closed" {
		t.Fatalf("expected status closed, got %q", rec.GetString("status"))
	}

	// A later append for the same (now closed) session ID must be a no-op —
	// the in-memory start-time entry was cleared by CloseRecord.
	before, _ := os.ReadFile(transcriptPath(sessionID))
	AppendTranscript(sessionID, []byte("late write"))
	after, _ := os.ReadFile(transcriptPath(sessionID))
	if string(before) != string(after) {
		t.Fatal("expected AppendTranscript to no-op after CloseRecord")
	}
}

func TestPurgeExpiredRemovesRecordAndFile(t *testing.T) {
	fx := newTermSessionTestApp(t)
	app := fx.app
	dir := t.TempDir()
	t.Setenv("TERMINAL_SESSIONS_STORAGE_PATH", dir)

	sessionID := "sess-expired"
	CreateRecord(app, CreateRecordParams{
		SessionID:   sessionID,
		StackID:     fx.stackID,
		StackName:   "test-stack",
		UserID:      fx.userID,
		UserEmail:   "user@example.com",
		WorkerID:    fx.workerID,
		ContainerID: "container-1",
		Rows:        24,
		Cols:        80,
	})

	rec, err := app.FindFirstRecordByFilter(collectionName, "session_id = {:id}", map[string]any{"id": sessionID})
	if err != nil {
		t.Fatalf("expected session record: %v", err)
	}
	rec.Set("expires_at", time.Now().Add(-time.Hour))
	if err := app.Save(rec); err != nil {
		t.Fatalf("force-expire record: %v", err)
	}

	transcriptFile := filepath.Join(dir, sessionID+".cast")
	if _, err := os.Stat(transcriptFile); err != nil {
		t.Fatalf("expected transcript file to exist before purge: %v", err)
	}

	if err := PurgeExpired(app); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}

	if _, err := app.FindFirstRecordByFilter(collectionName, "session_id = {:id}", map[string]any{"id": sessionID}); err == nil {
		t.Fatal("expected expired session record to be deleted")
	}
	if _, err := os.Stat(transcriptFile); !os.IsNotExist(err) {
		t.Fatal("expected transcript file to be removed")
	}
}
