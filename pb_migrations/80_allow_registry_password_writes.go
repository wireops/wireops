package pb_migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// PocketBase ignores schema-hidden fields in non-superuser CRUD requests.
// Registry passwords are write-only through the record-enrich hook instead;
// collection rules still restrict writes and save hooks encrypt the value.
func init() {
	m.Register(func(app core.App) error {
		return setRegistryPasswordHidden(app, false)
	}, func(app core.App) error {
		return setRegistryPasswordHidden(app, true)
	})
}

func setRegistryPasswordHidden(app core.App, hidden bool) error {
	collection, err := app.FindCollectionByNameOrId("registry_credentials")
	if err != nil {
		return err
	}
	field := collection.Fields.GetByName("password")
	if field == nil {
		return fmt.Errorf("registry credential password field is missing")
	}
	field.SetHidden(hidden)
	return app.Save(collection)
}
