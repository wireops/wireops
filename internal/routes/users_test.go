package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/rbac"
)

func callUpdateUserHandler(t *testing.T, app core.App, userID string, body any, actor *core.Record) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/custom/users/"+userID, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", userID)
	rec := httptest.NewRecorder()
	e := &core.RequestEvent{
		App: app,
		Event: router.Event{
			Response: rec,
			Request:  req,
		},
		Auth: actor,
	}
	if err := handleUpdateUser(app)(e); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return rec
}

func TestUpdateUserDisableEnableRoundtrip(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "admin@example.com", "Password1!", rbac.RoleAdmin)
	target := createTestUser(t, app, "target@example.com", "Password1!", rbac.RoleViewer)

	rec := callUpdateUserHandler(t, app, target.Id, map[string]any{"disabled": true}, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	reloaded, _ := app.FindRecordById("users", target.Id)
	if !reloaded.GetBool("disabled") {
		t.Fatal("expected user to be disabled")
	}

	rec = callUpdateUserHandler(t, app, target.Id, map[string]any{"disabled": false}, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	reloaded, _ = app.FindRecordById("users", target.Id)
	if reloaded.GetBool("disabled") {
		t.Fatal("expected user to be enabled again")
	}
}

func TestUpdateUserCannotDisableProtectedAdmin(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	protected := createTestUser(t, app, "root@example.com", "Password1!", rbac.RoleAdmin)
	protected.Set("protected", true)
	if err := app.Save(protected); err != nil {
		t.Fatalf("save protected user: %v", err)
	}
	actor := createTestUser(t, app, "actor@example.com", "Password1!", rbac.RoleAdmin)

	rec := callUpdateUserHandler(t, app, protected.Id, map[string]any{"disabled": true}, actor)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateUserCannotDemoteProtectedAdmin(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	protected := createTestUser(t, app, "root@example.com", "Password1!", rbac.RoleAdmin)
	protected.Set("protected", true)
	if err := app.Save(protected); err != nil {
		t.Fatalf("save protected user: %v", err)
	}
	actor := createTestUser(t, app, "actor@example.com", "Password1!", rbac.RoleAdmin)

	rec := callUpdateUserHandler(t, app, protected.Id, map[string]any{"role": rbac.RoleViewer}, actor)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateUserCannotDisableLastActiveAdmin(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "sole@example.com", "Password1!", rbac.RoleAdmin)

	rec := callUpdateUserHandler(t, app, admin.Id, map[string]any{"disabled": true}, admin)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateUserCannotDemoteLastActiveAdmin(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "sole@example.com", "Password1!", rbac.RoleAdmin)

	rec := callUpdateUserHandler(t, app, admin.Id, map[string]any{"role": rbac.RoleViewer}, admin)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateUserAllowsDemoteWhenMultipleAdmins(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	createTestUser(t, app, "admin1@example.com", "Password1!", rbac.RoleAdmin)
	admin2 := createTestUser(t, app, "admin2@example.com", "Password1!", rbac.RoleAdmin)
	actor := createTestUser(t, app, "actor@example.com", "Password1!", rbac.RoleAdmin)

	rec := callUpdateUserHandler(t, app, admin2.Id, map[string]any{"role": rbac.RoleViewer}, actor)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
