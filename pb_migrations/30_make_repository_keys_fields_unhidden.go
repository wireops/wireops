package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("repository_keys")
		if err != nil {
			return err
		}

		fields := []string{"ssh_private_key", "ssh_passphrase", "git_password"}
		changed := false
		for _, name := range fields {
			field := col.Fields.GetByName(name)
			if field != nil {
				if textField, ok := field.(*core.TextField); ok {
					textField.Hidden = false
					changed = true
				}
			}
		}

		if changed {
			if err := app.Save(col); err != nil {
				return err
			}
			log.Println("[MIGRATE] Exposed 'ssh_private_key', 'ssh_passphrase', and 'git_password' fields on repository_keys collection")
		}
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("repository_keys")
		if err != nil {
			return err
		}

		fields := []string{"ssh_private_key", "ssh_passphrase", "git_password"}
		changed := false
		for _, name := range fields {
			field := col.Fields.GetByName(name)
			if field != nil {
				if textField, ok := field.(*core.TextField); ok {
					textField.Hidden = true
					changed = true
				}
			}
		}

		if changed {
			if err := app.Save(col); err != nil {
				return err
			}
		}
		return nil
	})
}
