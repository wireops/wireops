package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Migration 6: add secret_provider field to stack_env_vars.
// This enables individual env vars to reference external secret backends
// (e.g. "vault", "infisical") in addition to the default "wireops" AES-GCM provider.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stack_env_vars")
		if err != nil {
			return err
		}

		// Only add if not already present (idempotent).
		if col.Fields.GetByName("secret_provider") != nil {
			return nil
		}

		col.Fields.Add(&core.TextField{
			Name: "secret_provider",
		})

		return app.Save(col)
	}, func(app core.App) error {
		// Rollback: remove the field.
		col, err := app.FindCollectionByNameOrId("stack_env_vars")
		if err != nil {
			return err
		}

		field := col.Fields.GetByName("secret_provider")
		if field == nil {
			return nil
		}

		col.Fields.RemoveById(field.GetId())
		return app.Save(col)
	})
}
