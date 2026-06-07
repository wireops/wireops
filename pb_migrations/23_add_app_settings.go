package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		// Create app_settings singleton collection
		settings := core.NewBaseCollection("app_settings")
		
		settings.Fields.Add(&core.TextField{Name: "timezone"})
		settings.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		settings.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		// Superusers only — no public access.
		settings.ListRule = nil
		settings.ViewRule = nil
		settings.CreateRule = nil
		settings.UpdateRule = nil
		settings.DeleteRule = nil

		if err := app.Save(settings); err != nil {
			return err
		}

		log.Println("[MIGRATE] Created app_settings collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("app_settings")
		if err != nil {
			return err
		}
		return app.Delete(col)
	})
}
