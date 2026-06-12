package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		
		field := col.Fields.GetByName("protected")
		if field != nil {
			if boolField, ok := field.(*core.BoolField); ok {
				boolField.Hidden = false
				if err := app.Save(col); err != nil {
					return err
				}
				log.Println("[MIGRATE] Exposed 'protected' field on users collection")
			}
		}
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		field := col.Fields.GetByName("protected")
		if field != nil {
			if boolField, ok := field.(*core.BoolField); ok {
				boolField.Hidden = true
				if err := app.Save(col); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
