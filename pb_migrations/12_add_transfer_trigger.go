package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
)

func init() {
	core.SystemMigrations.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}

		triggerField := col.Fields.GetByName("trigger")
		if sf, ok := triggerField.(*core.SelectField); ok {
			sf.Values = []string{"cron", "webhook", "manual", "redeploy", "rollback", "transfer"}
		}

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}

		triggerField := col.Fields.GetByName("trigger")
		if sf, ok := triggerField.(*core.SelectField); ok {
			sf.Values = []string{"cron", "webhook", "manual", "redeploy"}
		}

		return app.Save(col)
	})
}
