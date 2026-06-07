package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		// 1. Create worker_policies singleton collection (global defaults).
		pol := core.NewBaseCollection("worker_policies")
		// allowed_volumes: JSON array of host-path prefixes or named volume names.
		pol.Fields.Add(&core.JSONField{Name: "allowed_volumes"})
		// allowed_networks: JSON array of Docker network names.
		pol.Fields.Add(&core.JSONField{Name: "allowed_networks"})
		// allowed_images: JSON array of image patterns (supports glob wildcards).
		pol.Fields.Add(&core.JSONField{Name: "allowed_images"})
		pol.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		pol.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		// Superusers only — no public access.
		pol.ListRule = nil
		pol.ViewRule = nil
		pol.CreateRule = nil
		pol.UpdateRule = nil
		pol.DeleteRule = nil

		if err := app.Save(pol); err != nil {
			return err
		}

		// 2. Add per-worker policy override fields to the workers collection.
		workers, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}

		// policy_inherit: when true (default), missing local fields fall back to global policy.
		workers.Fields.Add(&core.BoolField{Name: "policy_inherit"})
		// policy_volumes / policy_networks / policy_images: worker-local overrides (JSON arrays).
		// When non-null and policy_inherit=false, they fully replace the global policy.
		workers.Fields.Add(&core.JSONField{Name: "policy_volumes"})
		workers.Fields.Add(&core.JSONField{Name: "policy_networks"})
		workers.Fields.Add(&core.JSONField{Name: "policy_images"})

		if err := app.Save(workers); err != nil {
			return err
		}

		log.Println("[MIGRATE] Created worker_policies collection and added policy fields to workers")
		return nil
	}, func(app core.App) error {
		// Remove policy fields from workers.
		workers, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}
		workers.Fields.RemoveByName("policy_inherit")
		workers.Fields.RemoveByName("policy_volumes")
		workers.Fields.RemoveByName("policy_networks")
		workers.Fields.RemoveByName("policy_images")
		if err := app.Save(workers); err != nil {
			return err
		}

		// Drop the worker_policies collection.
		col, err := app.FindCollectionByNameOrId("worker_policies")
		if err != nil {
			return err
		}
		return app.Delete(col)
	})
}
