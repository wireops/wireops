package routes

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/wireops/wireops/internal/buildinfo"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/rbac"
)

// RegisterMeRoutes mounts the caller-identity introspection route.
//
// It carries no rbac.Require: the route tells a principal only about itself,
// and gating it on a capability would make the one call an API client needs to
// find out what it may do the first call that can fail for lack of permission.
// Anonymous requests are rejected in the handler instead.
func RegisterMeRoutes(r *router.Router[*core.RequestEvent], app core.App) {
	// GET /api/custom/me
	// Who the caller is, what it may do, and the instance facts a client needs
	// to interpret the rest of the API (version, default poll cadence). Added
	// for the MCP server, whose clients could previously only discover their
	// own role by collecting 403s, but it is a plain API route.
	r.GET("/api/custom/me", func(e *core.RequestEvent) error {
		if e.Auth == nil {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		}

		role, actorType, actorID := rbac.ResolveActor(e)
		if role == "" {
			// A disabled user authenticates but satisfies nothing — report it
			// as such rather than as an empty capability list with a role of "".
			return e.JSON(http.StatusForbidden, map[string]string{"error": "this account is disabled"})
		}

		capabilities := rbac.CapabilitiesFor(role)
		capNames := make([]string, 0, len(capabilities))
		for _, capability := range capabilities {
			capNames = append(capNames, string(capability))
		}

		name := e.Auth.GetString("name")
		if name == "" {
			name = e.Auth.GetString("email")
		}

		collection := ""
		if col := e.Auth.Collection(); col != nil {
			collection = col.Name
		}

		return e.JSON(http.StatusOK, map[string]any{
			"actor_id":            actorID,
			"actor_type":          actorType,
			"actor_collection":    collection,
			"name":                name,
			"role":                role,
			"capabilities":        capNames,
			"is_superuser":        e.Auth.IsSuperuser(),
			"version":             buildinfo.Version,
			"commit":              buildinfo.Commit,
			"build_date":          buildinfo.BuildDate,
			"scan_period_seconds": int(config.GetScanPeriod().Seconds()),
		})
	})
}
