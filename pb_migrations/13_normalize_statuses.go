package pb_migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		if _, err := app.DB().NewQuery("UPDATE sync_logs SET status = 'success' WHERE status = 'done'").Execute(); err != nil {
			return fmt.Errorf("migration 13: normalize sync_logs status: %w", err)
		}

		syncLogs, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}
		if field := syncLogs.Fields.GetByName("status"); field != nil {
			selectField, ok := field.(*core.SelectField)
			if !ok {
				return fmt.Errorf("migration 13: sync_logs.status is %T, want SelectField", field)
			}
			selectField.Values = []string{"running", "success", "error", "queued"}
			if err := app.Save(syncLogs); err != nil {
				return err
			}
		}

		jobs, err := app.FindCollectionByNameOrId("scheduled_jobs")
		if err != nil {
			return err
		}
		if field := jobs.Fields.GetByName("status"); field != nil {
			selectField, ok := field.(*core.SelectField)
			if !ok {
				return fmt.Errorf("migration 13: scheduled_jobs.status is %T, want SelectField", field)
			}
			selectField.Values = []string{"active", "paused", "stalled", "error"}
			if err := app.Save(jobs); err != nil {
				return err
			}
		}

		return nil
	}, func(app core.App) error {
		syncLogs, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}
		if field := syncLogs.Fields.GetByName("status"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				selectField.Values = []string{"running", "success", "error", "done", "queued"}
				if err := app.Save(syncLogs); err != nil {
					return err
				}
			}
		}

		jobs, err := app.FindCollectionByNameOrId("scheduled_jobs")
		if err != nil {
			return err
		}
		if field := jobs.Fields.GetByName("status"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				selectField.Values = []string{"active", "paused", "stalled"}
				if err := app.Save(jobs); err != nil {
					return err
				}
			}
		}

		return nil
	})
}
