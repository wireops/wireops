package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/rbac"
	"github.com/wireops/wireops/internal/termsession"
	"github.com/wireops/wireops/internal/termstream"
)

func newTerminalTestEvent(auth *core.Record) (*core.RequestEvent, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	return &core.RequestEvent{
		Event: router.Event{Response: w, Request: req},
		Auth:  auth,
	}, w
}

func TestAuthorizeTerminalSessionUnknownSession(t *testing.T) {
	rr := routeRegistrar{}
	e, w := newTerminalTestEvent(nil)

	if _, ok := rr.authorizeTerminalSession(e, "does-not-exist"); ok {
		t.Fatal("expected authorization to fail for an unknown session")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAuthorizeTerminalSessionRejectsOtherActor(t *testing.T) {
	rr := routeRegistrar{}

	col := core.NewAuthCollection("users")
	owner := core.NewRecord(col)
	owner.Id = "user-owner"
	owner.Set("role", rbac.RoleOperator)

	other := core.NewRecord(col)
	other.Id = "user-other"
	other.Set("role", rbac.RoleOperator)

	sessionID := "sess-ownership-test"
	rememberTerminalSession(sessionID, terminalSessionMeta{workerID: "worker-1", actorID: owner.Id, stackID: "stack-1"})
	defer forgetTerminalSession(sessionID)

	e, w := newTerminalTestEvent(other)
	if _, ok := rr.authorizeTerminalSession(e, sessionID); ok {
		t.Fatal("expected authorization to fail for a different actor")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestAuthorizeTerminalSessionAllowsOwner(t *testing.T) {
	rr := routeRegistrar{}

	col := core.NewAuthCollection("users")
	owner := core.NewRecord(col)
	owner.Id = "user-owner-2"
	owner.Set("role", rbac.RoleOperator)

	sessionID := "sess-owner-ok"
	meta := terminalSessionMeta{workerID: "worker-2", actorID: owner.Id, stackID: "stack-2"}
	rememberTerminalSession(sessionID, meta)
	defer forgetTerminalSession(sessionID)

	e, _ := newTerminalTestEvent(owner)
	got, ok := rr.authorizeTerminalSession(e, sessionID)
	if !ok {
		t.Fatal("expected authorization to succeed for the session's own actor")
	}
	if got.workerID != meta.workerID || got.actorID != meta.actorID || got.stackID != meta.stackID {
		t.Fatalf("expected %+v, got %+v", meta, got)
	}
}

func TestForgetTerminalSessionRemovesEntry(t *testing.T) {
	sessionID := "sess-forget"
	rememberTerminalSession(sessionID, terminalSessionMeta{workerID: "w", actorID: "a", stackID: "s"})
	if _, ok := lookupTerminalSession(sessionID); !ok {
		t.Fatal("expected session to be present after remembering it")
	}
	forgetTerminalSession(sessionID)
	if _, ok := lookupTerminalSession(sessionID); ok {
		t.Fatal("expected session to be gone after forgetting it")
	}
}

func TestAuthorizeTerminalSessionForCloseAllowsAdminOverride(t *testing.T) {
	rr := routeRegistrar{}

	col := core.NewAuthCollection("users")
	owner := core.NewRecord(col)
	owner.Id = "user-owner-3"
	owner.Set("role", rbac.RoleOperator)

	admin := core.NewRecord(col)
	admin.Id = "user-admin"
	admin.Set("role", rbac.RoleAdmin)

	stranger := core.NewRecord(col)
	stranger.Id = "user-stranger"
	stranger.Set("role", rbac.RoleOperator)

	sessionID := "sess-close-admin"
	rememberTerminalSession(sessionID, terminalSessionMeta{workerID: "worker-3", actorID: owner.Id, stackID: "stack-3"})
	defer forgetTerminalSession(sessionID)

	if e, _ := newTerminalTestEvent(stranger); true {
		if _, ok := rr.authorizeTerminalSessionForClose(e, sessionID); ok {
			t.Fatal("expected a non-owner, non-admin actor to be denied")
		}
	}

	if e, _ := newTerminalTestEvent(admin); true {
		if _, ok := rr.authorizeTerminalSessionForClose(e, sessionID); !ok {
			t.Fatal("expected an admin (CapManageSecurity) actor to be allowed to close another user's session")
		}
	}

	if e, _ := newTerminalTestEvent(owner); true {
		if _, ok := rr.authorizeTerminalSessionForClose(e, sessionID); !ok {
			t.Fatal("expected the session's own actor to be allowed to close it")
		}
	}
}

func TestCountTerminalSessionsForWorker(t *testing.T) {
	rememberTerminalSession("sess-count-1", terminalSessionMeta{workerID: "worker-count", actorID: "a"})
	rememberTerminalSession("sess-count-2", terminalSessionMeta{workerID: "worker-count", actorID: "b"})
	rememberTerminalSession("sess-count-3", terminalSessionMeta{workerID: "worker-other", actorID: "c"})
	defer forgetTerminalSession("sess-count-1")
	defer forgetTerminalSession("sess-count-2")
	defer forgetTerminalSession("sess-count-3")

	if got := countTerminalSessionsForWorker("worker-count"); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
	if got := countTerminalSessionsForWorker("worker-other"); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
	if got := countTerminalSessionsForWorker("worker-none"); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestIdleTerminalSessions(t *testing.T) {
	fresh := "sess-idle-fresh"
	stale := "sess-idle-stale"
	rememberTerminalSession(fresh, terminalSessionMeta{workerID: "w", actorID: "a"})
	rememberTerminalSession(stale, terminalSessionMeta{workerID: "w", actorID: "a"})
	defer forgetTerminalSession(fresh)
	defer forgetTerminalSession(stale)

	touchTerminalSession(fresh)
	terminalSessionsMu.Lock()
	staleMeta := terminalSessions[stale]
	staleMeta.lastActivity = time.Now().Add(-2 * terminalIdleTimeout)
	terminalSessions[stale] = staleMeta
	terminalSessionsMu.Unlock()

	idle := idleTerminalSessions()
	if _, ok := idle[stale]; !ok {
		t.Fatal("expected the stale session to be reported idle")
	}
	if _, ok := idle[fresh]; ok {
		t.Fatal("expected the freshly-touched session to not be reported idle")
	}
}

// TestHandleTerminalOpenRejectedClosesRecord guards the fix for a real race:
// a worker rejecting MsgTerminalOpen (draining/overloaded) is handled async,
// on a different goroutine than the POST handler. If the session record were
// created after dispatch (as it used to be), that rejection could arrive and
// no-op against a not-yet-existing row, and the row created moments later
// would then never get closed until the 15-minute idle sweep. The POST
// handler now creates the record before dispatching, so by the time any
// rejection can possibly arrive, the row it needs to close already exists —
// this test exercises HandleTerminalOpenRejected against that row directly.
func TestHandleTerminalOpenRejectedClosesRecord(t *testing.T) {
	app := newSetupTestApp(t)
	t.Setenv("TERMINAL_SESSIONS_STORAGE_PATH", t.TempDir())
	broker := termstream.New()

	sessionID := "sess-open-rejected"
	rememberTerminalSession(sessionID, terminalSessionMeta{workerID: "worker-x", actorID: "user-x", stackID: "stack-x"})

	termsession.CreateRecord(app, termsession.CreateRecordParams{
		SessionID:   sessionID,
		ContainerID: "container-x",
		Rows:        24,
		Cols:        80,
	})

	HandleTerminalOpenRejected(app, broker, sessionID, "worker overloaded")

	if _, ok := lookupTerminalSession(sessionID); ok {
		t.Fatal("expected session to be forgotten from the in-memory routing map")
	}

	rec, err := app.FindFirstRecordByFilter("terminal_sessions", "session_id = {:id}", map[string]any{"id": sessionID})
	if err != nil {
		t.Fatalf("expected session record to still exist: %v", err)
	}
	if rec.GetString("status") != "closed" {
		t.Fatalf("expected status closed, got %q", rec.GetString("status"))
	}
	if rec.GetInt("exit_code") != -1 {
		t.Fatalf("expected exit_code -1, got %d", rec.GetInt("exit_code"))
	}

	sub, unsub := broker.Subscribe(sessionID)
	defer unsub()
	select {
	case ev := <-sub:
		if !ev.Closed || ev.Error != "worker overloaded" {
			t.Fatalf("expected closed event with reason, got %+v", ev)
		}
	default:
		t.Fatal("expected a buffered closed event for a subscriber connecting after the rejection")
	}
}
