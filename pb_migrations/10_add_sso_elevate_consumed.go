package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Migration 10: SSO elevate replay protection — one successful /auth/elevate per SSO session.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sso_users")
		if err != nil {
			return nil
		}
		if col.Fields.GetByName("elevate_consumed") != nil {
			return nil
		}
		col.Fields.Add(&core.BoolField{Name: "elevate_consumed"})
		col.Fields.Add(&core.DateField{Name: "elevate_consumed_at"})
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sso_users")
		if err != nil {
			return nil
		}
		col.Fields.RemoveByName("elevate_consumed_at")
		col.Fields.RemoveByName("elevate_consumed")
		return app.Save(col)
	})
}
