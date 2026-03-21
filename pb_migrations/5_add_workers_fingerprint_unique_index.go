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

		col.Indexes = append(col.Indexes, "CREATE UNIQUE INDEX IF NOT EXISTS idx_workers_fingerprint ON workers (fingerprint)")

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added unique index on workers.fingerprint")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}

		for i, idx := range col.Indexes {
			if idx == "CREATE UNIQUE INDEX IF NOT EXISTS idx_workers_fingerprint ON workers (fingerprint)" {
				col.Indexes = append(col.Indexes[:i], col.Indexes[i+1:]...)
				break
			}
		}

		return app.Save(col)
	})
}
