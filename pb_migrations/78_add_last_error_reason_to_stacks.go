package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// last_error_reason says *why* a deploy failed, next to last_error_category's
// *where*. The two are orthogonal: a timeout is always category "deploy", and
// "deploy" covers plenty that is not a timeout.
//
// It exists because the only previous evidence that a deploy was killed by
// deploy_timeout_seconds was the literal `signal: killed` at the end of a
// compose progress log — which an OOM kill produces too, and which no caller
// should have to string-match. The worker now reports the timeout as a flag
// (protocol.CommandResult.TimedOut) and this is where the server keeps it.
//
// Values cover only what the server can actually tell apart at the moment a
// deploy result is persisted:
//
//	timeout  — a worker or server budget expired (see withDeployTimeout)
//	dispatch — the command never completed through the worker: it went
//	           offline, refused the command, or never answered
//	compose  — `docker compose` itself returned non-zero
//
// Empty means "not known", which is what every pre-existing row carries, what
// every pre-dispatch (category "sync") failure writes, and what an older
// worker's results still produce.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		if col.Fields.GetByName("last_error_reason") == nil {
			col.Fields.Add(&core.SelectField{
				Name:      "last_error_reason",
				MaxSelect: 1,
				Values:    []string{"timeout", "dispatch", "compose"},
			})
			if err := app.Save(col); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		col.Fields.RemoveByName("last_error_reason")
		return app.Save(col)
	})
}
