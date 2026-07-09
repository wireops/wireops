package jobscheduler

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// ActiveJobRunsForRepository returns job_runs currently in the "running"
// state whose job belongs to repoID, joined via scheduled_jobs.repository.
// Used by the reconciler to decide whether an automatic deploy should wait
// for in-flight jobs on the same repository before proceeding (P1.2).
func ActiveJobRunsForRepository(app core.App, repoID string) ([]*core.Record, error) {
	if repoID == "" {
		return nil, nil
	}
	return app.FindRecordsByFilter(
		"job_runs",
		"job.repository = {:repoID} && status = 'running'",
		"+created",
		0,
		0,
		dbx.Params{"repoID": repoID},
	)
}
