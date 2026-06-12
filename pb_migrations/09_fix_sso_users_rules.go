package pb_migrations

import (
	"database/sql"
	"errors"

	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/pocketbase/pocketbase/core"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("sso_users")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Nothing to update if the collection does not exist yet.
				return nil
			}
			return err
		}

		col.ViewRule = strPtr("id = @request.auth.id")
		col.CreateRule = strPtr("")
		col.UpdateRule = strPtr("id = @request.auth.id")

		return app.Save(col)
	}, nil)
}
