package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const jobRunsIndexSQL = "CREATE INDEX IF NOT EXISTS idx_job_runs_job_created ON job_runs (job, created)"

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("job_runs")
		if err != nil {
			return err
		}

		for _, idx := range col.Indexes {
			if idx == jobRunsIndexSQL {
				return nil // already applied
			}
		}

		col.Indexes = append(col.Indexes, jobRunsIndexSQL)

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added index on job_runs (job, created)")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("job_runs")
		if err != nil {
			return err
		}

		for i, idx := range col.Indexes {
			if idx == jobRunsIndexSQL {
				col.Indexes = append(col.Indexes[:i], col.Indexes[i+1:]...)
				break
			}
		}

		return app.Save(col)
	})
}
