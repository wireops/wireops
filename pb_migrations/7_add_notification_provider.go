package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stack_sync_events")
		if err != nil {
			return err
		}

		// Add new fields for multi-provider support
		col.Fields.Add(&core.TextField{Name: "provider"}) // "webhook" or "ntfy"
		col.Fields.Add(&core.TextField{Name: "ntfy_user"})
		col.Fields.Add(&core.TextField{Name: "ntfy_topic"})
		col.Fields.Add(&core.TextField{Name: "ntfy_template"}) // Textarea

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stack_sync_events")
		if err != nil {
			return err
		}

		// Remove fields
		col.Fields.RemoveByName("provider")
		col.Fields.RemoveByName("ntfy_user")
		col.Fields.RemoveByName("ntfy_topic")
		col.Fields.RemoveByName("ntfy_template")

		return app.Save(col)
	})
}
