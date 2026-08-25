package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/rbac"
)

func callCreateInviteHandler(t *testing.T, app core.App, body any, actor *core.Record) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/custom/users/invite", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e := &core.RequestEvent{
		App: app,
		Event: router.Event{
			Response: rec,
			Request:  req,
		},
		Auth: actor,
	}
	if err := handleCreateInvite(app)(e); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return rec
}

func TestCreateInviteDelivery(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	admin := createTestUser(t, app, "admin@example.com", "Password1!", rbac.RoleAdmin)

	tests := []struct {
		name           string
		body           map[string]any
		expectedCode   int
		expectedEmails int
		expectsLink    bool
		expectsInvite  bool
	}{
		{
			name: "shareable link",
			body: map[string]any{
				"email":    "link@example.com",
				"role":     rbac.RoleViewer,
				"delivery": "link",
			},
			expectedCode:  http.StatusOK,
			expectsLink:   true,
			expectsInvite: true,
		},
		{
			name: "email by default when delivery is omitted",
			body: map[string]any{
				"email": "email@example.com",
				"role":  rbac.RoleViewer,
			},
			expectedCode:   http.StatusOK,
			expectedEmails: 1,
			expectsInvite:  true,
		},
		{
			name: "invalid delivery",
			body: map[string]any{
				"email":    "invalid@example.com",
				"role":     rbac.RoleViewer,
				"delivery": "pigeon",
			},
			expectedCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emailsBefore := app.TestMailer.TotalSend()
			rec := callCreateInviteHandler(t, app, tt.body, admin)
			if rec.Code != tt.expectedCode {
				t.Fatalf("expected %d, got %d: %s", tt.expectedCode, rec.Code, rec.Body.String())
			}

			invites, err := app.FindAllRecords("invites", dbx.HashExp{"email": tt.body["email"]})
			if err != nil {
				t.Fatalf("find invites: %v", err)
			}
			if got := len(invites) == 1; got != tt.expectsInvite {
				t.Fatalf("expected invite persisted=%t, got %d invite(s)", tt.expectsInvite, len(invites))
			}
			if got, want := app.TestMailer.TotalSend(), emailsBefore+tt.expectedEmails; got != want {
				t.Fatalf("unexpected number of sent emails: got %d, expected %d", got, want)
			}
			if !tt.expectsInvite {
				return
			}

			var response struct {
				Status    string `json:"status"`
				InviteURL string `json:"invite_url"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Status != "invited" {
				t.Fatalf("unexpected response: %#v", response)
			}
			if tt.expectsLink {
				if !strings.Contains(response.InviteURL, "/invite?token=") || !strings.HasSuffix(response.InviteURL, invites[0].GetString("token")) {
					t.Fatalf("invite URL does not include the saved token: %q", response.InviteURL)
				}
			} else if response.InviteURL != "" {
				t.Fatalf("email delivery must not return an invite URL: %q", response.InviteURL)
			}
		})
	}
}

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

func callUpdateSelfHandler(t *testing.T, app core.App, body any, actor *core.Record) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/custom/users/me", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e := &core.RequestEvent{
		App: app,
		Event: router.Event{
			Response: rec,
			Request:  req,
		},
		Auth: actor,
	}
	if err := handleUpdateSelf(app)(e); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return rec
}

// TestUpdateSelfAllowsViewerToChangeOwnPassword guards against the users
// collection's admin-only UpdateRule (pb_migrations/26_add_rbac_identity.go)
// silently blocking a non-admin's self-service password change.
func TestUpdateSelfAllowsViewerToChangeOwnPassword(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	viewer := createTestUser(t, app, "viewer@example.com", "Password1!", rbac.RoleViewer)

	rec := callUpdateSelfHandler(t, app, map[string]any{
		"old_password":     "Password1!",
		"password":         "NewPassword2!",
		"password_confirm": "NewPassword2!",
	}, viewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	reloaded, err := app.FindRecordById("users", viewer.Id)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !reloaded.ValidatePassword("NewPassword2!") {
		t.Fatal("expected password to have been updated")
	}
}

func TestUpdateSelfRejectsWrongOldPassword(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	user := createTestUser(t, app, "user@example.com", "Password1!", rbac.RoleViewer)

	rec := callUpdateSelfHandler(t, app, map[string]any{
		"old_password":     "WrongPassword!",
		"password":         "NewPassword2!",
		"password_confirm": "NewPassword2!",
	}, user)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	reloaded, err := app.FindRecordById("users", user.Id)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !reloaded.ValidatePassword("Password1!") {
		t.Fatal("password should not have changed")
	}
}

func TestUpdateSelfRejectsMismatchedConfirmation(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	user := createTestUser(t, app, "user@example.com", "Password1!", rbac.RoleViewer)

	rec := callUpdateSelfHandler(t, app, map[string]any{
		"old_password":     "Password1!",
		"password":         "NewPassword2!",
		"password_confirm": "SomethingElse3!",
	}, user)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateSelfCanUpdateName(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)
	user := createTestUser(t, app, "user@example.com", "Password1!", rbac.RoleViewer)

	rec := callUpdateSelfHandler(t, app, map[string]any{"name": "New Name"}, user)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	reloaded, err := app.FindRecordById("users", user.Id)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.GetString("name") != "New Name" {
		t.Fatalf("expected name to be updated, got %q", reloaded.GetString("name"))
	}
}

func TestUpdateSelfRejectsUnauthenticated(t *testing.T) {
	app := newSetupTestApp(t)
	clearAllUsers(t, app)

	rec := callUpdateSelfHandler(t, app, map[string]any{"name": "New Name"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}
