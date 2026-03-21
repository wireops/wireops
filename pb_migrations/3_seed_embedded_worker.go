package pb_migrations

import (
	"log"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		workersCol, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}

		existing, _ := app.FindAllRecords("workers", dbx.HashExp{"fingerprint": "embedded"})
		if len(existing) > 0 {
			return nil
		}

		record := core.NewRecord(workersCol)
		record.Set("hostname", "Server (Embedded)")
		record.Set("fingerprint", "embedded")
		record.Set("status", "ACTIVE")

		if err := app.Save(record); err != nil {
			return err
		}

		log.Println("[MIGRATE] Created embedded server worker")
		return nil
	}, func(app core.App) error {
		records, _ := app.FindAllRecords("workers", dbx.HashExp{"fingerprint": "embedded"})
		for _, rec := range records {
			_ = app.Delete(rec)
		}
		return nil
	})
}
