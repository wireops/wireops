package pb_migrations

import (
	"database/sql"
	"errors"

	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/pocketbase/pocketbase/core"
	"github.com/wireops/wireops/internal/oidc"
)

// The default public CreateRule ("") on sso_users lets anyone POST a record
// directly to /api/collections/sso_users/records, pre-occupying an email
// before the real OAuth2 flow runs (email squatting) and leaving orphan rows
// with no linked _externalAuths.
//
// Restricting CreateRule to "@request.context = 'oauth2'" blocks direct API
// creation (which runs with the "default" context) while still allowing the
// self-signup path: PocketBase creates the sso_users record via an internal
// request tagged with the "oauth2" context. This does NOT require superuser
// auth, so the anonymous OAuth2 self-signup keeps working (unlike a nil rule).
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sso_users")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}

		col.CreateRule = strPtr("@request.context = 'oauth2'")
		oidc.HydrateClientSecretForValidation(col)

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sso_users")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}

		col.CreateRule = strPtr("")
		oidc.HydrateClientSecretForValidation(col)

		return app.Save(col)
	})
}
