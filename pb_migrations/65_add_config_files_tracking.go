package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		if err := createStackConfigFiles(app); err != nil {
			return err
		}
		if err := createJobConfigFiles(app); err != nil {
			return err
		}
		app.Logger().Info("Added stack_config_files and job_config_files collections")
		return nil
	}, func(app core.App) error {
		for _, name := range []string{"stack_config_files", "job_config_files"} {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue
			}
			if err := app.Delete(col); err != nil {
				return err
			}
		}
		return nil
	})
}

// createStackConfigFiles tracks the resolved `configs:` entries most
// recently rendered for a stack (name, git source path, in-container target,
// content checksum) — UI/drift visibility only, mirrors stack_revisions.
// Rows are delete-then-recreated by the renderer on every render
// (internal/configfiles.UpsertStackConfigFiles), so writes are system-only.
func createStackConfigFiles(app core.App) error {
	col := core.NewBaseCollection("stack_config_files")

	stacksCol, err := app.FindCollectionByNameOrId("stacks")
	if err != nil {
		return err
	}

	col.Fields.Add(&core.RelationField{
		Name:          "stack",
		CollectionId:  stacksCol.Id,
		Required:      true,
		MaxSelect:     1,
		CascadeDelete: true,
	})
	col.Fields.Add(&core.TextField{Name: "name", Required: true})
	col.Fields.Add(&core.TextField{Name: "source_path", Required: true})
	col.Fields.Add(&core.TextField{Name: "target", Required: true})
	col.Fields.Add(&core.TextField{Name: "checksum", Required: true})

	col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

	col.AddIndex("idx_stack_config_files_unique", true, "stack, name", "")

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = nil // System only
	col.UpdateRule = nil // System only
	col.DeleteRule = nil // System only

	return app.Save(col)
}

// createJobConfigFiles is job_config_files, the same tracking shape as
// createStackConfigFiles but for scheduled jobs (job.yaml `configs:`).
func createJobConfigFiles(app core.App) error {
	col := core.NewBaseCollection("job_config_files")

	jobsCol, err := app.FindCollectionByNameOrId("scheduled_jobs")
	if err != nil {
		return err
	}

	col.Fields.Add(&core.RelationField{
		Name:          "job",
		CollectionId:  jobsCol.Id,
		Required:      true,
		MaxSelect:     1,
		CascadeDelete: true,
	})
	col.Fields.Add(&core.TextField{Name: "name", Required: true})
	col.Fields.Add(&core.TextField{Name: "source_path", Required: true})
	col.Fields.Add(&core.TextField{Name: "target", Required: true})
	col.Fields.Add(&core.TextField{Name: "checksum", Required: true})

	col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

	col.AddIndex("idx_job_config_files_unique", true, "job, name", "")

	col.ListRule = strPtr("@request.auth.id != ''")
	col.ViewRule = strPtr("@request.auth.id != ''")
	col.CreateRule = nil // System only
	col.UpdateRule = nil // System only
	col.DeleteRule = nil // System only

	return app.Save(col)
}
