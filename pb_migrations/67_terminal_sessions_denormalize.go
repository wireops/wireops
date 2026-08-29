package pb_migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Terminal session history is moving into the Audit settings page (a
// standing compliance record), so it needs to survive deletion of the
// stack/user/worker it referenced — the same requirement audit_logs
// already meets by storing plain text instead of relations. Two changes:
//
//  1. Add denormalized text fields (populated once at session-open time)
//     so the history table has something to show even after the stack,
//     user, or worker record is gone.
//  2. Relax the stack/user/worker relations from Required to optional —
//     PocketBase blocks deleting a record that's still a *required*
//     reference elsewhere ("record cannot be deleted because it is part
//     of a required reference", see migration 49's fix for the same
//     issue on sync_log_phases). With Required: false and CascadeDelete
//     left false, PocketBase instead just unsets the relation on delete,
//     leaving the terminal_sessions row (and its denormalized fields)
//     intact.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("terminal_sessions")
		if err != nil {
			return err
		}

		col.Fields.Add(&core.TextField{Name: "stack_name"})
		col.Fields.Add(&core.TextField{Name: "service_name"})
		col.Fields.Add(&core.TextField{Name: "container_name"})
		col.Fields.Add(&core.TextField{Name: "user_email"})
		col.Fields.Add(&core.TextField{Name: "worker_hostname"})

		for _, name := range []string{"stack", "user", "worker"} {
			field, ok := col.Fields.GetByName(name).(*core.RelationField)
			if !ok {
				return fmt.Errorf("terminal_sessions.%s field not found or wrong type", name)
			}
			field.Required = false
		}

		if err := app.Save(col); err != nil {
			return err
		}

		app.Logger().Info("Denormalized terminal_sessions and relaxed its stack/user/worker relations")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("terminal_sessions")
		if err != nil {
			return err
		}

		for _, name := range []string{"stack_name", "service_name", "container_name", "user_email", "worker_hostname"} {
			if f := col.Fields.GetByName(name); f != nil {
				col.Fields.RemoveById(f.GetId())
			}
		}
		for _, name := range []string{"stack", "user", "worker"} {
			if field, ok := col.Fields.GetByName(name).(*core.RelationField); ok {
				field.Required = true
			}
		}

		return app.Save(col)
	})
}
