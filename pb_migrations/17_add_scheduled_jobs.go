package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		if err := createScheduledJobs(app); err != nil {
			return err
		}
		if err := createJobEnvVars(app); err != nil {
			return err
		}
		return createJobRuns(app)
	}, func(app core.App) error {
		for _, name := range []string{"job_runs", "job_env_vars", "scheduled_jobs"} {
			col, err := app.FindCollectionByNameOrId(name)
			if err == nil {
				_ = app.Delete(col)
			}
		}
		return nil
	})
}

// scheduled_jobs: thin reference — all config lives in job.yaml in the repository.
func createScheduledJobs(app core.App) error {
	reposCol, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		return err
	}

	col := core.NewBaseCollection("scheduled_jobs")

	col.Fields.Add(&core.RelationField{
		Name:         "repository",
		CollectionId: reposCol.Id,
		Required:     true,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.TextField{Name: "job_file", Required: true})
	col.Fields.Add(&core.BoolField{Name: "enabled"})
	col.Fields.Add(&core.SelectField{
		Name:   "status",
		Values: []string{"active", "paused", "stalled"},
	})
	col.Fields.Add(&core.DateField{Name: "last_run_at"})
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("@request.auth.id != ''")
	col.UpdateRule = strPtr("@request.auth.id != ''")
	col.DeleteRule = strPtr("@request.auth.id != ''")

	return app.Save(col)
}

// job_env_vars: secret key/value pairs injected at runtime. Not committed to the repo.
func createJobEnvVars(app core.App) error {
	jobsCol, err := app.FindCollectionByNameOrId("scheduled_jobs")
	if err != nil {
		return err
	}

	col := core.NewBaseCollection("job_env_vars")

	col.Fields.Add(&core.RelationField{
		Name:         "job",
		CollectionId: jobsCol.Id,
		Required:     true,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.TextField{Name: "key", Required: true})
	col.Fields.Add(&core.TextField{Name: "value"})
	col.Fields.Add(&core.BoolField{Name: "secret"})
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("@request.auth.id != ''")
	col.UpdateRule = strPtr("@request.auth.id != ''")
	col.DeleteRule = strPtr("@request.auth.id != ''")

	return app.Save(col)
}

// job_runs: execution history for scheduled jobs.
func createJobRuns(app core.App) error {
	jobsCol, err := app.FindCollectionByNameOrId("scheduled_jobs")
	if err != nil {
		return err
	}
	agentsCol, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		return err
	}

	col := core.NewBaseCollection("job_runs")

	col.Fields.Add(&core.RelationField{
		Name:         "job",
		CollectionId: jobsCol.Id,
		Required:     true,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.RelationField{
		Name:         "agent",
		CollectionId: agentsCol.Id,
		Required:     false,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.SelectField{
		Name:   "trigger",
		Values: []string{"cron", "manual"},
	})
	col.Fields.Add(&core.SelectField{
		Name:   "status",
		Values: []string{"pending", "running", "success", "error", "stalled"},
	})
	col.Fields.Add(&core.TextField{Name: "output"})
	col.Fields.Add(&core.NumberField{Name: "duration_ms"})
	col.Fields.Add(&core.DateField{Name: "expires_at"})
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("")
	col.UpdateRule = strPtr("")
	col.DeleteRule = strPtr("@request.auth.id != ''")

	return app.Save(col)
}
