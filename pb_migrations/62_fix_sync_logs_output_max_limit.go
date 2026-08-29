package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/wireops/wireops/internal/constants"
)

const syncLogOutputMax = constants.MaxOutputLength

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
					if textField.Max != syncLogOutputMax {
						textField.Max = syncLogOutputMax
						if err := app.Save(col); err != nil {
							return err
						}
						app.Logger().Info("Set max character limit on output field", "collection", colName, "max", syncLogOutputMax)
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
