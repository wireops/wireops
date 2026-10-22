package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	_ "github.com/wireops/wireops/pb_migrations"

	"github.com/wireops/wireops/internal/rbac"
)

// meMux builds the /api/custom/me route with auth fixed to the given record
// (nil for an anonymous request), mirroring setupTestSAApp's router shape.
func meMux(t *testing.T, app core.App, auth *core.Record) http.Handler {
	t.Helper()

	r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
		return &core.RequestEvent{
			App:   app,
			Event: router.Event{Response: w, Request: req},
			Auth:  auth,
		}, nil
	})
	RegisterMeRoutes(r, app)
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("build mux: %v", err)
	}
	return mux
}

func getMe(t *testing.T, mux http.Handler) (int, map[string]any) {
	t.Helper()

	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/custom/me", nil))

	var body map[string]any
	if res.Body.Len() > 0 {
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding body %q: %v", res.Body.String(), err)
		}
	}
	return res.Code, body
}

func capabilityStrings(t *testing.T, body map[string]any) map[string]bool {
	t.Helper()

	raw, ok := body["capabilities"].([]any)
	if !ok {
		t.Fatalf("capabilities missing or not a list: %#v", body["capabilities"])
	}
	out := make(map[string]bool, len(raw))
	for _, item := range raw {
		name, ok := item.(string)
		if !ok {
			t.Fatalf("capability %#v is not a string", item)
		}
		out[name] = true
	}
	return out
}

// TestMe covers GET /api/custom/me as subtests sharing one app and one mux per
// principal, instead of one PocketBase app spin-up per case — each bootstrap
// costs about a second and this package's suite is already close to go test's
// 10-minute per-package timeout (see AGENTS.md § Testing & Coverage). Each
// subtest still gets its own service account or user so nothing leaks between
// cases.
func TestMe(t *testing.T) {
	t.Setenv("SECRET_KEY", strings.Repeat("a", 32))
	t.Setenv("SCAN_PERIOD", "45")

	app := newSetupTestApp(t)
	clearAllUsers(t, app)

	t.Run("ServiceAccountReportsRoleAndCapabilities", func(t *testing.T) {
		sa := createTestSA(t, app, "deploy-bot", "operator key", rbac.RoleOperator, true)

		code, body := getMe(t, meMux(t, app, sa))
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %#v", code, body)
		}

		if body["actor_type"] != rbac.ActorAgent {
			t.Errorf("actor_type = %v, want %q", body["actor_type"], rbac.ActorAgent)
		}
		if body["actor_id"] != sa.Id {
			t.Errorf("actor_id = %v, want %q", body["actor_id"], sa.Id)
		}
		if body["actor_collection"] != "service_accounts" {
			t.Errorf("actor_collection = %v, want service_accounts", body["actor_collection"])
		}
		if body["role"] != rbac.RoleOperator {
			t.Errorf("role = %v, want %q", body["role"], rbac.RoleOperator)
		}
		if body["name"] != "deploy-bot" {
			t.Errorf("name = %v, want deploy-bot", body["name"])
		}
		if body["is_superuser"] != false {
			t.Errorf("is_superuser = %v, want false", body["is_superuser"])
		}
		// SCAN_PERIOD is what makes a stack's sync_interval_seconds=0
		// meaningful, so a client can resolve the effective interval without
		// guessing.
		if got := body["scan_period_seconds"]; got != float64(45) {
			t.Errorf("scan_period_seconds = %v, want 45", got)
		}

		caps := capabilityStrings(t, body)
		for _, capability := range []rbac.Capability{rbac.CapOperateStacks, rbac.CapUseTerminal, rbac.CapViewWorkerPolicy} {
			if !caps[string(capability)] {
				t.Errorf("expected operator key to report %q", capability)
			}
		}
		for _, capability := range []rbac.Capability{rbac.CapManageSettings, rbac.CapViewAuditLogs} {
			if caps[string(capability)] {
				t.Errorf("operator key must not report admin capability %q", capability)
			}
		}
	})

	t.Run("ViewerHasNoWriteCapabilities", func(t *testing.T) {
		sa := createTestSA(t, app, "readonly-bot", "viewer key", rbac.RoleViewer, true)

		code, body := getMe(t, meMux(t, app, sa))
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %#v", code, body)
		}
		if body["role"] != rbac.RoleViewer {
			t.Fatalf("role = %v, want %q", body["role"], rbac.RoleViewer)
		}

		caps := capabilityStrings(t, body)
		if !caps[string(rbac.CapViewStacks)] {
			t.Error("expected viewer key to report view_stacks")
		}
		for _, capability := range []rbac.Capability{rbac.CapOperateStacks, rbac.CapUseTerminal, rbac.CapViewWorkerPolicy} {
			if caps[string(capability)] {
				t.Errorf("viewer key must not report %q", capability)
			}
		}
	})

	t.Run("AnonymousIsUnauthorized", func(t *testing.T) {
		code, body := getMe(t, meMux(t, app, nil))
		if code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %#v", code, body)
		}
	})

	t.Run("DisabledUserIsForbidden", func(t *testing.T) {
		user := createTestUser(t, app, "disabled@example.com", "Password1!", rbac.RoleAdmin)
		user.Set("disabled", true)
		if err := app.Save(user); err != nil {
			t.Fatalf("disabling user: %v", err)
		}

		code, body := getMe(t, meMux(t, app, user))
		if code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %#v", code, body)
		}
		if msg, _ := body["error"].(string); !strings.Contains(msg, "disabled") {
			t.Errorf("error = %q, want it to name the disabled account", msg)
		}
	})

	t.Run("UnknownRoleIsReportedAsNoCapabilities", func(t *testing.T) {
		// ResolveActor blanks an unrecognized role the same way it blanks a
		// disabled user's. Reporting "disabled" for it would send an operator
		// looking for the wrong switch, so the honest answer is a 200 with
		// nothing granted.
		sa := createTestSA(t, app, "misconfigured-bot", "no usable role", rbac.RoleViewer, true)
		sa.Set("role", "")
		if err := app.Save(sa); err != nil {
			t.Fatalf("clearing the role: %v", err)
		}

		code, body := getMe(t, meMux(t, app, sa))
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %#v", code, body)
		}
		if body["role"] != "" {
			t.Errorf("role = %v, want empty", body["role"])
		}
		if caps := capabilityStrings(t, body); len(caps) != 0 {
			t.Errorf("expected no capabilities, got %v", caps)
		}
	})
}
