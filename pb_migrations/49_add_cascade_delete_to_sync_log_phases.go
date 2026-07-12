package pb_migrations

import (
	"fmt"
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Migration 48 created sync_log_phases.sync_log as a required relation but
// left CascadeDelete unset, so deleting a sync_logs row fails once it has
// phase rows attached ("record cannot be deleted because it is part of a
// required reference"). Deploy timeline phases have no independent meaning
// once their parent sync log is gone, so they should always cascade.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sync_log_phases")
		if err != nil {
			return err
		}
		field, ok := col.Fields.GetByName("sync_log").(*core.RelationField)
		if !ok {
			return fmt.Errorf("sync_log_phases.sync_log field not found or wrong type")
		}
		field.CascadeDelete = true
		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Enabled cascade delete on sync_log_phases.sync_log")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sync_log_phases")
		if err != nil {
			return err
		}
		if field, ok := col.Fields.GetByName("sync_log").(*core.RelationField); ok {
			field.CascadeDelete = false
		}
		return app.Save(col)
	})
}
