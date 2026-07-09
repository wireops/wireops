package pb_migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		syncLogs, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}
		field := syncLogs.Fields.GetByName("status")
		selectField, ok := field.(*core.SelectField)
		if !ok {
			return fmt.Errorf("migration 40: sync_logs.status is %T, want SelectField", field)
		}
		selectField.Values = []string{"running", "success", "error", "queued", "noop"}
		return app.Save(syncLogs)
	}, func(app core.App) error {
		syncLogs, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}
		field := syncLogs.Fields.GetByName("status")
		selectField, ok := field.(*core.SelectField)
		if !ok {
			return fmt.Errorf("migration 40 rollback: sync_logs.status is %T, want SelectField", field)
		}
		selectField.Values = []string{"running", "success", "error", "queued"}
		return app.Save(syncLogs)
	})
}
