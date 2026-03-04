package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stack_env_vars")
		if err != nil {
			return err
		}

		col.Fields.Add(&core.BoolField{Name: "secret"})

		// Remove Hidden from value so non-secret values can be returned via API
		for i, f := range col.Fields {
			if f.GetName() == "value" {
				if tf, ok := col.Fields[i].(*core.TextField); ok {
					tf.Hidden = false
				}
			}
		}

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stack_env_vars")
		if err != nil {
			return err
		}

		col.Fields.RemoveByName("secret")

		for i, f := range col.Fields {
			if f.GetName() == "value" {
				if tf, ok := col.Fields[i].(*core.TextField); ok {
					tf.Hidden = true
				}
			}
		}

		return app.Save(col)
	})
}
