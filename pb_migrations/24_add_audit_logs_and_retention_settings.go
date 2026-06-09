package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const (
	auditLogsActorCreatedIndexSQL    = "CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_created ON audit_logs (actor_id, created)"
	auditLogsResourceCreatedIndexSQL = "CREATE INDEX IF NOT EXISTS idx_audit_logs_resource_created ON audit_logs (resource_type, resource_id, created)"
	auditLogsActionCreatedIndexSQL   = "CREATE INDEX IF NOT EXISTS idx_audit_logs_action_created ON audit_logs (action, created)"
	auditLogsExpiresIndexSQL         = "CREATE INDEX IF NOT EXISTS idx_audit_logs_expires_at ON audit_logs (expires_at)"
)

func init() {
	m.Register(func(app core.App) error {
		col := core.NewBaseCollection("audit_logs")
		col.Fields.Add(&core.SelectField{
			Name:      "actor_type",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"user", "system", "agent"},
		})
		col.Fields.Add(&core.TextField{Name: "actor_id"})
		col.Fields.Add(&core.TextField{Name: "action", Required: true})
		col.Fields.Add(&core.TextField{Name: "resource_type", Required: true})
		col.Fields.Add(&core.TextField{Name: "resource_id"})
		col.Fields.Add(&core.SelectField{
			Name:      "status",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"success", "error"},
		})
		col.Fields.Add(&core.TextField{Name: "error_code"})
		col.Fields.Add(&core.DateField{Name: "expires_at", Required: true})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})

		// Superusers can read audit events. Writes are system-only.
		col.ListRule = nil
		col.ViewRule = nil
		col.CreateRule = nil
		col.UpdateRule = nil
		col.DeleteRule = nil

		col.Indexes = append(col.Indexes,
			auditLogsActorCreatedIndexSQL,
			auditLogsResourceCreatedIndexSQL,
			auditLogsActionCreatedIndexSQL,
			auditLogsExpiresIndexSQL,
		)

		if err := app.Save(col); err != nil {
			return err
		}

		settings, err := app.FindCollectionByNameOrId("app_settings")
		if err != nil {
			return err
		}
		minDays := 1.0
		settings.Fields.Add(&core.NumberField{Name: "audit_retention_days", OnlyInt: true, Min: &minDays})
		settings.Fields.Add(&core.NumberField{Name: "job_run_retention_days", OnlyInt: true, Min: &minDays})
		if err := app.Save(settings); err != nil {
			return err
		}

		records, err := app.FindAllRecords("app_settings")
		if err != nil {
			return err
		}
		for _, rec := range records {
			if rec.GetInt("audit_retention_days") <= 0 {
				rec.Set("audit_retention_days", 30)
			}
			if rec.GetInt("job_run_retention_days") <= 0 {
				rec.Set("job_run_retention_days", 7)
			}
			if err := app.Save(rec); err != nil {
				return err
			}
		}

		if _, err := app.DB().
			NewQuery("UPDATE job_runs SET expires_at = datetime(created, '+7 days') WHERE expires_at IS NULL OR expires_at = '' OR expires_at > datetime(created, '+7 days')").
			Execute(); err != nil {
			return err
		}

		log.Println("[MIGRATE] Created audit_logs collection and retention settings")
		return nil
	}, func(app core.App) error {
		settings, err := app.FindCollectionByNameOrId("app_settings")
		if err == nil {
			settings.Fields.RemoveByName("audit_retention_days")
			settings.Fields.RemoveByName("job_run_retention_days")
			if err := app.Save(settings); err != nil {
				return err
			}
		}

		col, err := app.FindCollectionByNameOrId("audit_logs")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
