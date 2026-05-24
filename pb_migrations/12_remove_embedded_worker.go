package pb_migrations

import (
	"log"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		embeddedWorkers, err := app.FindAllRecords("workers", dbx.HashExp{"fingerprint": "embedded"})
		if err != nil {
			return err
		}

		for _, worker := range embeddedWorkers {
			tokens, tokenErr := app.FindAllRecords("worker_tokens", dbx.HashExp{"worker": worker.Id})
			if tokenErr == nil {
				for _, token := range tokens {
					if err := app.Delete(token); err != nil {
						return err
					}
				}
			}

			if err := app.Delete(worker); err != nil {
				return err
			}
		}

		if len(embeddedWorkers) > 0 {
			log.Printf("[MIGRATE] Removed %d embedded worker record(s)", len(embeddedWorkers))
		}
		return nil
	}, func(app core.App) error {
		return nil
	})
}
