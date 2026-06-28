package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		if err := createGlobalEnvVars(app); err != nil {
			return err
		}
		if err := createStackGlobalEnvVars(app); err != nil {
			return err
		}
		if err := createJobGlobalEnvVars(app); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added global environment variables")
		return nil
	}, func(app core.App) error {
		for _, name := range []string{"job_global_env_vars", "stack_global_env_vars", "global_env_vars"} {
			col, err := app.FindCollectionByNameOrId(name)
			if err == nil {
				if err := app.Delete(col); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func createGlobalEnvVars(app core.App) error {
	if _, err := app.FindCollectionByNameOrId("global_env_vars"); err == nil {
		return nil
	}

	col := core.NewBaseCollection("global_env_vars")
	col.Fields.Add(&core.TextField{Name: "key", Required: true})
	col.Fields.Add(&core.TextField{Name: "value"})
	col.Fields.Add(&core.BoolField{Name: "secret"})
	col.Fields.Add(&core.TextField{Name: "secret_provider"})
	col.Fields.Add(&core.TextField{Name: "description"})
	addAutoDateFields(col)

	col.AddIndex("idx_global_env_vars_key_unique", true, "key", "")

	col.ListRule = strPtr(rbacReadRule)
	col.ViewRule = strPtr(rbacReadRule)
	col.CreateRule = strPtr(rbacOperatorRule)
	col.UpdateRule = strPtr(rbacOperatorRule)
	col.DeleteRule = strPtr(rbacOperatorRule)

	return app.Save(col)
}

func createStackGlobalEnvVars(app core.App) error {
	if _, err := app.FindCollectionByNameOrId("stack_global_env_vars"); err == nil {
		return nil
	}

	stacksCol, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		return err
	}
	globalCol, err := app.FindCollectionByNameOrId("global_env_vars")
	if err != nil {
		return err
	}

	col := core.NewBaseCollection("stack_global_env_vars")
	col.Fields.Add(&core.RelationField{Name: "stack", CollectionId: stacksCol.Id, Required: true, MaxSelect: 1})
	col.Fields.Add(&core.RelationField{Name: "global_env_var", CollectionId: globalCol.Id, Required: true, MaxSelect: 1})
	addAutoDateFields(col)

	col.AddIndex("idx_stack_global_env_vars_unique", true, "stack, global_env_var", "")

	col.ListRule = strPtr(rbacReadRule)
	col.ViewRule = strPtr(rbacReadRule)
	col.CreateRule = strPtr(rbacOperatorRule)
	col.UpdateRule = strPtr(rbacOperatorRule)
	col.DeleteRule = strPtr(rbacOperatorRule)

	return app.Save(col)
}

func createJobGlobalEnvVars(app core.App) error {
	if _, err := app.FindCollectionByNameOrId("job_global_env_vars"); err == nil {
		return nil
	}

	jobsCol, err := app.FindCollectionByNameOrId("scheduled_jobs")
	if err != nil {
		return err
	}
	globalCol, err := app.FindCollectionByNameOrId("global_env_vars")
	if err != nil {
		return err
	}

	col := core.NewBaseCollection("job_global_env_vars")
	col.Fields.Add(&core.RelationField{Name: "job", CollectionId: jobsCol.Id, Required: true, MaxSelect: 1})
	col.Fields.Add(&core.RelationField{Name: "global_env_var", CollectionId: globalCol.Id, Required: true, MaxSelect: 1})
	addAutoDateFields(col)

	col.AddIndex("idx_job_global_env_vars_unique", true, "job, global_env_var", "")

	col.ListRule = strPtr(rbacReadRule)
	col.ViewRule = strPtr(rbacReadRule)
	col.CreateRule = strPtr(rbacOperatorRule)
	col.UpdateRule = strPtr(rbacOperatorRule)
	col.DeleteRule = strPtr(rbacOperatorRule)

	return app.Save(col)
}
