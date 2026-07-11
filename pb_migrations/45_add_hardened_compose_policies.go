package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		// 1. Add hardened boolean policy flags + new allowlists to the worker_policies singleton.
		pol, err := app.FindCollectionByNameOrId("worker_policies")
		if err != nil {
			return err
		}
		pol.Fields.Add(&core.BoolField{Name: "block_privileged"})
		pol.Fields.Add(&core.BoolField{Name: "block_host_network"})
		pol.Fields.Add(&core.BoolField{Name: "block_host_pid"})
		pol.Fields.Add(&core.BoolField{Name: "block_host_ipc"})
		pol.Fields.Add(&core.BoolField{Name: "block_docker_socket"})
		pol.Fields.Add(&core.JSONField{Name: "allowed_cap_add"})
		pol.Fields.Add(&core.JSONField{Name: "allowed_devices"})
		pol.Fields.Add(&core.JSONField{Name: "allowed_security_opt"})
		if err := app.Save(pol); err != nil {
			return err
		}

		// 2. Add per-worker override allowlists to the workers collection.
		// The 5 new boolean flags are stored inside the existing policy_flags JSON blob
		// (no schema change needed there).
		workers, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}
		workers.Fields.Add(&core.JSONField{Name: "policy_cap_add"})
		workers.Fields.Add(&core.JSONField{Name: "policy_devices"})
		workers.Fields.Add(&core.JSONField{Name: "policy_security_opt"})
		if err := app.Save(workers); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added privileged/host-network/host-pid/host-ipc/docker-socket policy flags and cap_add/devices/security_opt allowlists")
		return nil
	}, func(app core.App) error {
		workers, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}
		workers.Fields.RemoveByName("policy_cap_add")
		workers.Fields.RemoveByName("policy_devices")
		workers.Fields.RemoveByName("policy_security_opt")
		if err := app.Save(workers); err != nil {
			return err
		}

		pol, err := app.FindCollectionByNameOrId("worker_policies")
		if err != nil {
			return err
		}
		pol.Fields.RemoveByName("block_privileged")
		pol.Fields.RemoveByName("block_host_network")
		pol.Fields.RemoveByName("block_host_pid")
		pol.Fields.RemoveByName("block_host_ipc")
		pol.Fields.RemoveByName("block_docker_socket")
		pol.Fields.RemoveByName("allowed_cap_add")
		pol.Fields.RemoveByName("allowed_devices")
		pol.Fields.RemoveByName("allowed_security_opt")
		return app.Save(pol)
	})
}
