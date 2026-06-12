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

		if col.Fields.GetByName("is_sso") == nil {
			col.Fields.Add(&core.BoolField{Name: "is_sso"})
		}

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added is_sso field to users collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}

		if col.Fields.GetByName("is_sso") != nil {
			col.Fields.RemoveByName("is_sso")
			if err := app.Save(col); err != nil {
				return err
			}
		}

		return nil
	})
}
