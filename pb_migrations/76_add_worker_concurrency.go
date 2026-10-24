package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// workerConcurrencyFields are reported by the worker on every heartbeat:
// heavy_concurrency is its configured deploy slot count
// (WORKER_HEAVY_CONCURRENCY), active_heavy/queued_heavy the live occupancy of
// that pool. Workers older than this never send them, so the fields stay 0.
var workerConcurrencyFields = []string{"heavy_concurrency", "active_heavy", "queued_heavy"}

func init() {
	m.Register(func(app core.App) error {
		workers, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}
		for _, name := range workerConcurrencyFields {
			if workers.Fields.GetByName(name) == nil {
				workers.Fields.Add(&core.NumberField{Name: name, OnlyInt: true})
			}
		}
		return app.Save(workers)
	}, func(app core.App) error {
		workers, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}
		for _, name := range workerConcurrencyFields {
			workers.Fields.RemoveByName(name)
		}
		return app.Save(workers)
	})
}
