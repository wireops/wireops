package pb_migrations

import (
	"log"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// init registers the registry_credentials collection: reusable container
// registry credentials (username/password, token, or GCP service account
// JSON key) that the server decrypts and hands to a worker per-deploy so it
// can authenticate `docker compose pull` / `docker run` against a private
// registry. See internal/registry/credential_store.go for the encrypt/decrypt
// path, mirroring repository_keys.
func init() {
	m.Register(func(app core.App) error {
		col := core.NewBaseCollection("registry_credentials")
		col.Fields.Add(&core.TextField{Name: "name", Required: true})
		col.Fields.Add(&core.TextField{Name: "registry_url", Required: true})
		col.Fields.Add(&core.SelectField{
			Name:      "auth_type",
			Required:  true,
			MaxSelect: 1,
			Values:    []string{"basic", "token", "gcp_service_account"},
		})
		col.Fields.Add(&core.TextField{Name: "username"})
		col.Fields.Add(&core.TextField{Name: "password", Hidden: true})
		col.Fields.Add(&core.BoolField{Name: "insecure"})
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

		col.ListRule = strPtr(rbacReadRule)
		col.ViewRule = strPtr(rbacReadRule)
		col.CreateRule = strPtr(rbacOperatorRule)
		col.UpdateRule = strPtr(rbacOperatorRule)
		col.DeleteRule = strPtr(rbacOperatorRule)

		if err := app.Save(col); err != nil {
			return err
		}

		stacksCol, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		stacksCol.Fields.Add(&core.RelationField{
			Name:         "registry_credential",
			CollectionId: col.Id,
			MaxSelect:    1,
		})
		if err := app.Save(stacksCol); err != nil {
			return err
		}

		log.Println("[MIGRATE] Created registry_credentials collection and stacks.registry_credential relation")
		return nil
	}, func(app core.App) error {
		stacksCol, err := app.FindCollectionByNameOrId("stacks")
		if err != nil {
			return err
		}
		stacksCol.Fields.RemoveByName("registry_credential")
		if err := app.Save(stacksCol); err != nil {
			return err
		}

		col, err := app.FindCollectionByNameOrId("registry_credentials")
		if err != nil {
			return err
		}
		return app.Delete(col)
	})
}
