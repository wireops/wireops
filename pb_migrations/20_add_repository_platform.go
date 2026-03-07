package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("repositories")
		if err != nil {
			return err
		}

		col.Fields.Add(&core.SelectField{
			Name:      "platform",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"github", "gitlab", "gitea", "forgejo", "bitbucket"},
		})

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("repositories")
		if err != nil {
			return err
		}

		if f := col.Fields.GetByName("platform"); f != nil {
			col.Fields.RemoveById(f.GetId())
		}

		return app.Save(col)
	})
}
