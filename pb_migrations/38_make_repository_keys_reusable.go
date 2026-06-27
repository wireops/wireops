package pb_migrations

import (
	"fmt"
	"log"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(migrateReusableRepositoryKeys, func(app core.App) error {
		// Reverting a reusable many-to-one relationship to the former one-to-one
		// model would require duplicating encrypted credentials. Keep the migrated
		// data intact instead of performing a lossy rollback.
		return nil
	})
}

func migrateReusableRepositoryKeys(app core.App) error {
	repositories, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		return err
	}
	keys, err := app.FindCollectionByNameOrId("repository_keys")
	if err != nil {
		return err
	}

	if keys.Fields.GetByName("name") == nil {
		keys.Fields.Add(&core.TextField{Name: "name"})
	}
	if repositories.Fields.GetByName("repository_key") == nil {
		repositories.Fields.Add(&core.RelationField{
			Name:         "repository_key",
			CollectionId: keys.Id,
			MaxSelect:    1,
		})
	}
	if err := app.Save(keys); err != nil {
		return err
	}
	if err := app.Save(repositories); err != nil {
		return err
	}

	keyRecords, err := app.FindAllRecords("repository_keys")
	if err != nil {
		return err
	}
	for _, key := range keyRecords {
		repositoryID := strings.TrimSpace(key.GetString("repository"))
		if key.GetString("auth_type") == "none" {
			if _, err := app.DB().Delete("repository_keys", dbx.HashExp{"id": key.Id}).Execute(); err != nil {
				return fmt.Errorf("delete obsolete public repository key %s: %w", key.Id, err)
			}
			continue
		}

		name := "Repository credentials"
		if repositoryID != "" {
			if repository, findErr := app.FindRecordById("repositories", repositoryID); findErr == nil {
				if repositoryName := strings.TrimSpace(repository.GetString("name")); repositoryName != "" {
					name = repositoryName + " credentials"
				}
				if _, err := app.DB().Update("repository_keys", dbx.Params{"name": name}, dbx.HashExp{"id": key.Id}).Execute(); err != nil {
					return fmt.Errorf("name repository key %s: %w", key.Id, err)
				}
				if _, err := app.DB().Update("repositories", dbx.Params{"repository_key": key.Id}, dbx.HashExp{"id": repository.Id}).Execute(); err != nil {
					return fmt.Errorf("associate repository %s with key %s: %w", repository.Id, key.Id, err)
				}
				continue
			}
		}

		suffix := key.Id
		if len(suffix) > 6 {
			suffix = suffix[:6]
		}
		name = fmt.Sprintf("%s %s", name, suffix)
		if _, err := app.DB().Update("repository_keys", dbx.Params{"name": name}, dbx.HashExp{"id": key.Id}).Execute(); err != nil {
			return fmt.Errorf("name orphan repository key %s: %w", key.Id, err)
		}
	}

	keys, err = app.FindCollectionByNameOrId("repository_keys")
	if err != nil {
		return err
	}
	if nameField, ok := keys.Fields.GetByName("name").(*core.TextField); ok {
		nameField.Required = true
	}
	if authField, ok := keys.Fields.GetByName("auth_type").(*core.SelectField); ok {
		authField.Values = []string{"ssh_key", "basic"}
		authField.Required = true
	}
	keys.Fields.RemoveByName("repository")
	for i := len(keys.Indexes) - 1; i >= 0; i-- {
		if keys.Indexes[i] == repositoryKeysRepositoryIndexSQL {
			keys.Indexes = append(keys.Indexes[:i], keys.Indexes[i+1:]...)
		}
	}
	if err := app.Save(keys); err != nil {
		return err
	}

	log.Println("[MIGRATE] Made repository keys reusable across repositories")
	return nil
}
