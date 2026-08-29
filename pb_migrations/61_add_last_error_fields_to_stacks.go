package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		if col.Fields.GetByName("last_error_category") == nil {
			col.Fields.Add(&core.SelectField{
				Name:      "last_error_category",
				MaxSelect: 1,
				Values:    []string{"sync", "deploy"},
			})
		}
		if col.Fields.GetByName("last_error_message") == nil {
			col.Fields.Add(&core.TextField{Name: "last_error_message", Max: syncLogOutputMax})
		}
		if col.Fields.GetByName("last_error_at") == nil {
			col.Fields.Add(&core.DateField{Name: "last_error_at"})
		}

		if err := app.Save(col); err != nil {
			return err
		}

		app.Logger().Info("Added last_error_category/last_error_message/last_error_at to stacks collection (denormalized so the UI can show a sync-vs-deploy error card without joining sync_logs/sync_log_phases)")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		col.Fields.RemoveByName("last_error_category")
		col.Fields.RemoveByName("last_error_message")
		col.Fields.RemoveByName("last_error_at")

		return app.Save(col)
	})
}
