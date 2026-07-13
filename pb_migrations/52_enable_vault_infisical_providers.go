package pb_migrations

import (
	"fmt"
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Migration 52: enable "vault" and "infisical" as secret providers, now that
// their Resolve() implementations exist (see internal/secrets/vault.go,
// internal/secrets/infisical.go). Extends secrets.ValidProviders and widens
// the DB-level Pattern on every collection's secret_provider field
// accordingly, per the follow-up instructions left in migration 07.
//
// job_env_vars never got a secret_provider field at all (only
// stack_env_vars/global_env_vars did) — added here for parity so job-scoped
// secrets can also reference external providers.
const providerPattern = `^(internal|vault|infisical|)$`

func init() {
	m.Register(func(app core.App) error {
		if err := widenProviderPattern(app, "stack_env_vars"); err != nil {
			return err
		}
		if err := widenProviderPattern(app, "global_env_vars"); err != nil {
			return err
		}
		if err := addProviderFieldToJobEnvVars(app); err != nil {
			return err
		}

		log.Println("[MIGRATE] Enabled vault/infisical secret providers")
		return nil
	}, func(app core.App) error {
		// stack_env_vars originally had Pattern = "^(internal|)$" (migration 07);
		// global_env_vars originally had no Pattern set at all.
		if err := restrictProviderPattern(app, "stack_env_vars", `^(internal|)$`); err != nil {
			return err
		}
		if err := restrictProviderPattern(app, "global_env_vars", ""); err != nil {
			return err
		}

		col, err := app.FindCollectionByNameOrId("job_env_vars")
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

func widenProviderPattern(app core.App, collectionName string) error {
	col, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return err
	}

	field := col.Fields.GetByName("secret_provider")
	if field == nil {
		// Not present yet on this collection — nothing to widen.
		return nil
	}

	textField, ok := field.(*core.TextField)
	if !ok {
		return fmt.Errorf("migration 52: %s.secret_provider is not a TextField, got %T", collectionName, field)
	}

	textField.Pattern = providerPattern
	return app.Save(col)
}

func restrictProviderPattern(app core.App, collectionName, originalPattern string) error {
	col, err := app.FindCollectionByNameOrId(collectionName)
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

	textField.Pattern = originalPattern
	return app.Save(col)
}

func addProviderFieldToJobEnvVars(app core.App) error {
	col, err := app.FindCollectionByNameOrId("job_env_vars")
	if err != nil {
		return err
	}

	if col.Fields.GetByName("secret_provider") != nil {
		return nil
	}

	col.Fields.Add(&core.TextField{
		Name:    "secret_provider",
		Pattern: providerPattern,
	})

	return app.Save(col)
}
