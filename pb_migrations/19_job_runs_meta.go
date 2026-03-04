package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("job_runs")
		if err != nil {
			return err
		}

		col.Fields.Add(&core.TextField{Name: "container_name"})
		col.Fields.Add(&core.TextField{Name: "commit_sha"})

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("job_runs")
		if err != nil {
			return err
		}

		for _, name := range []string{"container_name", "commit_sha"} {
			if f := col.Fields.GetByName(name); f != nil {
				col.Fields.RemoveById(f.GetId())
			}
		}

		return app.Save(col)
	})
}
