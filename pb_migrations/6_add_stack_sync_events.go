package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col := core.NewBaseCollection("stack_sync_events")

		col.Fields.Add(&core.TextField{Name: "url"})
		col.Fields.Add(&core.TextField{Name: "secret", Hidden: true})
		col.Fields.Add(&core.SelectField{
			Name:      "events",
			MaxSelect: 3,
			Values:    []string{"sync.started", "sync.done", "sync.error"},
		})
		col.Fields.Add(&core.JSONField{Name: "headers"})
		col.Fields.Add(&core.BoolField{Name: "enabled"})
		addAutoDateFields(col)

		col.ListRule = strPtr("@request.auth.id != ''")
		col.ViewRule = strPtr("@request.auth.id != ''")
		col.CreateRule = strPtr("@request.auth.id != ''")
		col.UpdateRule = strPtr("@request.auth.id != ''")
		col.DeleteRule = strPtr("@request.auth.id != ''")

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stack_sync_events")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
