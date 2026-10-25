package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// last_error_commit deliberately starts empty for historical errors. A prior
// last_error record does not prove which revision failed, so migration must not
// guess from deployed_commit, desired_commit, or a repository's moving HEAD.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		if col.Fields.GetByName("last_error_commit") == nil {
			col.Fields.Add(&core.TextField{Name: "last_error_commit", Max: 128})
			if err := app.Save(col); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		col.Fields.RemoveByName("last_error_commit")
		return app.Save(col)
	})
}
