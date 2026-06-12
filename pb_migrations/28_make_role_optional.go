package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/wireops/wireops/internal/oidc"
)

func init() {
	m.Register(func(app core.App) error {
		for _, name := range []string{"users", "sso_users"} {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue
			}
			field := col.Fields.GetByName("role")
			if field != nil {
				if selectField, ok := field.(*core.SelectField); ok {
					selectField.Required = false
					if name == "sso_users" {
						oidc.HydrateClientSecretForValidation(col)
					}
					if err := app.Save(col); err != nil {
						return err
					}
				}
			}
		}
		log.Println("[MIGRATE] Made role field optional on users and sso_users to fix OAuth2 signups")
		return nil
	}, func(app core.App) error {
		return nil
	})
}
