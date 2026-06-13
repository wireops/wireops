package pb_migrations

import (
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
		if field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				// Prevent duplicate insertions
				hasStats := false
				hasLogs := false
				for _, v := range selectField.Values {
					if v == "get_container_stats" {
						hasStats = true
					}
					if v == "get_container_logs" {
						hasLogs = true
					}
				}

				changed := false
				if !hasStats {
					selectField.Values = append(selectField.Values, "get_container_stats")
					changed = true
				}
				if !hasLogs {
					selectField.Values = append(selectField.Values, "get_container_logs")
					changed = true
				}

				if changed {
					if err := app.Save(col); err != nil {
						return err
					}
					log.Println("[MIGRATE] Added 'get_container_stats' and 'get_container_logs' to worker_commands.command_type allowed values")
				}
			}
		}
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("worker_commands")
		if err != nil {
			return err
		}

		field := col.Fields.GetByName("command_type")
		if field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				var newValues []string
				for _, v := range selectField.Values {
					if v != "get_container_stats" && v != "get_container_logs" {
						newValues = append(newValues, v)
					}
				}
				selectField.Values = newValues
				return app.Save(col)
			}
		}
		return nil
	})
}
