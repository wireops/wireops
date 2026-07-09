package pb_migrations

import (
	"fmt"
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const workerCommandsIdempotencyKeyIndexSQL = "CREATE INDEX IF NOT EXISTS idx_worker_commands_idempotency_key ON worker_commands (idempotency_key)"

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("worker_commands")
		if err != nil {
			return err
		}

		if col.Fields.GetByName("message_id") == nil {
			col.Fields.Add(&core.TextField{
				Name: "message_id",
			})
		}

		if col.Fields.GetByName("idempotency_key") == nil {
			col.Fields.Add(&core.TextField{
				Name: "idempotency_key",
			})
		}

		if col.Fields.GetByName("attempt_count") == nil {
			col.Fields.Add(&core.NumberField{
				Name: "attempt_count",
			})
		}

		if col.Fields.GetByName("next_attempt_at") == nil {
			col.Fields.Add(&core.DateField{
				Name: "next_attempt_at",
			})
		}

		field := col.Fields.GetByName("status")
		if field == nil {
			return fmt.Errorf("worker_commands.status field not found")
		}
		selectField, ok := field.(*core.SelectField)
		if !ok {
			return fmt.Errorf("worker_commands.status field is %T, expected *core.SelectField", field)
		}
		hasQueued := false
		for _, v := range selectField.Values {
			if v == "queued" {
				hasQueued = true
				break
			}
		}
		if !hasQueued {
			selectField.Values = append([]string{"queued"}, selectField.Values...)
		}

		col.Indexes = append(col.Indexes, workerCommandsIdempotencyKeyIndexSQL)

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added durable queue fields (message_id, idempotency_key, attempt_count, next_attempt_at, queued status) to worker_commands")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("worker_commands")
		if err != nil {
			return err
		}

		for _, name := range []string{"message_id", "idempotency_key", "attempt_count", "next_attempt_at"} {
			if f := col.Fields.GetByName(name); f != nil {
				col.Fields.RemoveByName(name)
			}
		}

		if field := col.Fields.GetByName("status"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				var newValues []string
				for _, v := range selectField.Values {
					if v != "queued" {
						newValues = append(newValues, v)
					}
				}
				selectField.Values = newValues
			}
		}

		var newIndexes []string
		for _, idx := range col.Indexes {
			if idx != workerCommandsIdempotencyKeyIndexSQL {
				newIndexes = append(newIndexes, idx)
			}
		}
		col.Indexes = newIndexes

		return app.Save(col)
	})
}
