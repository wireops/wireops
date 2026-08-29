package pb_migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Migration 65 created stack_config_files.stack and job_config_files.job as
// required relations. CascadeDelete was added to the migration source after
// it had already run on deployed instances, so those existing collections
// were created without it (migrations don't re-run on edit) — deleting a
// stack/job with tracked config files fails with "record cannot be deleted
// because it is part of a required reference". Same gap migration 49 fixed
// once already for sync_log_phases.
func init() {
	m.Register(func(app core.App) error {
		if err := enableConfigFilesCascade(app, "stack_config_files", "stack", true); err != nil {
			return err
		}
		if err := enableConfigFilesCascade(app, "job_config_files", "job", true); err != nil {
			return err
		}
		app.Logger().Info("Enabled cascade delete on stack_config_files.stack and job_config_files.job")
		return nil
	}, func(app core.App) error {
		// Rollback is a no-op. Migration 65's source was itself edited to set
		// CascadeDelete: true after it had already run on deployed instances
		// (see the comment above), so on any install created from the current
		// source — fresh installs, and every instance created after that fix
		// landed — migration 65 already leaves this true and this migration's
		// up() is a no-op. Unconditionally setting it back to false here would
		// silently break those installs' stack/job deletion, and even on the
		// legacy instances this migration actually fixes, false is the bug
		// being fixed, not a state worth restoring (same rationale as the
		// notifier-secret-encryption rollback in migration 63).
		return nil
	})
}

func enableConfigFilesCascade(app core.App, collectionName, fieldName string, cascade bool) error {
	col, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return err
	}
	field, ok := col.Fields.GetByName(fieldName).(*core.RelationField)
	if !ok {
		return fmt.Errorf("%s.%s field not found or wrong type", collectionName, fieldName)
	}
	field.CascadeDelete = cascade
	return app.Save(col)
}
