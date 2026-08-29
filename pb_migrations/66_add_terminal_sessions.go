package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

const (
	terminalSessionsSessionIDIndexSQL = "CREATE UNIQUE INDEX IF NOT EXISTS idx_terminal_sessions_session_id ON terminal_sessions (session_id)"
	terminalSessionsStackIndexSQL     = "CREATE INDEX IF NOT EXISTS idx_terminal_sessions_stack_started ON terminal_sessions (stack, started_at)"
	terminalSessionsExpiresIndexSQL   = "CREATE INDEX IF NOT EXISTS idx_terminal_sessions_expires_at ON terminal_sessions (expires_at)"
)

// init registers the terminal_sessions collection (phase 2 of the audited
// web terminal, see docs/TERMINAL.md): one row per interactive exec session,
// pointing at a transcript file under DATA_DIR/terminal_sessions/ rather than
// storing the (potentially large, potentially secret-carrying) byte stream
// in SQLite. Locked API rules — same posture as audit_logs: reads only via
// the RBAC-gated custom routes in internal/routes/terminal.go.
func init() {
	m.Register(func(app core.App) error {
		stacksCol, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		usersCol, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		workersCol, err := app.FindCollectionByNameOrId("workers")
		if err != nil {
			return err
		}

		col := core.NewBaseCollection("terminal_sessions")
		col.Fields.Add(&core.TextField{Name: "session_id", Required: true})
		col.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacksCol.Id, Required: true, MaxSelect: 1})
		col.Fields.Add(&core.RelationField{Name: "user", CollectionId: usersCol.Id, Required: true, MaxSelect: 1})
		col.Fields.Add(&core.RelationField{Name: "worker", CollectionId: workersCol.Id, Required: true, MaxSelect: 1})
		col.Fields.Add(&core.TextField{Name: "container_id", Required: true})
		col.Fields.Add(&core.TextField{Name: "transcript_path", Required: true})
		col.Fields.Add(&core.DateField{Name: "started_at", Required: true})
		col.Fields.Add(&core.DateField{Name: "ended_at"})
		col.Fields.Add(&core.NumberField{Name: "exit_code", OnlyInt: true})
		col.Fields.Add(&core.SelectField{
			Name:      "status",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"open", "closed"},
		})
		col.Fields.Add(&core.DateField{Name: "expires_at", Required: true})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})

		col.ListRule = nil
		col.ViewRule = nil
		col.CreateRule = nil
		col.UpdateRule = nil
		col.DeleteRule = nil

		col.Indexes = append(col.Indexes,
			terminalSessionsSessionIDIndexSQL,
			terminalSessionsStackIndexSQL,
			terminalSessionsExpiresIndexSQL,
		)

		if err := app.Save(col); err != nil {
			return err
		}

		settings, err := app.FindCollectionByNameOrId("app_settings")
		if err != nil {
			return err
		}
		minDays := 1.0
		settings.Fields.Add(&core.NumberField{Name: "terminal_retention_days", OnlyInt: true, Min: &minDays})
		if err := app.Save(settings); err != nil {
			return err
		}

		records, err := app.FindAllRecords("app_settings")
		if err != nil {
			return err
		}
		for _, rec := range records {
			if rec.GetInt("terminal_retention_days") <= 0 {
				rec.Set("terminal_retention_days", 30)
			}
			if err := app.Save(rec); err != nil {
				return err
			}
		}

		app.Logger().Info("Created terminal_sessions collection and retention setting")
		return nil
	}, func(app core.App) error {
		settings, err := app.FindCollectionByNameOrId("app_settings")
		if err == nil {
			settings.Fields.RemoveByName("terminal_retention_days")
			if err := app.Save(settings); err != nil {
				return err
			}
		}

		col, err := app.FindCollectionByNameOrId("terminal_sessions")
		if err != nil {
			// Collection already gone — rollback has nothing left to undo,
			// not a failure.
			return nil
		}
		return app.Delete(col)
	})
}
