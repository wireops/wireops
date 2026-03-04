package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col := core.NewBaseCollection("agents")
		col.Name = "Agents"
		
		col.Fields.Add(&core.TextField{Name: "hostname", Required: true})
		col.Fields.Add(&core.TextField{Name: "fingerprint", Required: true})
		col.Fields.Add(&core.SelectField{
			Name: "status", 
			Values: []string{"ACTIVE", "REVOKED"},
			MaxSelect: 1, 
			Required: true,
		})
		col.Fields.Add(&core.AutodateField{Name: "last_seen", OnCreate: true, OnUpdate: true})

		col.ListRule = strPtr("@request.auth.id != ''")
		col.ViewRule = strPtr("@request.auth.id != ''")
		col.CreateRule = nil // System only
		col.UpdateRule = nil // System only
		col.DeleteRule = nil // System only

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("agents")
		if err == nil {
			return app.Delete(col)
		}
		return nil
	})
}
