package pb_migrations

import (
	"fmt"
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("worker_commands")
		if err != nil {
			return err
		}

		field := col.Fields.GetByName("command_type")
		if field == nil {
			return fmt.Errorf("worker_commands.command_type field not found")
		}
		selectField, ok := field.(*core.SelectField)
		if !ok {
			return fmt.Errorf("worker_commands.command_type field is %T, expected *core.SelectField", field)
		}
		hasMetrics := false
		for _, v := range selectField.Values {
			if v == "get_metrics" {
				hasMetrics = true
				break
			}
		}

		if !hasMetrics {
			selectField.Values = append(selectField.Values, "get_metrics")
			if err := app.Save(col); err != nil {
				return err
			}
			log.Println("[MIGRATE] Added 'get_metrics' to worker_commands.command_type allowed values")
		}
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("worker_commands")
		if err != nil {
			return err
		}

		field := col.Fields.GetByName("command_type")
		if field == nil {
			return fmt.Errorf("worker_commands.command_type field not found")
		}
		selectField, ok := field.(*core.SelectField)
		if !ok {
			return fmt.Errorf("worker_commands.command_type field is %T, expected *core.SelectField", field)
		}
		var newValues []string
		for _, v := range selectField.Values {
			if v != "get_metrics" {
				newValues = append(newValues, v)
			}
		}
		selectField.Values = newValues
		return app.Save(col)
	})
}
