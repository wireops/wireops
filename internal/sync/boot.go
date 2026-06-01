package sync

import (
	"log"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// RecoverOrphanState finds and updates any records stuck in transitional states
// (e.g. running, syncing, pending) due to an abrupt server shutdown/restart.
func RecoverOrphanState(app core.App) error {
	log.Println("[boot] starting orphan state recovery...")

	// 1. Recover stuck sync_logs (status: "running" -> "error")
	syncLogs, err := app.FindAllRecords("sync_logs", dbx.HashExp{"status": "running"})
	if err != nil {
		log.Printf("[boot] warning: failed to query running sync logs: %v", err)
	} else {
		for _, rec := range syncLogs {
			rec.Set("status", "error")
			rec.Set("output", "Sincronização interrompida: servidor reiniciado durante a execução.")
			rec.Set("duration_ms", 0)
			if err := app.Save(rec); err != nil {
				log.Printf("[boot] failed to update sync log %s: %v", rec.Id, err)
			} else {
				log.Printf("[boot] recovered stuck sync log %s to error status", rec.Id)
			}
		}
	}

	// 2. Recover stuck stacks (status: "syncing" -> "error")
	stacks, err := app.FindAllRecords("stacks", dbx.HashExp{"status": "syncing"})
	if err != nil {
		log.Printf("[boot] warning: failed to query syncing stacks: %v", err)
	} else {
		for _, rec := range stacks {
			rec.Set("status", "error")
			if err := app.Save(rec); err != nil {
				log.Printf("[boot] failed to update stack %s: %v", rec.Id, err)
			} else {
				log.Printf("[boot] recovered stuck stack %s to error status", rec.Id)
			}
		}
	}

	// 3. Recover stuck job_runs in "pending" status (status: "pending" -> "error")
	// (Note: "running" job runs are handled when workers reconnect)
	jobRuns, err := app.FindAllRecords("job_runs", dbx.HashExp{"status": "pending"})
	if err != nil {
		log.Printf("[boot] warning: failed to query pending job runs: %v", err)
	} else {
		for _, rec := range jobRuns {
			rec.Set("status", "error")
			rec.Set("output", "Execução cancelada: servidor reiniciado antes do despacho.")
			rec.Set("duration_ms", 0)
			if err := app.Save(rec); err != nil {
				log.Printf("[boot] failed to update pending job run %s: %v", rec.Id, err)
			} else {
				log.Printf("[boot] recovered pending job run %s to error status", rec.Id)
			}
		}
	}

	// 4. Recover stuck worker_commands (status: "dispatched" or "acked" -> "error")
	workerCommands, err := app.FindAllRecords("worker_commands", dbx.In("status", "dispatched", "acked"))
	if err != nil {
		log.Printf("[boot] warning: failed to query stuck worker commands: %v", err)
	} else {
		for _, rec := range workerCommands {
			rec.Set("status", "error")
			rec.Set("result", map[string]string{"error": "Comando interrompido: servidor reiniciado durante o despacho."})
			if err := app.Save(rec); err != nil {
				log.Printf("[boot] failed to update worker command %s: %v", rec.Id, err)
			} else {
				log.Printf("[boot] recovered stuck worker command %s to error status", rec.Id)
			}
		}
	}

	log.Println("[boot] orphan state recovery complete.")
	return nil
}
