package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}
		
		col.Fields.Add(&core.JSONField{
			Name: "health_history",
		})
		
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}
		col.Fields.RemoveByName("health_history")
		return app.Save(col)
	})
}
