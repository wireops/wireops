package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}

		triggerField := col.Fields.GetByName("trigger")
		if sf, ok := triggerField.(*core.SelectField); ok {
			sf.Values = []string{"cron", "webhook", "manual", "redeploy", "rollback", "transfer", "queue"}
		}
		
		statusField := col.Fields.GetByName("status")
		if sf, ok := statusField.(*core.SelectField); ok {
			sf.Values = []string{"running", "success", "error", "done", "queued"}
		}

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}

		triggerField := col.Fields.GetByName("trigger")
		if sf, ok := triggerField.(*core.SelectField); ok {
			sf.Values = []string{"cron", "webhook", "manual", "redeploy", "rollback", "transfer"}
		}
		
		statusField := col.Fields.GetByName("status")
		if sf, ok := statusField.(*core.SelectField); ok {
			sf.Values = []string{"running", "success", "error", "done"}
		}

		return app.Save(col)
	})
}
