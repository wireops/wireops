package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		// 1. Add boolean policy flags to the worker_policies singleton.
		pol, err := app.FindCollectionByNameOrId("worker_policies")
		if err != nil {
			return err
		}
		// prevent_latest_images: reject images with no tag or :latest tag.
		pol.Fields.Add(&core.BoolField{Name: "prevent_latest_images"})
		// block_host_volumes: reject bind-mounts (host paths); only named volumes are allowed.
		pol.Fields.Add(&core.BoolField{Name: "block_host_volumes"})
		if err := app.Save(pol); err != nil {
			return err
		}

		// 2. Add per-worker override flags to the workers collection.
		workers, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}
		// Stored as JSON so we can represent three states: null (inherit), true, false.
		// Wire format: {"prevent_latest_images": true|false|null, "block_host_volumes": true|false|null}
		workers.Fields.Add(&core.JSONField{Name: "policy_flags"})
		if err := app.Save(workers); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added prevent_latest_images + block_host_volumes to worker_policies and policy_flags to workers")
		return nil
	}, func(app core.App) error {
		// Remove policy_flags from workers.
		workers, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}
		workers.Fields.RemoveByName("policy_flags")
		if err := app.Save(workers); err != nil {
			return err
		}

		// Remove flags from worker_policies.
		pol, err := app.FindCollectionByNameOrId("worker_policies")
		if err != nil {
			return err
		}
		pol.Fields.RemoveByName("prevent_latest_images")
		pol.Fields.RemoveByName("block_host_volumes")
		return app.Save(pol)
	})
}
