package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stack_sync_events")
		if err != nil {
			// Already deleted or doesn't exist
			return nil
		}
		if err := app.Delete(col); err != nil {
			return err
		}
		log.Println("[MIGRATE] Dropped collection stack_sync_events")
		return nil
	}, func(app core.App) error {
		return nil
	})
}
