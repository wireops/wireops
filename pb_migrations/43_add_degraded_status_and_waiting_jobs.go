package pb_migrations

import (
	"fmt"
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// addSelectValue appends value to a SelectField's allowed values if not
// already present, returning whether it changed anything.
func addSelectValue(selectField *core.SelectField, value string) bool {
	for _, v := range selectField.Values {
		if v == value {
			return false
		}
	}
	selectField.Values = append(selectField.Values, value)
	return true
}

func removeSelectValue(selectField *core.SelectField, value string) {
	var newValues []string
	for _, v := range selectField.Values {
		if v != value {
			newValues = append(newValues, v)
		}
	}
	selectField.Values = newValues
}

func init() {
	m.Register(func(app core.App) error {
		stacksCol, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		stacksStatus, ok := stacksCol.Fields.GetByName("status").(*core.SelectField)
		if !ok {
			return fmt.Errorf("stacks.status field not found or wrong type")
		}
		// "degraded": post-deploy check found some but not all expected
		// services running/healthy — distinct from "active" (fully up) and
		// "error" (deploy itself failed / nothing came up).
		changedStacks := addSelectValue(stacksStatus, "degraded")
		if changedStacks {
			if err := app.Save(stacksCol); err != nil {
				return err
			}
		}

		syncLogsCol, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}
		syncLogsStatus, ok := syncLogsCol.Fields.GetByName("status").(*core.SelectField)
		if !ok {
			return fmt.Errorf("sync_logs.status field not found or wrong type")
		}
		changedLogs := addSelectValue(syncLogsStatus, "degraded")
		// "waiting_jobs": deploy is paused waiting for in-flight job_runs on
		// the same repository to finish before proceeding (P1.2).
		if addSelectValue(syncLogsStatus, "waiting_jobs") {
			changedLogs = true
		}
		if changedLogs {
			if err := app.Save(syncLogsCol); err != nil {
				return err
			}
		}

		log.Println("[MIGRATE] Added 'degraded' status to stacks and sync_logs, and 'waiting_jobs' to sync_logs")
		return nil
	}, func(app core.App) error {
		stacksCol, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		if f, ok := stacksCol.Fields.GetByName("status").(*core.SelectField); ok {
			removeSelectValue(f, "degraded")
		}
		if err := app.Save(stacksCol); err != nil {
			return err
		}

		syncLogsCol, err := app.FindCollectionByNameOrId("sync_logs")
		if err != nil {
			return err
		}
		if f, ok := syncLogsCol.Fields.GetByName("status").(*core.SelectField); ok {
			removeSelectValue(f, "degraded")
			removeSelectValue(f, "waiting_jobs")
		}
		return app.Save(syncLogsCol)
	})
}
