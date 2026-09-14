package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const stackComposeProjectIndexSQL = `CREATE UNIQUE INDEX IF NOT EXISTS idx_stacks_worker_compose_project
	ON stacks (worker, compose_project_name)
	WHERE compose_project_name != ''`

func init() {
	m.Register(func(app core.App) error {
		stacks, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		stacks.Fields.Add(&core.TextField{Name: "compose_project_name"})
		stacks.Indexes = append(stacks.Indexes, stackComposeProjectIndexSQL)
		if err := app.Save(stacks); err != nil {
			return err
		}

		workers, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}
		workers.Fields.Add(&core.JSONField{Name: "capabilities"})
		if err := app.Save(workers); err != nil {
			return err
		}

		return nil
	}, func(app core.App) error {
		stacks, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		for i, index := range stacks.Indexes {
			if index == stackComposeProjectIndexSQL {
				stacks.Indexes = append(stacks.Indexes[:i], stacks.Indexes[i+1:]...)
				break
			}
		}
		stacks.Fields.RemoveByName("compose_project_name")
		if err := app.Save(stacks); err != nil {
			return err
		}

		workers, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}
		workers.Fields.RemoveByName("capabilities")
		return app.Save(workers)
	})
}
