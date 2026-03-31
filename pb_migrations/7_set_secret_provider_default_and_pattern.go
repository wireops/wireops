package pb_migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Migration 7: tighten the secret_provider field on stack_env_vars.
//
//   - Sets Pattern to restrict accepted values to implemented providers only,
//     preventing vault/infisical (stub Resolve) from being persisted and
//     causing guaranteed deploy-time failures.
//
// The application-level default ("internal") is enforced by the OnRecordCreate
// hook in internal/hooks/pb_hooks.go rather than at the schema level because
// core.TextField does not expose a DefaultValue field in PocketBase v0.36.
//
// When vault or infisical Resolve() is implemented, add a new migration that:
//  1. Updates the Pattern to include the new provider name.
//  2. Extends secrets.ValidProviders in internal/secrets/provider.go.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stack_env_vars")
		if err != nil {
			return err
		}

		field := col.Fields.GetByName("secret_provider")
		if field == nil {
			// Field was not created by migration 6 yet — nothing to tighten.
			return nil
		}

		textField, ok := field.(*core.TextField)
		if !ok {
			return fmt.Errorf("migration 7: secret_provider is not a TextField, got %T", field)
		}

		// Allow empty (defaults to "internal" in the application layer) or
		// exactly "internal" (the only provider with a working Resolve()).
		textField.Pattern = `^(internal|)$`

		return app.Save(col)
	}, func(app core.App) error {
		// Rollback: clear Pattern.
		col, err := app.FindCollectionByNameOrId("stack_env_vars")
		if err != nil {
			return err
		}

		field := col.Fields.GetByName("secret_provider")
		if field == nil {
			return nil
		}

		textField, ok := field.(*core.TextField)
		if !ok {
			return nil
		}

		textField.Pattern = ""
		return app.Save(col)
	})
}
