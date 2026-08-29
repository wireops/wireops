package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// The compose lint step runs between git_fetch and render and records its own
// sync_log_phases row, so "lint" has to be a permitted value of that select
// field. Ordering within the timeline comes from the seq column
// (constants.DeployPhaseOrder), not from this list.
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

		hasLint := false
		for _, v := range field.Values {
			if v == "lint" {
				hasLint = true
				break
			}
		}
		if !hasLint {
			field.Values = append(field.Values, "lint")
		}

		if err := app.Save(col); err != nil {
			return err
		}

		app.Logger().Info("Added lint to sync_log_phases.phase enum")
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
			if v != "lint" {
				values = append(values, v)
			}
		}
		field.Values = values

		return app.Save(col)
	})
}
