package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sync_log_phases")
		if err != nil {
			return err
		}

		field, ok := col.Fields.GetByName("phase").(*core.SelectField)
		if !ok || field == nil {
			return nil
		}

		hasSecretsFetch := false
		for _, v := range field.Values {
			if v == "secrets_fetch" {
				hasSecretsFetch = true
				break
			}
		}
		if !hasSecretsFetch {
			field.Values = append(field.Values, "secrets_fetch")
		}

		if err := app.Save(col); err != nil {
			return err
		}

		app.Logger().Info("Added secrets_fetch to sync_log_phases.phase enum")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sync_log_phases")
		if err != nil {
			return err
		}

		field, ok := col.Fields.GetByName("phase").(*core.SelectField)
		if !ok || field == nil {
			return nil
		}

		values := make([]string, 0, len(field.Values))
		for _, v := range field.Values {
			if v != "secrets_fetch" {
				values = append(values, v)
			}
		}
		field.Values = values

		return app.Save(col)
	})
}
