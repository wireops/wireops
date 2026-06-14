package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		optionalCollections := map[string]bool{
			"service_accounts": true,
			"sso_group_roles":  true,
		}
		collections := []string{"users", "service_accounts", "invites", "sso_group_roles"}
		for _, name := range collections {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				if optionalCollections[name] {
					log.Printf("[MIGRATE] Warning: Optional collection %s not found, skipping: %v", name, err)
					continue
				}
				return err
			}

			field := col.Fields.GetByName("role")
			if field != nil {
				if selectField, ok := field.(*core.SelectField); ok {
					hasMonitoring := false
					for _, v := range selectField.Values {
						if v == "monitoring" {
							hasMonitoring = true
							break
						}
					}

					if !hasMonitoring {
						// prepend or append "monitoring" so role rankings work correctly
						selectField.Values = append(selectField.Values, "monitoring")
						if err := app.Save(col); err != nil {
							return err
						}
						log.Printf("[MIGRATE] Added 'monitoring' to %s.role allowed values", name)
					}
				}
			}
		}
		return nil
	}, func(app core.App) error {
		optionalCollections := map[string]bool{
			"service_accounts": true,
			"sso_group_roles":  true,
		}
		collections := []string{"users", "service_accounts", "invites", "sso_group_roles"}
		for _, name := range collections {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				if optionalCollections[name] {
					continue
				}
				return err
			}

			field := col.Fields.GetByName("role")
			if field != nil {
				if selectField, ok := field.(*core.SelectField); ok {
					var newValues []string
					for _, v := range selectField.Values {
						if v != "monitoring" {
							newValues = append(newValues, v)
						}
					}
					selectField.Values = newValues
					if err := app.Save(col); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}
