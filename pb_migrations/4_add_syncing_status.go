package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		for i, f := range col.Fields {
			if f.GetName() == "status" {
				if sf, ok := col.Fields[i].(*core.SelectField); ok {
					sf.Values = []string{"active", "syncing", "paused", "error", "pending"}
				}
			}
		}

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		for i, f := range col.Fields {
			if f.GetName() == "status" {
				if sf, ok := col.Fields[i].(*core.SelectField); ok {
					sf.Values = []string{"active", "paused", "error", "pending"}
				}
			}
		}

		return app.Save(col)
	})
}
