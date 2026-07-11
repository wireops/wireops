package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		if f := col.Fields.GetByName("poll_interval"); f != nil {
			col.Fields.RemoveByName("poll_interval")
		}

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Removed poll_interval from stacks collection (replaced by global SCAN_PERIOD config)")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		if col.Fields.GetByName("poll_interval") == nil {
			col.Fields.Add(&core.NumberField{Name: "poll_interval"})
		}

		return app.Save(col)
	})
}
