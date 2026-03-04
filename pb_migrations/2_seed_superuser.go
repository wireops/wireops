package pb_migrations

import (
	"fmt"
	"log"
	"os"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		superusers, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
		if err != nil {
			return err
		}

		email := os.Getenv("PB_ADMIN_EMAIL")
		if email == "" {
			email = "admin@wireops.local"
		}
		password := os.Getenv("PB_ADMIN_PASSWORD")
		if password == "" {
			password = "admin12345"
		}
		if len(password) < 8 {
			log.Printf("[migration] WARNING: PB_ADMIN_PASSWORD must be at least 8 characters (got %d)", len(password))
			return fmt.Errorf("PB_ADMIN_PASSWORD must be at least 8 characters")
		}

		// skip if already exists
		existing, _ := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, email)
		if existing != nil {
			return nil
		}

		record := core.NewRecord(superusers)
		record.Set("email", email)
		record.Set("password", password)
		return app.Save(record)
	}, func(app core.App) error {
		email := os.Getenv("PB_ADMIN_EMAIL")
		if email == "" {
			email = "admin@wireops.local"
		}
		record, _ := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, email)
		if record == nil {
			return nil
		}
		return app.Delete(record)
	})
}
