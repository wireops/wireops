package rbac

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	pbrouter "github.com/pocketbase/pocketbase/tools/router"
)

func TestNormalizeRole(t *testing.T) {
	if got := NormalizeRole(" Operator "); got != RoleOperator {
		t.Fatalf("expected operator, got %q", got)
	}
	if got := NormalizeRole("owner"); got != "" {
		t.Fatalf("expected invalid role to normalize empty, got %q", got)
	}
}

func TestAtLeast(t *testing.T) {
	cases := []struct {
		name    string
		role    string
		minimum string
		want    bool
	}{
		{name: "viewer reads viewer", role: RoleViewer, minimum: RoleViewer, want: true},
		{name: "viewer cannot operate", role: RoleViewer, minimum: RoleOperator, want: false},
		{name: "operator can view", role: RoleOperator, minimum: RoleViewer, want: true},
		{name: "admin can operate", role: RoleAdmin, minimum: RoleOperator, want: true},
		{name: "unknown cannot view", role: "", minimum: RoleViewer, want: false},
		{name: "monitoring reads metrics", role: RoleMonitoring, minimum: RoleMonitoring, want: true},
		{name: "monitoring cannot view stacks", role: RoleMonitoring, minimum: RoleViewer, want: false},
		{name: "viewer can read metrics", role: RoleViewer, minimum: RoleMonitoring, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AtLeast(tc.role, tc.minimum); got != tc.want {
				t.Fatalf("AtLeast(%q, %q) = %v, want %v", tc.role, tc.minimum, got, tc.want)
			}
		})
	}
}

func TestHighestRole(t *testing.T) {
	if got := HighestRole(RoleViewer, RoleAdmin, RoleOperator); got != RoleAdmin {
		t.Fatalf("expected admin, got %q", got)
	}
	if got := HighestRole("unknown", RoleViewer); got != RoleViewer {
		t.Fatalf("expected viewer, got %q", got)
	}
}

func TestMinimumRole(t *testing.T) {
	if got := MinimumRole(CapOperateStacks); got != RoleOperator {
		t.Fatalf("expected operator, got %q", got)
	}
	if got := MinimumRole("unknown"); got != RoleAdmin {
		t.Fatalf("unknown capability should require admin, got %q", got)
	}
}

func TestCapViewWorkersIsViewerLevel(t *testing.T) {
	if got := MinimumRole(CapViewWorkers); got != RoleViewer {
		t.Fatalf("expected viewer, got %q", got)
	}
	if !AtLeast(RoleViewer, MinimumRole(CapViewWorkers)) {
		t.Fatal("expected viewer role to satisfy CapViewWorkers")
	}
	if AtLeast(RoleMonitoring, MinimumRole(CapViewWorkers)) {
		t.Fatal("expected monitoring role to NOT satisfy CapViewWorkers")
	}
}

func TestResolveActorDisabledUser(t *testing.T) {
	col := core.NewAuthCollection("users")
	user := core.NewRecord(col)
	user.Set("role", RoleAdmin)
	user.Set("disabled", true)

	event := &core.RequestEvent{Auth: user}
	role, actorType, _ := ResolveActor(event)

	if role != "" {
		t.Fatalf("expected empty role for disabled user, got %q", role)
	}
	if actorType != ActorUser {
		t.Fatalf("expected actor type %q, got %q", ActorUser, actorType)
	}
}

func TestResolveActorActiveUser(t *testing.T) {
	col := core.NewAuthCollection("users")
	user := core.NewRecord(col)
	user.Set("role", RoleOperator)
	user.Set("disabled", false)

	event := &core.RequestEvent{Auth: user}
	role, _, _ := ResolveActor(event)

	if role != RoleOperator {
		t.Fatalf("expected role %q for active user, got %q", RoleOperator, role)
	}
}

func TestDisabledUserCannotAccessCapability(t *testing.T) {
	col := core.NewAuthCollection("users")
	user := core.NewRecord(col)
	user.Set("role", RoleAdmin)
	user.Set("disabled", true)

	event := &core.RequestEvent{Auth: user}

	if Can(event, CapViewStacks) {
		t.Fatal("disabled admin should not be able to view stacks")
	}
}

