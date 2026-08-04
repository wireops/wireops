package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("repositories")
		if err != nil {
			return err
		}

		if col.Fields.GetByName("sync_interval_seconds") == nil {
			// Min/Max/OnlyInt keep the value in a range that can safely be
			// converted to a time.Duration: an unbounded value overflows
			// `time.Duration(seconds) * time.Second` and wraps negative,
			// which panics time.NewTicker when the repo ticker starts.
			// The ceiling mirrors sync.maxSyncIntervalSeconds (1 year).
			minInterval, maxInterval := 0.0, float64(365*24*60*60)
			col.Fields.Add(&core.NumberField{
				Name:    "sync_interval_seconds",
				OnlyInt: true,
				Min:     &minInterval,
				Max:     &maxInterval,
			})
		}

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added sync_interval_seconds to repositories collection (repo-level fetch tick override; 0 = global SCAN_PERIOD)")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("repositories")
		if err != nil {
			return err
		}

		if f := col.Fields.GetByName("sync_interval_seconds"); f != nil {
			col.Fields.RemoveByName("sync_interval_seconds")
		}

		return app.Save(col)
	})
}
