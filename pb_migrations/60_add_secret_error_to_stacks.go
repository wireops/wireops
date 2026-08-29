package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		if col.Fields.GetByName("secret_error") == nil {
			col.Fields.Add(&core.BoolField{Name: "secret_error"})
		}

		if err := app.Save(col); err != nil {
			return err
		}

		app.Logger().Info("Added secret_error to stacks collection (marks stacks failing on secret fetch/decrypt so cron stops retrying until the user fixes the secret and triggers a manual sync)")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		if f := col.Fields.GetByName("secret_error"); f != nil {
			col.Fields.RemoveByName("secret_error")
		}

		return app.Save(col)
	})
}
