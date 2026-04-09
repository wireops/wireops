package pb_migrations

import (
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/pocketbase/pocketbase/core"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sso_users")
		if err != nil {
			return nil
		}

		col.ViewRule = strPtr("id = @request.auth.id")
		col.CreateRule = strPtr("")
		col.UpdateRule = strPtr("id = @request.auth.id")

		return app.Save(col)
	}, nil)
}
