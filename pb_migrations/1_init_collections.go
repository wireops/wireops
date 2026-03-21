package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		return createCollections(app)
	}, func(app core.App) error {
		for _, name := range []string{
			"integrations", "job_runs", "job_env_vars", "scheduled_jobs",
			"invites", "stack_pending_reconciles", "workers",
			"stack_revisions", "stack_sync_events", "stack_env_vars",
			"stack_services", "sync_logs", "stacks",
			"repository_keys", "repositories",
		} {
			col, err := app.FindCollectionByNameOrId(name)
			if err == nil {
				_ = app.Delete(col)
			}
		}
		return nil
	})
}

func createCollections(app core.App) error {
	if err := createRepositories(app); err != nil {
		return err
	}
	if err := createRepositoryCredentials(app); err != nil {
		return err
	}
	if err := createWorkers(app); err != nil {
		return err
	}
	if err := createStacks(app); err != nil {
		return err
	}
	if err := createSyncLogs(app); err != nil {
		return err
	}
	if err := createStackServices(app); err != nil {
		return err
	}
	if err := createStackEnvVars(app); err != nil {
		return err
	}
	if err := createStackSyncEvents(app); err != nil {
		return err
	}
	if err := createStackRevisions(app); err != nil {
		return err
	}
	if err := createStackPendingReconciles(app); err != nil {
		return err
	}
	if err := createInvites(app); err != nil {
		return err
	}
	if err := createScheduledJobs(app); err != nil {
		return err
	}
	if err := createJobEnvVars(app); err != nil {
		return err
	}
	if err := createJobRuns(app); err != nil {
		return err
	}
	return createIntegrations(app)
}

func createRepositories(app core.App) error {
	col := core.NewBaseCollection("repositories")

	col.Fields.Add(&core.TextField{Name: "name", Required: true})
	col.Fields.Add(&core.TextField{Name: "git_url", Required: true})
	col.Fields.Add(&core.TextField{Name: "branch"})
	col.Fields.Add(&core.SelectField{
		Name:   "status",
		Values: []string{"connected", "error"},
	})
	col.Fields.Add(&core.TextField{Name: "last_commit_sha"})
	col.Fields.Add(&core.DateField{Name: "last_fetched_at"})
	col.Fields.Add(&core.TextField{Name: "platform"})
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("@request.auth.id != ''")
	col.UpdateRule = strPtr("@request.auth.id != ''")
	col.DeleteRule = strPtr("@request.auth.id != ''")

	return app.Save(col)
}

func createRepositoryCredentials(app core.App) error {
	col := core.NewBaseCollection("repository_keys")

	reposCol, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		return err
	}

	col.Fields.Add(&core.RelationField{
		Name:         "repository",
		CollectionId: reposCol.Id,
		Required:     true,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.SelectField{
		Name:   "auth_type",
		Values: []string{"none", "ssh_key", "basic"},
	})
	col.Fields.Add(&core.TextField{Name: "ssh_private_key", Hidden: true})
	col.Fields.Add(&core.TextField{Name: "ssh_passphrase", Hidden: true})
	col.Fields.Add(&core.TextField{Name: "ssh_known_host"})
	col.Fields.Add(&core.TextField{Name: "git_username"})
	col.Fields.Add(&core.TextField{Name: "git_password", Hidden: true})
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("@request.auth.id != ''")
	col.UpdateRule = strPtr("@request.auth.id != ''")
	col.DeleteRule = strPtr("@request.auth.id != ''")

	return app.Save(col)
}

func createWorkers(app core.App) error {
	col := core.NewBaseCollection("workers")

	col.Fields.Add(&core.TextField{Name: "hostname", Required: true})
	col.Fields.Add(&core.TextField{Name: "fingerprint", Required: true})
	col.Fields.Add(&core.SelectField{
		Name:      "status",
		Values:    []string{"ACTIVE", "REVOKED"},
		MaxSelect: 1,
		Required:  true,
	})
	col.Fields.Add(&core.AutodateField{Name: "last_seen", OnCreate: true, OnUpdate: true})
	col.Fields.Add(&core.JSONField{Name: "health_history"})

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = nil // System only
	col.UpdateRule = nil // System only
	col.DeleteRule = nil // System only

	return app.Save(col)
}

