package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const workerCommandsIdIndexSQL = "CREATE UNIQUE INDEX IF NOT EXISTS idx_worker_commands_command_id ON worker_commands (command_id)"

func init() {
	m.Register(func(app core.App) error {
		workersCol, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}

		col := core.NewBaseCollection("worker_commands")
		col.Fields.Add(&core.RelationField{
			Name:         "worker",
			CollectionId: workersCol.Id,
			Required:     true,
			MaxSelect:    1,
		})
		col.Fields.Add(&core.TextField{
			Name:     "command_id",
			Required: true,
		})
		col.Fields.Add(&core.SelectField{
			Name:      "command_type",
			Required:  true,
			MaxSelect: 1,
			Values: []string{
				"deploy", "redeploy", "teardown", "probe", "inspect",
				"get_status", "get_resources", "stop_container", "restart_container",
				"discover_projects", "read_file", "run_job", "kill_job",
			},
		})
		col.Fields.Add(&core.SelectField{
			Name:      "status",
			Required:  true,
			MaxSelect: 1,
			Values: []string{
				"dispatched", "acked", "success", "error", "timed_out", "cancelled",
			},
		})
		col.Fields.Add(&core.JSONField{
			Name: "payload",
		})
		col.Fields.Add(&core.JSONField{
			Name: "result",
		})
		col.Fields.Add(&core.NumberField{
			Name: "duration_ms",
		})
		col.Fields.Add(&core.DateField{
			Name: "expires_at",
		})

		addAutoDateFields(col)

		// Set default rules to allow authenticated users to view logs
		col.ListRule = strPtr("@request.auth.id != ''")
		col.ViewRule = strPtr("@request.auth.id != ''")
		col.CreateRule = nil // System only
		col.UpdateRule = nil // System only
		col.DeleteRule = nil // System only

		col.Indexes = append(col.Indexes, workerCommandsIdIndexSQL)

		if err := app.Save(col); err != nil {
			return err
		}

		log.Println("[MIGRATE] Created worker_commands collection")
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("worker_commands")
		if err != nil {
			return err
		}
		return app.Delete(col)
	})
}
