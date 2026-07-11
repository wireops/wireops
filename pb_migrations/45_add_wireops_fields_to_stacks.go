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

		if col.Fields.GetByName("remove_orphans") == nil {
			col.Fields.Add(&core.BoolField{
				Name: "remove_orphans",
			})
		}
		if col.Fields.GetByName("force_pull") == nil {
			col.Fields.Add(&core.BoolField{
				Name: "force_pull",
			})
		}
		if col.Fields.GetByName("deploy_timeout_seconds") == nil {
			col.Fields.Add(&core.NumberField{
				Name: "deploy_timeout_seconds",
			})
		}
		if col.Fields.GetByName("worker_tags") == nil {
			col.Fields.Add(&core.JSONField{
				Name: "worker_tags",
			})
		}
		if col.Fields.GetByName("config_source") == nil {
			col.Fields.Add(&core.SelectField{
				Name:      "config_source",
				MaxSelect: 1,
				Values:    []string{"manual", "wireops_file"},
			})
		}
		if col.Fields.GetByName("wireops_file_path") == nil {
			col.Fields.Add(&core.TextField{
				Name: "wireops_file_path",
			})
		}

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added wireops.yaml-driven fields to stacks collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		for _, name := range []string{
			"remove_orphans",
			"force_pull",
			"deploy_timeout_seconds",
			"worker_tags",
			"config_source",
			"wireops_file_path",
		} {
			if f := col.Fields.GetByName(name); f != nil {
				col.Fields.RemoveByName(name)
			}
		}

		return app.Save(col)
	})
}
