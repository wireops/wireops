package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		// Create stack_revisions collection
		col := core.NewBaseCollection("stack_revisions")

		stacksCol, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}

		col.Fields.Add(&core.RelationField{
			Name:         "stack",
			CollectionId: stacksCol.Id,
			Required:     true,
			MaxSelect:    1,
		})
		col.Fields.Add(&core.NumberField{Name: "version", Required: true})
		col.Fields.Add(&core.TextField{Name: "commit_sha", Required: true})
		col.Fields.Add(&core.TextField{Name: "checksum", Required: true})
		col.Fields.Add(&core.TextField{Name: "compose_path", Required: true})
		
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		col.ListRule = strPtr("@request.auth.id != ''")
		col.ViewRule = strPtr("@request.auth.id != ''")
		col.CreateRule = nil // System only
		col.UpdateRule = nil // System only
		col.DeleteRule = nil // System only

		if err := app.Save(col); err != nil {
			return err
		}

		// Update stacks collection
		stacksCol.Fields.Add(&core.NumberField{Name: "current_version"})
		stacksCol.Fields.Add(&core.TextField{Name: "desired_commit"})
		stacksCol.Fields.Add(&core.TextField{Name: "checksum"})

		return app.Save(stacksCol)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stack_revisions")
		if err == nil {
			_ = app.Delete(col)
		}

		stacksCol, err := app.FindCollectionByNameOrId("stacks")
		if err == nil {
			stacksCol.Fields.RemoveByName("current_version")
			stacksCol.Fields.RemoveByName("desired_commit")
			stacksCol.Fields.RemoveByName("checksum")
			_ = app.Save(stacksCol)
		}

		return nil
	})
}
