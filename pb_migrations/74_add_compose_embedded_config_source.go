package pb_migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Migration 74: add "compose_embedded" as a stacks.config_source value.
//
// A stack can now be driven by an "x-wireops" extension block embedded
// directly in the compose file (see internal/manifest.ParseComposeManifest),
// instead of a separate wireops.yaml. It's treated the same as
// config_source == "wireops_file" everywhere: fields are copied into the
// stack record at creation time and become immutable via the API.
func init() {
	m.Register(func(app core.App) error {
		stacks, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		field := stacks.Fields.GetByName("config_source")
		selectField, ok := field.(*core.SelectField)
		if !ok {
			return fmt.Errorf("migration 74: stacks.config_source is %T, want SelectField", field)
		}
		selectField.Values = []string{"manual", "wireops_file", "compose_embedded"}
		return app.Save(stacks)
	}, func(app core.App) error {
		// Raw update, not app.Save(record): compose_embedded is one of the
		// wireopsManagedStackFields, so going through the record API would
		// trip validateWireopsFieldsImmutable (internal/hooks/pb_hooks.go)
		// on every row and abort the rollback.
		if _, err := app.DB().NewQuery(
			"UPDATE stacks SET config_source = 'wireops_file' WHERE config_source = 'compose_embedded'",
		).Execute(); err != nil {
			return fmt.Errorf("migration 74 rollback: normalize compose_embedded stacks: %w", err)
		}

		stacks, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		field := stacks.Fields.GetByName("config_source")
		selectField, ok := field.(*core.SelectField)
		if !ok {
			return fmt.Errorf("migration 74 rollback: stacks.config_source is %T, want SelectField", field)
		}
		selectField.Values = []string{"manual", "wireops_file"}
		return app.Save(stacks)
	})
}
