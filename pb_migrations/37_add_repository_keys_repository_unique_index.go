package pb_migrations

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const repositoryKeysRepositoryIndexSQL = "CREATE UNIQUE INDEX IF NOT EXISTS idx_repository_keys_repository_unique ON repository_keys (repository)"

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("repository_keys")
		if err != nil {
			return err
		}

		for _, idx := range col.Indexes {
			if idx == repositoryKeysRepositoryIndexSQL {
				return nil
			}
		}

		var duplicate struct {
			Repository string `db:"repository"`
			Count      int    `db:"count"`
		}
		err = app.DB().
			NewQuery("SELECT repository, COUNT(*) AS count FROM repository_keys GROUP BY repository HAVING COUNT(*) > 1 LIMIT 1").
			One(&duplicate)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			return fmt.Errorf("cannot add unique repository_keys.repository index: repository %s has %d credential records", duplicate.Repository, duplicate.Count)
		}

		col.Indexes = append(col.Indexes, repositoryKeysRepositoryIndexSQL)
		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added unique index on repository_keys.repository")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("repository_keys")
		if err != nil {
			return err
		}

		for i, idx := range col.Indexes {
			if idx == repositoryKeysRepositoryIndexSQL {
				col.Indexes = append(col.Indexes[:i], col.Indexes[i+1:]...)
				break
			}
		}

		return app.Save(col)
	})
}
