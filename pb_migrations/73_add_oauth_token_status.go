package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Migration 73: track OAuth token refresh health on repository_keys.
//
// GitLab access tokens are short-lived (~2h) and their refresh token rotates
// on each use; a refresh can therefore permanently fail (revoked/consumed
// refresh token) with no automatic recovery. oauth_last_refresh_at records
// the last successful proactive/lazy refresh (cosmetic "connected, refreshed
// Xm ago"); oauth_refresh_error holds the last failure message and, more
// importantly, its non-empty-ness is what the UI uses to show "reconnect
// required" instead of failing silently on the next sync/browse attempt.
func init() {
	m.Register(func(app core.App) error {
		return addOAuthTokenStatusFields(app)
	}, func(app core.App) error {
		return removeOAuthTokenStatusFields(app)
	})
}

func addOAuthTokenStatusFields(app core.App) error {
	col, err := app.FindCollectionByNameOrId("repository_keys")
	if err != nil {
		return err
	}

	if col.Fields.GetByName("oauth_last_refresh_at") == nil {
		col.Fields.Add(&core.DateField{Name: "oauth_last_refresh_at"})
	}
	if col.Fields.GetByName("oauth_refresh_error") == nil {
		col.Fields.Add(&core.TextField{Name: "oauth_refresh_error"})
	}

	return app.Save(col)
}

func removeOAuthTokenStatusFields(app core.App) error {
	col, err := app.FindCollectionByNameOrId("repository_keys")
	if err != nil {
		return err
	}

	for _, name := range []string{"oauth_last_refresh_at", "oauth_refresh_error"} {
		if field := col.Fields.GetByName(name); field != nil {
			col.Fields.RemoveById(field.GetId())
		}
	}

	return app.Save(col)
}
