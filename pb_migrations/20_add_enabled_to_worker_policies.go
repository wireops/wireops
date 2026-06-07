package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		pol, err := app.FindCollectionByNameOrId("worker_policies")
		if err != nil {
			return err
		}

		pol.Fields.Add(&core.BoolField{Name: "enabled"})
		if err := app.Save(pol); err != nil {
			return err
		}

		// Update existing records to default enabled = true
		records, err := app.FindAllRecords("worker_policies")
		if err != nil {
			return err
		}
		for _, rec := range records {
			rec.Set("enabled", true)
			if err := app.Save(rec); err != nil {
				log.Printf("[MIGRATE] Failed to update worker_policies record %s: %v", rec.Id, err)
			}
		}

		log.Println("[MIGRATE] Added enabled field to worker_policies and set existing to true")
		return nil
	}, func(app core.App) error {
		pol, err := app.FindCollectionByNameOrId("worker_policies")
		if err != nil {
			return err
		}
		pol.Fields.RemoveByName("enabled")
		return app.Save(pol)
	})
}
