package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col := core.NewBaseCollection("stack_pending_reconciles")

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
		col.Fields.Add(&core.SelectField{
			Name:   "trigger",
			Values: []string{"cron", "webhook", "manual", "redeploy", "rollback", "transfer"},
		})
		col.Fields.Add(&core.TextField{Name: "commit_sha"})
		
		col.Fields.Add(&core.AutodateField{
			Name:     "created",
			OnCreate: true,
		})
		col.Fields.Add(&core.AutodateField{
			Name:     "updated",
			OnCreate: true,
			OnUpdate: true,
		})

		// Only admins/system can access these
		col.ListRule = nil
		col.ViewRule = nil
		col.CreateRule = nil
		col.UpdateRule = nil
		col.DeleteRule = nil

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stack_pending_reconciles")
		if err == nil {
			return app.Delete(col)
		}
		return nil
	})
}
