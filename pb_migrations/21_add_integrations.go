package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col := core.NewBaseCollection("integrations")

		// Identifier for the integration (e.g., "traefik", "dozzle")
		col.Fields.Add(&core.TextField{Name: "slug", Required: true})
		// Global toggle
		col.Fields.Add(&core.BoolField{Name: "enabled"})
		// JSON configuration specific to the integration
		col.Fields.Add(&core.JSONField{Name: "config"})

		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		// Only superusers can manage integrations
		col.ListRule = strPtr("@request.auth.id != ''")
		col.ViewRule = strPtr("@request.auth.id != ''")
		col.CreateRule = strPtr("@request.auth.id != ''")
		col.UpdateRule = strPtr("@request.auth.id != ''")
		col.DeleteRule = strPtr("@request.auth.id != ''")

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("integrations")
		if err != nil {
			return err
		}
		return app.Delete(col)
	})
}
