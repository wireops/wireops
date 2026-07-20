package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		// 1. Allow render-time overrides to be persisted per stack (ephemeral, not
		// committed to git — applied only on explicit redeploy-with-overrides).
		stacks, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		if stacks.Fields.GetByName("render_overrides") == nil {
			stacks.Fields.Add(&core.JSONField{Name: "render_overrides"})
		}
		if err := app.Save(stacks); err != nil {
			return err
		}

		// 2. Gate the capability behind a worker policy flag, off by default.
		pol, err := app.FindCollectionByNameOrId("worker_policies")
		if err != nil {
			return err
		}
		if pol.Fields.GetByName("allow_render_overrides") == nil {
			pol.Fields.Add(&core.BoolField{Name: "allow_render_overrides"})
		}
		if err := app.Save(pol); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added render_overrides to stacks and allow_render_overrides to worker_policies")
		return nil
	}, func(app core.App) error {
		stacks, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		stacks.Fields.RemoveByName("render_overrides")
		if err := app.Save(stacks); err != nil {
			return err
		}

		pol, err := app.FindCollectionByNameOrId("worker_policies")
		if err != nil {
			return err
		}
		pol.Fields.RemoveByName("allow_render_overrides")
		return app.Save(pol)
	})
}
