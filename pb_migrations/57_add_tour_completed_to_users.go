package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}

		if col.Fields.GetByName("tour_completed") == nil {
			col.Fields.Add(&core.BoolField{Name: "tour_completed"})
		}

		if err := app.Save(col); err != nil {
			return err
		}

		app.Logger().Info("Added tour_completed field to users collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}

		if col.Fields.GetByName("tour_completed") != nil {
			col.Fields.RemoveByName("tour_completed")
			if err := app.Save(col); err != nil {
				return err
			}
		}

		return nil
	})
}
