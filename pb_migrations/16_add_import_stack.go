package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		stacksCol, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		// Make repository not required for local imported stacks
		if field := stacksCol.Fields.GetByName("repository"); field != nil {
			if rel, ok := field.(*core.RelationField); ok {
				rel.Required = false
			}
		}

		// source_type distinguishes git-backed stacks from locally imported ones
		stacksCol.Fields.Add(&core.SelectField{
			Name:   "source_type",
			Values: []string{"git", "local"},
		})

		// import_path: absolute path to the compose file on the agent host
		stacksCol.Fields.Add(&core.TextField{Name: "import_path"})

		// import_recreate_volumes: user preference captured during import wizard;
		// read once on the first reconcile, then ignored.
		stacksCol.Fields.Add(&core.BoolField{Name: "import_recreate_volumes"})

		return app.Save(stacksCol)
	}, func(app core.App) error {
		stacksCol, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		if field := stacksCol.Fields.GetByName("repository"); field != nil {
			if rel, ok := field.(*core.RelationField); ok {
				rel.Required = true
			}
		}

		stacksCol.Fields.RemoveByName("source_type")
		stacksCol.Fields.RemoveByName("import_path")
		stacksCol.Fields.RemoveByName("import_recreate_volumes")

		return app.Save(stacksCol)
	})
}
