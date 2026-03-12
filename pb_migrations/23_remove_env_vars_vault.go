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

		col.Fields.RemoveByName("secret_integration")
		col.Fields.RemoveByName("secret_group")
		col.Fields.RemoveByName("secret_path")
		col.Fields.RemoveByName("secret_key")

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stack_env_vars")
		if err != nil {
			return err
		}

		integrationsCol, err := app.FindCollectionByNameOrId("integrations")
		if err != nil {
			return err
		}

		col.Fields.Add(&core.RelationField{
			Name:         "secret_integration",
			CollectionId: integrationsCol.Id,
			MaxSelect:    1,
		})
		col.Fields.Add(&core.TextField{Name: "secret_group"})
		col.Fields.Add(&core.TextField{Name: "secret_path"})
		col.Fields.Add(&core.TextField{Name: "secret_key"})

		return app.Save(col)
	})
}
