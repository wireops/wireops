package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Migration 54: secret_key_canary collection.
//
// Holds a single row with a fixed plaintext string encrypted under the
// runtime SECRET_KEY. Written on first boot after this migration runs (see
// crypto.VerifyOrSeedSecretKeyCanary, called from cmd/serve.go's
// OnBootstrap after validateStartupSecretKey succeeds) — not seeded here,
// since the migration may run before SECRET_KEY has been validated as
// well-formed. Its only purpose is to let startup detect a SECRET_KEY that
// no longer matches this DATA_DIR (e.g. after restoring a backup onto a
// host with the wrong key) instead of silently corrupting encrypted stack
// secrets.
func init() {
	m.Register(func(app core.App) error {
		col := core.NewBaseCollection("secret_key_canary")
		col.Fields.Add(&core.TextField{Name: "value", Required: true, Hidden: true})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		// Superusers only — no public access.
		col.ListRule = nil
		col.ViewRule = nil
		col.CreateRule = nil
		col.UpdateRule = nil
		col.DeleteRule = nil

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Created secret_key_canary collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("secret_key_canary")
		if err != nil {
			return err
		}
		return app.Delete(col)
	})
}
