package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// keep in sync with maxOutputLength in internal/sync/reconciler.go and internal/jobscheduler/scheduler.go
const syncLogOutputMax = 1000000

func init() {
	m.Register(func(app core.App) error {
		for _, colName := range []string{"sync_logs", "job_runs"} {
			col, err := app.FindCollectionByNameOrId(colName)
			if err != nil {
				log.Printf("[MIGRATE] Warning: collection %s not found: %v", colName, err)
				continue
			}

			field := col.Fields.GetByName("output")
			if field != nil {
				if textField, ok := field.(*core.TextField); ok {
					if textField.Max != syncLogOutputMax {
						textField.Max = syncLogOutputMax
						if err := app.Save(col); err != nil {
							return err
						}
						log.Printf("[MIGRATE] Set max character limit on %s.output to %d", colName, syncLogOutputMax)
					}
				}
			}
		}
		return nil
	}, func(app core.App) error {
		// Rollback is a no-op; the prior state (implicit 5000 limit) was the bug.
		return nil
	})
}
