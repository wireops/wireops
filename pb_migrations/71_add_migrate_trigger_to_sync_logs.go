package pb_migrations

import (
	"fmt"
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Stack repo migration (POST /api/custom/stacks/{id}/migrate) reconciles
// through the normal Scheduler.TriggerSync path with trigger="migrate", so
// sync_logs.trigger's Select enum must accept it or the reconcile's own log
// record fails to save.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}

		field := col.Fields.GetByName("trigger")
		if field == nil {
			return fmt.Errorf("sync_logs.trigger field not found")
		}
		selectField, ok := field.(*core.SelectField)
		if !ok {
			return fmt.Errorf("sync_logs.trigger field is %T, expected *core.SelectField", field)
		}

		hasMigrate := false
		for _, v := range selectField.Values {
			if v == "migrate" {
				hasMigrate = true
				break
			}
		}

		if !hasMigrate {
			selectField.Values = append(selectField.Values, "migrate")
			if err := app.Save(col); err != nil {
				return err
			}
			log.Println("[MIGRATE] Added 'migrate' to sync_logs.trigger allowed values")
		}
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}

		field := col.Fields.GetByName("trigger")
		if field == nil {
			return fmt.Errorf("sync_logs.trigger field not found")
		}
		selectField, ok := field.(*core.SelectField)
		if !ok {
			return fmt.Errorf("sync_logs.trigger field is %T, expected *core.SelectField", field)
		}
		var newValues []string
		for _, v := range selectField.Values {
			if v != "migrate" {
				newValues = append(newValues, v)
			}
		}
		selectField.Values = newValues
		return app.Save(col)
	})
}
