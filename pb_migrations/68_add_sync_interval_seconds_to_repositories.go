package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("repositories")
		if err != nil {
			return err
		}

		if col.Fields.GetByName("sync_interval_seconds") == nil {
			col.Fields.Add(&core.NumberField{Name: "sync_interval_seconds"})
		}

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added sync_interval_seconds to repositories collection (repo-level fetch tick override; 0 = global SCAN_PERIOD)")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("repositories")
		if err != nil {
			return err
		}

		if f := col.Fields.GetByName("sync_interval_seconds"); f != nil {
			col.Fields.RemoveByName("sync_interval_seconds")
		}

		return app.Save(col)
	})
}
