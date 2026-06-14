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

		col.Fields.Add(&core.TextField{Name: "docker_version"})
		col.Fields.Add(&core.TextField{Name: "compose_version"})
		col.Fields.Add(&core.TextField{Name: "os"})
		col.Fields.Add(&core.TextField{Name: "arch"})
		col.Fields.Add(&core.NumberField{Name: "cpu_usage"})
		col.Fields.Add(&core.NumberField{Name: "memory_usage"})
		col.Fields.Add(&core.NumberField{Name: "disk_usage"})
		col.Fields.Add(&core.BoolField{Name: "docker_online"})

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added worker info and telemetry fields to workers collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}

		col.Fields.RemoveByName("docker_version")
		col.Fields.RemoveByName("compose_version")
		col.Fields.RemoveByName("os")
		col.Fields.RemoveByName("arch")
		col.Fields.RemoveByName("cpu_usage")
		col.Fields.RemoveByName("memory_usage")
		col.Fields.RemoveByName("disk_usage")
		col.Fields.RemoveByName("docker_online")

		return app.Save(col)
	})
}
