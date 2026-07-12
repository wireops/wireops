package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}

		col.Fields.Add(&core.TextField{Name: "version"})

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added version field to workers collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}

		col.Fields.RemoveByName("version")

		return app.Save(col)
	})
}
