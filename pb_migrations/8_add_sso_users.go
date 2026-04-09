package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col := core.NewAuthCollection("sso_users")

		col.PasswordAuth.Enabled = false
		col.OAuth2.Enabled = false

		col.ListRule = nil
		col.ViewRule = strPtr("id = @request.auth.id")
		col.CreateRule = strPtr("")
		col.UpdateRule = strPtr("id = @request.auth.id")
		col.DeleteRule = nil

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sso_users")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
