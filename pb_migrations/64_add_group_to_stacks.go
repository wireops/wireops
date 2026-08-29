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

		if col.Fields.GetByName("group") == nil {
			col.Fields.Add(&core.TextField{
				Name: "group",
				Max:  120,
			})
		}

		if err := app.Save(col); err != nil {
			return err
		}

		app.Logger().Info("Added group field to stacks collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		col.Fields.RemoveByName("group")

		return app.Save(col)
	})
}
