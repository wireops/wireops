package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collections := []string{"users", "service_accounts", "invites", "sso_group_roles"}
		for _, name := range collections {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				log.Printf("[MIGRATE] Warning: Collection %s not found, skipping: %v", name, err)
				continue
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
		collections := []string{"users", "service_accounts", "invites", "sso_group_roles"}
		for _, name := range collections {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue
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
