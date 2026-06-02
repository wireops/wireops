package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("worker_commands")
		if err != nil {
			return err
		}

		col.Fields.Add(&core.TextField{
			Name: "created_by",
		})

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added created_by field to worker_commands")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("worker_commands")
		if err != nil {
			return err
		}

		col.Fields.RemoveByName("created_by")

		return app.Save(col)
	})
}