func TestRequireSuperuserMiddleware(t *testing.T) {
	t.Run("allows a real superuser", func(t *testing.T) {
		superuser := core.NewRecord(core.NewAuthCollection(core.CollectionNameSuperusers))

		r := pbrouter.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, pbrouter.EventCleanupFunc) {
			return &core.RequestEvent{
				Event: pbrouter.Event{Response: w, Request: req},
				Auth:  superuser,
			}, nil
		})
		r.GET("/protected", func(e *core.RequestEvent) error {
			return e.String(http.StatusOK, "ok")
		}).BindFunc(RequireSuperuser())

		mux, err := r.BuildMux()
		if err != nil {
			t.Fatalf("build mux: %v", err)
		}

		res := httptest.NewRecorder()
		mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/protected", nil))

		if res.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
		}
	})

	t.Run("rejects a wireops admin role that is not a real superuser", func(t *testing.T) {
		admin := core.NewRecord(core.NewAuthCollection("users"))
		admin.Set("role", RoleAdmin)

		event := &core.RequestEvent{
			Event: pbrouter.Event{
				Response: httptest.NewRecorder(),
				Request:  httptest.NewRequest(http.MethodGet, "/", nil),
			},
			Auth: admin,
		}

		if err := RequireSuperuser()(event); err != nil {
			t.Fatalf("middleware returned error: %v", err)
		}
		res := event.Response.(*httptest.ResponseRecorder)
		if res.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for non-superuser admin, got %d", res.Code)
		}
	})

	t.Run("rejects unauthenticated", func(t *testing.T) {
		event := &core.RequestEvent{Event: pbrouter.Event{
			Response: httptest.NewRecorder(),
			Request:  httptest.NewRequest(http.MethodGet, "/", nil),
		}}

		if err := RequireSuperuser()(event); err != nil {
			t.Fatalf("middleware returned error: %v", err)
		}
		res := event.Response.(*httptest.ResponseRecorder)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauthenticated, got %d", res.Code)
		}
	})
}

func TestRequireMiddleware(t *testing.T) {
	t.Run("allows and calls next handler", func(t *testing.T) {
		user := core.NewRecord(core.NewAuthCollection("users"))
		user.Set("role", RoleAdmin)

		r := pbrouter.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, pbrouter.EventCleanupFunc) {
			return &core.RequestEvent{
				Event: pbrouter.Event{Response: w, Request: req},
				Auth:  user,
			}, nil
		})
		r.GET("/protected", func(e *core.RequestEvent) error {
			return e.String(http.StatusOK, "ok")
		}).BindFunc(Require(CapManageSecurity))

		mux, err := r.BuildMux()
		if err != nil {
			t.Fatalf("build mux: %v", err)
		}

		res := httptest.NewRecorder()
		mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/protected", nil))

		if res.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
		}
		if res.Body.String() != "ok" {
			t.Fatalf("expected next handler body, got %q", res.Body.String())
		}
	})

	t.Run("rejects unauthenticated", func(t *testing.T) {
		event := &core.RequestEvent{Event: pbrouter.Event{
			Response: httptest.NewRecorder(),
			Request:  httptest.NewRequest(http.MethodGet, "/", nil),
		}}

		if err := Require(CapViewStacks)(event); err != nil {
			t.Fatalf("middleware returned error: %v", err)
		}
		if got := event.Response.(*httptest.ResponseRecorder).Code; got != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", got)
		}
	})

	t.Run("rejects insufficient role", func(t *testing.T) {
		user := core.NewRecord(core.NewAuthCollection("users"))
		user.Set("role", RoleViewer)
		event := &core.RequestEvent{
			Event: pbrouter.Event{
				Response: httptest.NewRecorder(),
				Request:  httptest.NewRequest(http.MethodGet, "/", nil),
			},
			Auth: user,
		}

		if err := Require(CapManageSecurity)(event); err != nil {
			t.Fatalf("middleware returned error: %v", err)
		}
		if got := event.Response.(*httptest.ResponseRecorder).Code; got != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", got)
		}
	})
}
