package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("job_runs")
		if err != nil {
			return err
		}

		field, ok := col.Fields.GetByName("status").(*core.SelectField)
		if !ok {
			return nil
		}
		for _, v := range field.Values {
			if v == "forgotten" {
				return nil // already present
			}
		}
		field.Values = append(field.Values, "forgotten")

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("job_runs")
		if err != nil {
			return err
		}

		field, ok := col.Fields.GetByName("status").(*core.SelectField)
		if !ok {
			return nil
		}
		filtered := field.Values[:0]
		for _, v := range field.Values {
			if v != "forgotten" {
				filtered = append(filtered, v)
			}
		}
		field.Values = filtered

		return app.Save(col)
	})
}
