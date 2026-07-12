package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/wireops/wireops/internal/constants"
)

const syncLogPhasesSyncLogSeqIndexSQL = "CREATE INDEX IF NOT EXISTS idx_sync_log_phases_sync_log_seq ON sync_log_phases (sync_log, seq)"
const syncLogsCorrelationIdIndexSQL = "CREATE INDEX IF NOT EXISTS idx_sync_logs_correlation_id ON sync_logs (correlation_id)"

func init() {
	m.Register(func(app core.App) error {
		// sync_logs.correlation_id: explicit, documented alias for the id
		// already used to correlate a deploy's worker_commands row
		// (worker_commands.command_id == sync_logs.id for the deploy dispatch).
		syncLogsCol, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}
		if syncLogsCol.Fields.GetByName("correlation_id") == nil {
			syncLogsCol.Fields.Add(&core.TextField{
				Name: "correlation_id",
			})
		}
		syncLogsCol.Indexes = append(syncLogsCol.Indexes, syncLogsCorrelationIdIndexSQL)
		if err := app.Save(syncLogsCol); err != nil {
			return err
		}

		// worker_commands.acked_at: timestamp of the worker's receipt ack,
		// distinct from the transient "acked" status value (which gets
		// overwritten once the command reaches a terminal state) — needed to
		// compute a worker_ack phase duration in the deploy timeline.
		workerCommandsCol, err := app.FindCollectionByNameOrId("worker_commands")
		if err != nil {
			return err
		}
		if workerCommandsCol.Fields.GetByName("acked_at") == nil {
			workerCommandsCol.Fields.Add(&core.DateField{
				Name: "acked_at",
			})
		}
		if err := app.Save(workerCommandsCol); err != nil {
			return err
		}

		// sync_log_phases: structured per-phase timeline entries for a
		// sync_logs row (P2.1 - Linha do tempo de deploy).
		syncLogsColForRelation, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}

		col := core.NewBaseCollection("sync_log_phases")
		col.Fields.Add(&core.RelationField{
			Name:         "sync_log",
			CollectionId: syncLogsColForRelation.Id,
			Required:     true,
			MaxSelect:    1,
		})
		col.Fields.Add(&core.SelectField{
			Name:      "phase",
			Required:  true,
			MaxSelect: 1,
			Values:    constants.DeployPhaseOrder,
		})
		col.Fields.Add(&core.SelectField{
			Name:      "status",
			Required:  true,
			MaxSelect: 1,
			Values: []string{
				constants.PhaseStatusRunning, constants.PhaseStatusSuccess,
				constants.PhaseStatusError, constants.PhaseStatusSkipped,
			},
		})
		col.Fields.Add(&core.DateField{
			Name:     "started_at",
			Required: true,
		})
		col.Fields.Add(&core.NumberField{
			Name: "duration_ms",
		})
		col.Fields.Add(&core.TextField{
			Name: "detail",
			Max:  10000,
		})
		col.Fields.Add(&core.NumberField{
			Name: "seq",
		})

		addAutoDateFields(col)

		col.ListRule = strPtr("@request.auth.id != ''")
		col.ViewRule = strPtr("@request.auth.id != ''")
		col.CreateRule = nil // System only
		col.UpdateRule = nil // System only
		col.DeleteRule = nil // System only

		col.Indexes = append(col.Indexes, syncLogPhasesSyncLogSeqIndexSQL)

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added deploy timeline: sync_logs.correlation_id, worker_commands.acked_at, sync_log_phases collection")
		return nil
	}, func(app core.App) error {
		if col, err := app.FindCollectionByNameOrId("sync_log_phases"); err == nil {
			if err := app.Delete(col); err != nil {
				return err
			}
		}

		workerCommandsCol, err := app.FindCollectionByNameOrId("worker_commands")
		if err != nil {
			return err
		}
		if f := workerCommandsCol.Fields.GetByName("acked_at"); f != nil {
			workerCommandsCol.Fields.RemoveByName("acked_at")
		}
		if err := app.Save(workerCommandsCol); err != nil {
			return err
		}

		syncLogsCol, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}
		if f := syncLogsCol.Fields.GetByName("correlation_id"); f != nil {
			syncLogsCol.Fields.RemoveByName("correlation_id")
		}
		var newIndexes []string
		for _, idx := range syncLogsCol.Indexes {
			if idx != syncLogsCorrelationIdIndexSQL {
				newIndexes = append(newIndexes, idx)
			}
		}
		syncLogsCol.Indexes = newIndexes
		if err := app.Save(syncLogsCol); err != nil {
			return err
		}

		return nil
	})
}
