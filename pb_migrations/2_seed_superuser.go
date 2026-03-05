package pb_migrations

import (
	"database/sql"
	"errors"
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
		password := os.Getenv("PB_ADMIN_PASSWORD")

		if email == "" || password == "" {
			return fmt.Errorf("migration failed: PB_ADMIN_EMAIL and PB_ADMIN_PASSWORD environment variables must be set")
		}
		if len(password) < 8 {
			log.Printf("[migration] WARNING: PB_ADMIN_PASSWORD must be at least 8 characters (got %d)", len(password))
			return fmt.Errorf("PB_ADMIN_PASSWORD must be at least 8 characters")
		}

		// skip if already exists
		existing, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, email)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to check existing superuser: %w", err)
		}
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
			return fmt.Errorf("rollback failed: PB_ADMIN_EMAIL environment variable must be set")
		}
		record, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, email)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to lookup superuser for deletion: %w", err)
		}
		if record == nil {
			return nil
		}
		return app.Delete(record)
	})
}
