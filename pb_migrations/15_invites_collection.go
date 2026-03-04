package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col := core.NewBaseCollection("invites")

		col.Fields.Add(&core.TextField{Name: "email", Required: true})
		col.Fields.Add(&core.TextField{Name: "token", Required: true})
		col.Fields.Add(&core.DateField{Name: "expires_at", Required: true})
		col.Fields.Add(&core.BoolField{Name: "used"})
		col.Fields.Add(&core.TextField{Name: "created_by"})

		// No direct public access — all operations go through custom routes
		col.ListRule = nil
		col.ViewRule = nil
		col.CreateRule = nil
		col.UpdateRule = nil
		col.DeleteRule = nil

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("invites")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
