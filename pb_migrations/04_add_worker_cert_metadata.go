package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}

		col.Fields.Add(&core.DateField{Name: "cert_not_after"})
		col.Fields.Add(&core.TextField{Name: "cert_serial"})

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added cert_not_after and cert_serial to workers collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}

		if f := col.Fields.GetByName("cert_not_after"); f != nil {
			col.Fields.RemoveById(f.GetId())
		}
		if f := col.Fields.GetByName("cert_serial"); f != nil {
			col.Fields.RemoveById(f.GetId())
		}

		return app.Save(col)
	})
}
