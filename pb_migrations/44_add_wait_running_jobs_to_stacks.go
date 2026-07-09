package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		if col.Fields.GetByName("wait_running_jobs") == nil {
			col.Fields.Add(&core.SelectField{
				Name:      "wait_running_jobs",
				MaxSelect: 1,
				Values:    []string{"never", "always", "timeout"},
			})
		}
		if col.Fields.GetByName("wait_running_jobs_timeout_seconds") == nil {
			col.Fields.Add(&core.NumberField{
				Name: "wait_running_jobs_timeout_seconds",
			})
		}

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added wait_running_jobs and wait_running_jobs_timeout_seconds to stacks collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		for _, name := range []string{"wait_running_jobs", "wait_running_jobs_timeout_seconds"} {
			if f := col.Fields.GetByName(name); f != nil {
				col.Fields.RemoveByName(name)
			}
		}

		return app.Save(col)
	})
}