func createStacks(app core.App) error {
	col := core.NewBaseCollection("stacks")

	reposCol, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		return err
	}
	workersCol, err := app.FindCollectionByNameOrId("workers")
	if err != nil {
		return err
	}

	col.Fields.Add(&core.TextField{Name: "name", Required: true})
	col.Fields.Add(&core.RelationField{
		Name:         "repository",
		CollectionId: reposCol.Id,
		Required:     false,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.RelationField{
		Name:         "worker",
		CollectionId: workersCol.Id,
		Required:     false,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.TextField{Name: "compose_path"})
	col.Fields.Add(&core.TextField{Name: "compose_file"})
	col.Fields.Add(&core.NumberField{Name: "poll_interval"})
	col.Fields.Add(&core.BoolField{Name: "auto_sync"})
	col.Fields.Add(&core.SelectField{
		Name:   "status",
		Values: []string{"active", "syncing", "paused", "error", "pending"},
	})
	col.Fields.Add(&core.DateField{Name: "last_synced_at"})
	col.Fields.Add(&core.TextField{Name: "webhook_secret"})
	col.Fields.Add(&core.NumberField{Name: "current_version"})
	col.Fields.Add(&core.TextField{Name: "desired_commit"})
	col.Fields.Add(&core.TextField{Name: "checksum"})
	col.Fields.Add(&core.SelectField{
		Name:   "source_type",
		Values: []string{"git", "local"},
	})
	col.Fields.Add(&core.TextField{Name: "import_path"})
	col.Fields.Add(&core.BoolField{Name: "import_recreate_volumes"})
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("@request.auth.id != ''")
	col.UpdateRule = strPtr("@request.auth.id != ''")
	col.DeleteRule = strPtr("@request.auth.id != ''")

	return app.Save(col)
}

func createSyncLogs(app core.App) error {
	col := core.NewBaseCollection("sync_logs")

	stacksCol, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		return err
	}

	col.Fields.Add(&core.RelationField{
		Name:         "stack",
		CollectionId: stacksCol.Id,
		Required:     true,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.SelectField{
		Name:   "trigger",
		Values: []string{"cron", "webhook", "manual", "redeploy", "rollback", "transfer", "queue"},
	})
	col.Fields.Add(&core.SelectField{
		Name:   "status",
		Values: []string{"running", "success", "error", "done", "queued"},
	})
	col.Fields.Add(&core.TextField{Name: "commit_sha"})
	col.Fields.Add(&core.TextField{Name: "commit_message"})
	col.Fields.Add(&core.TextField{Name: "output"})
	col.Fields.Add(&core.NumberField{Name: "duration_ms"})
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("")
	col.UpdateRule = strPtr("")
	col.DeleteRule = strPtr("@request.auth.id != ''")

	return app.Save(col)
}

func createStackServices(app core.App) error {
	col := core.NewBaseCollection("stack_services")

	stacksCol, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		return err
	}

	col.Fields.Add(&core.RelationField{
		Name:         "stack",
		CollectionId: stacksCol.Id,
		Required:     true,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.TextField{Name: "service_name"})
	col.Fields.Add(&core.TextField{Name: "container_name"})
	col.Fields.Add(&core.TextField{Name: "status"})
	col.Fields.Add(&core.TextField{Name: "container_id"})
	col.Fields.Add(&core.DateField{Name: "last_checked_at"})
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("")
	col.UpdateRule = strPtr("")
	col.DeleteRule = strPtr("")

	return app.Save(col)
}

func createStackEnvVars(app core.App) error {
	col := core.NewBaseCollection("stack_env_vars")

	stacksCol, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		return err
	}

	col.Fields.Add(&core.RelationField{
		Name:         "stack",
		CollectionId: stacksCol.Id,
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

func createStackSyncEvents(app core.App) error {
	col := core.NewBaseCollection("stack_sync_events")

	col.Fields.Add(&core.TextField{Name: "provider"})
	col.Fields.Add(&core.TextField{Name: "url"})
	col.Fields.Add(&core.TextField{Name: "secret", Hidden: true})
	col.Fields.Add(&core.SelectField{
		Name:      "events",
		MaxSelect: 4,
		Values:    []string{"sync.started", "sync.done", "sync.error", "sync.test"},
	})
	col.Fields.Add(&core.JSONField{Name: "headers"})
	col.Fields.Add(&core.BoolField{Name: "enabled"})
	col.Fields.Add(&core.TextField{Name: "ntfy_user"})
	col.Fields.Add(&core.TextField{Name: "ntfy_topic"})
	col.Fields.Add(&core.TextField{Name: "ntfy_template"})
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("@request.auth.id != ''")
	col.UpdateRule = strPtr("@request.auth.id != ''")
	col.DeleteRule = strPtr("@request.auth.id != ''")

	return app.Save(col)
}

func createStackRevisions(app core.App) error {
	col := core.NewBaseCollection("stack_revisions")

	stacksCol, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		return err
	}

	col.Fields.Add(&core.RelationField{
		Name:         "stack",
		CollectionId: stacksCol.Id,
		Required:     true,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.NumberField{Name: "version", Required: true})
	col.Fields.Add(&core.TextField{Name: "commit_sha", Required: true})
	col.Fields.Add(&core.TextField{Name: "checksum", Required: true})
	col.Fields.Add(&core.TextField{Name: "compose_path", Required: true})

	col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = nil // System only
	col.UpdateRule = nil // System only
	col.DeleteRule = nil // System only

	return app.Save(col)
}

func createStackPendingReconciles(app core.App) error {
	col := core.NewBaseCollection("stack_pending_reconciles")

	stacksCol, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		return err
	}

	col.Fields.Add(&core.RelationField{
		Name:         "stack",
		CollectionId: stacksCol.Id,
		Required:     true,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.SelectField{
		Name:   "trigger",
		Values: []string{"cron", "webhook", "manual", "redeploy", "rollback", "transfer"},
	})
	col.Fields.Add(&core.TextField{Name: "commit_sha"})

	col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

	col.ListRule = nil // System only
	col.ViewRule = nil
	col.CreateRule = nil
	col.UpdateRule = nil
	col.DeleteRule = nil

	return app.Save(col)
}

func createInvites(app core.App) error {
	col := core.NewBaseCollection("invites")

	col.Fields.Add(&core.TextField{Name: "email", Required: true})
	col.Fields.Add(&core.TextField{Name: "token", Required: true})
	col.Fields.Add(&core.DateField{Name: "expires_at", Required: true})
	col.Fields.Add(&core.BoolField{Name: "used"})
	col.Fields.Add(&core.TextField{Name: "created_by"})

	col.ListRule = nil // System only
	col.ViewRule = nil
	col.CreateRule = nil
	col.UpdateRule = nil
	col.DeleteRule = nil

	return app.Save(col)
}

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

func createJobRuns(app core.App) error {
	jobsCol, err := app.FindCollectionByNameOrId("scheduled_jobs")
	if err != nil {
		return err
	}
	workersCol, err := app.FindCollectionByNameOrId("workers")
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
		Name:         "worker",
		CollectionId: workersCol.Id,
		Required:     false,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.SelectField{
		Name:   "trigger",
		Values: []string{"cron", "manual"},
	})
	col.Fields.Add(&core.SelectField{
		Name:   "status",
		Values: []string{"pending", "running", "success", "error", "stalled", "forgotten"},
	})
	col.Fields.Add(&core.TextField{Name: "output"})
	col.Fields.Add(&core.NumberField{Name: "duration_ms"})
	col.Fields.Add(&core.DateField{Name: "expires_at"})
	col.Fields.Add(&core.TextField{Name: "container_name"})
	col.Fields.Add(&core.TextField{Name: "commit_sha"})
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("@request.auth.id != ''")
	col.UpdateRule = strPtr("@request.auth.id != ''")
	col.DeleteRule = strPtr("@request.auth.id != ''")

	return app.Save(col)
}

func createIntegrations(app core.App) error {
	col := core.NewBaseCollection("integrations")

	col.Fields.Add(&core.TextField{Name: "slug", Required: true})
	col.AddIndex("idx_integrations_slug_unique", true, "slug", "")
	col.Fields.Add(&core.BoolField{Name: "enabled"})
	col.Fields.Add(&core.JSONField{Name: "config"})

	col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

	col.ListRule = nil // Superusers only
	col.ViewRule = nil
	col.CreateRule = nil
	col.UpdateRule = nil
	col.DeleteRule = nil

	return app.Save(col)
}

func strPtr(s string) *string {
	return &s
}

func addAutoDateFields(col *core.Collection) {
	col.Fields.Add(&core.AutodateField{
		Name:     "created",
		OnCreate: true,
	})
	col.Fields.Add(&core.AutodateField{
		Name:     "updated",
		OnCreate: true,
		OnUpdate: true,
	})
}
