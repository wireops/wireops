package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		col.Fields.Add(&core.NumberField{Name: "deployed_version"})
		col.Fields.Add(&core.TextField{Name: "deployed_commit"})
		col.Fields.Add(&core.TextField{Name: "deployed_checksum"})
		col.Fields.Add(&core.DateField{Name: "deployed_at"})

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added deployed_version, deployed_commit, deployed_checksum, deployed_at to stacks collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		for _, name := range []string{"deployed_version", "deployed_commit", "deployed_checksum", "deployed_at"} {
			if f := col.Fields.GetByName(name); f != nil {
				col.Fields.RemoveById(f.GetId())
			}
		}

		return app.Save(col)
	})
}
