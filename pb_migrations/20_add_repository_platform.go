package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("repositories")
		if err != nil {
			return err
		}

		// Step 1: add the field as NOT required so existing records don't fail validation.
		col.Fields.Add(&core.SelectField{
			Name:      "platform",
			Required:  false,
			MaxSelect: 1,
			Values:    []string{"github", "gitlab", "gitea", "forgejo", "bitbucket"},
		})
		if err := app.Save(col); err != nil {
			return err
		}

		// Step 2: backfill existing records that have no platform set.
		records, err := app.FindAllRecords("repositories")
		if err != nil {
			return err
		}
		for _, rec := range records {
			if rec.GetString("platform") == "" {
				rec.Set("platform", "github")
				if err := app.Save(rec); err != nil {
					return err
				}
			}
		}

		// Step 3: now that all records have a value, enforce Required: true.
		col, err = app.FindCollectionByNameOrId("repositories")
		if err != nil {
			return err
		}
		f := col.Fields.GetByName("platform")
		if sf, ok := f.(*core.SelectField); ok {
			sf.Required = true
		}
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("repositories")
		if err != nil {
			return err
		}

		if f := col.Fields.GetByName("platform"); f != nil {
			col.Fields.RemoveById(f.GetId())
		}

		return app.Save(col)
	})
}
