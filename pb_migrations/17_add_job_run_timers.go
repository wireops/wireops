package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("job_runs")
		if err != nil {
			return err
		}

		col.Fields.Add(&core.DateField{
			Name: "started_at",
		})

		col.Fields.Add(&core.NumberField{
			Name: "queue_time_ms",
		})

		col.Fields.Add(&core.NumberField{
			Name: "execution_time_ms",
		})

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added started_at, queue_time_ms, and execution_time_ms fields to job_runs")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("job_runs")
		if err != nil {
			return err
		}

		col.Fields.RemoveByName("started_at")
		col.Fields.RemoveByName("queue_time_ms")
		col.Fields.RemoveByName("execution_time_ms")

		return app.Save(col)
	})
}
