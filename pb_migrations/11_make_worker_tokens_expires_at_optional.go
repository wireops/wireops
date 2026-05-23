package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("worker_tokens")
		if err != nil {
			return err
		}
		field := col.Fields.GetByName("expires_at")
		if field != nil {
			if dateField, ok := field.(*core.DateField); ok {
				dateField.Required = false
			}
		}
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("worker_tokens")
		if err != nil {
			return err
		}
		field := col.Fields.GetByName("expires_at")
		if field != nil {
			if dateField, ok := field.(*core.DateField); ok {
				dateField.Required = true
			}
		}
		return app.Save(col)
	})
}
