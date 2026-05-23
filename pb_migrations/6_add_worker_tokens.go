package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const workerTokenHashIndexSQL = "CREATE UNIQUE INDEX IF NOT EXISTS idx_worker_tokens_token_hash ON worker_tokens (token_hash)"

func init() {
	m.Register(func(app core.App) error {
		workersCol, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}

		col := core.NewBaseCollection("worker_tokens")
		col.Fields.Add(&core.TextField{Name: "token_hash", Required: true})
		col.Fields.Add(&core.SelectField{
			Name:      "status",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"STAGING", "ACTIVE", "REVOKED", "EXPIRED"},
		})
		col.Fields.Add(&core.RelationField{
			Name:         "worker",
			CollectionId: workersCol.Id,
			Required:     false,
			MaxSelect:    1,
		})
		col.Fields.Add(&core.DateField{Name: "expires_at", Required: true})
		col.Fields.Add(&core.DateField{Name: "last_used_at"})
		col.Fields.Add(&core.TextField{Name: "created_by"})
		addAutoDateFields(col)

		col.ListRule = nil
		col.ViewRule = nil
		col.CreateRule = nil
		col.UpdateRule = nil
		col.DeleteRule = nil
		col.Indexes = append(col.Indexes, workerTokenHashIndexSQL)

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Created worker_tokens collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("worker_tokens")
		if err != nil {
			return err
		}
		return app.Delete(col)
	})
}
