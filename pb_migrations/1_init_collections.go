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
			"stack_services", "stack_env_vars", "sync_logs",
			"stacks", "repository_keys", "repositories",
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
	return nil
}

// repositories: git connection only
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
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("@request.auth.id != ''")
	col.UpdateRule = strPtr("@request.auth.id != ''")
	col.DeleteRule = strPtr("@request.auth.id != ''")

	return app.Save(col)
}

// repository_keys: git auth (relation to repository)
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

// stacks: compose config, 1:1 with a repository
func createStacks(app core.App) error {
	col := core.NewBaseCollection("stacks")

	reposCol, err := app.FindCollectionByNameOrId("repositories")
	if err != nil {
		return err
	}

	col.Fields.Add(&core.TextField{Name: "name", Required: true})
	col.Fields.Add(&core.RelationField{
		Name:         "repository",
		CollectionId: reposCol.Id,
		Required:     true,
		MaxSelect:    1,
	})
	col.Fields.Add(&core.TextField{Name: "compose_path"})
	col.Fields.Add(&core.TextField{Name: "compose_file"})
	col.Fields.Add(&core.NumberField{Name: "poll_interval"})
	col.Fields.Add(&core.BoolField{Name: "auto_sync"})
	col.Fields.Add(&core.SelectField{
		Name:   "status",
		Values: []string{"active", "paused", "error", "pending"},
	})
	col.Fields.Add(&core.DateField{Name: "last_synced_at"})
	col.Fields.Add(&core.TextField{Name: "webhook_secret"})
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("@request.auth.id != ''")
	col.UpdateRule = strPtr("@request.auth.id != ''")
	col.DeleteRule = strPtr("@request.auth.id != ''")

	return app.Save(col)
}

// sync_logs: linked to a stack
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
		Values: []string{"cron", "webhook", "manual"},
	})
	col.Fields.Add(&core.SelectField{
		Name:   "status",
		Values: []string{"running", "success", "error", "done"},
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

// stack_services: container status per stack
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

// stack_env_vars: env vars injected during compose up
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
	col.Fields.Add(&core.TextField{Name: "value", Hidden: true})
	addAutoDateFields(col)

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = strPtr("@request.auth.id != ''")
	col.UpdateRule = strPtr("@request.auth.id != ''")
	col.DeleteRule = strPtr("@request.auth.id != ''")

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
