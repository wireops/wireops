package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("scheduled_jobs")
		if err != nil {
			return err
		}

		col.Fields.Add(&core.TextField{
			Name:     "name",
			Required: true,
			Pattern:  `^[a-zA-Z0-9\p{L}_ -]+$`,
		})

		col.Fields.Add(&core.TextField{
			Name:     "description",
			Required: false,
		})

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added name and description fields to scheduled_jobs")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("scheduled_jobs")
		if err != nil {
			return err
		}

		col.Fields.RemoveByName("name")
		col.Fields.RemoveByName("description")

		return app.Save(col)
	})
}
