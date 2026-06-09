package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const auditLogsOriginCreatedIndexSQL = "CREATE INDEX IF NOT EXISTS idx_audit_logs_origin_created ON audit_logs (origin, created)"

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("audit_logs")
		if err != nil {
			return err
		}

		actorTypeField, ok := col.Fields.GetByName("actor_type").(*core.SelectField)
		if !ok {
			return fmt.Errorf("migration: audit_logs.actor_type has unexpected type: %T", col.Fields.GetByName("actor_type"))
		}
		actorTypeField.Values = []string{"anonymous", "user", "system", "worker"}

		if col.Fields.GetByName("origin") == nil {
			col.Fields.Add(&core.SelectField{
				Name:      "origin",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"api", "setup", "system", "ui", "webhook", "worker"},
			})
		}
		if col.Fields.GetByName("metadata_json") == nil {
			col.Fields.Add(&core.JSONField{Name: "metadata_json"})
		}

		col.Indexes = append(col.Indexes, auditLogsOriginCreatedIndexSQL)

		if err := app.Save(col); err != nil {
			return err
		}

		records, err := app.FindAllRecords("audit_logs")
		if err != nil {
			return err
		}
		for _, rec := range records {
			switch rec.GetString("actor_type") {
			case "", "agent":
				rec.Set("actor_type", "system")
			}
			if rec.GetString("origin") == "" {
				rec.Set("origin", "system")
			}
			if err := app.Save(rec); err != nil {
				return err
			}
		}

		log.Println("[MIGRATE] Hardened audit_logs schema with origin and metadata")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("audit_logs")
		if err != nil {
			return nil
		}

		if actorTypeField, ok := col.Fields.GetByName("actor_type").(*core.SelectField); ok {
			actorTypeField.Values = []string{"user", "system", "agent"}
		}

		for i := len(col.Indexes) - 1; i >= 0; i-- {
			if col.Indexes[i] == auditLogsOriginCreatedIndexSQL {
				col.Indexes = append(col.Indexes[:i], col.Indexes[i+1:]...)
			}
		}

		col.Fields.RemoveByName("origin")
		col.Fields.RemoveByName("metadata_json")
		if err := app.Save(col); err != nil {
			return err
		}

		return nil
	})
}
