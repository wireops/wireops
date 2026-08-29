package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		for _, colName := range []string{"sync_logs", "job_runs"} {
			col, err := app.FindCollectionByNameOrId(colName)
			if err != nil {
				app.Logger().Warn("Collection not found", "collection", colName, "error", err)
				continue
			}

			field := col.Fields.GetByName("output")
			if field != nil {
				if textField, ok := field.(*core.TextField); ok {
					if textField.Max != 0 {
						textField.Max = 0
						if err := app.Save(col); err != nil {
							return err
						}
						app.Logger().Info("Reset max character limit on output field to unlimited", "collection", colName)
					}
				}
			}
		}
		return nil
	}, func(app core.App) error {
		// Rollback is a no-op since setting to unlimited is safe and doesn't need to be reverted
		return nil
	})
}
