package pb_migrations

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/wireops/wireops/internal/oidc"
)

const (
	rbacReadRule     = "@request.auth.id != ''"
	rbacOperatorRule = "@request.auth.role = 'operator' || @request.auth.role = 'admin'"
	rbacAdminRule    = "@request.auth.role = 'admin'"
)

func init() {
	m.Register(func(app core.App) error {
		if err := createRBACUsers(app); err != nil {
			return err
		}
		if err := createServiceAccounts(app); err != nil {
			return err
		}
		if err := addRoleToSSOUsers(app); err != nil {
			return err
		}
		if err := addRoleToInvites(app); err != nil {
			return err
		}
		if err := createSSOGroupRoles(app); err != nil {
			return err
		}
		if err := addSSOClaimSetting(app); err != nil {
			return err
		}
		if err := expandAuditIdentityEnums(app); err != nil {
			return err
		}
		if err := applyRBACRules(app); err != nil {
			return err
		}
		if err := backfillUsersFromSuperusers(app); err != nil {
			return err
		}

		log.Println("[MIGRATE] Added RBAC users, service accounts and SSO group role mappings")
		return nil
	}, func(app core.App) error {
		for _, name := range []string{"service_accounts", "sso_group_roles", "users"} {
			if col, err := app.FindCollectionByNameOrId(name); err == nil {
				if err := app.Delete(col); err != nil {
					return err
				}
			}
		}

		if col, err := app.FindCollectionByNameOrId("sso_users"); err == nil {
			col.Fields.RemoveByName("role")
			oidc.HydrateClientSecretForValidation(col)
			if err := app.Save(col); err != nil {
				return err
			}
		}
		if col, err := app.FindCollectionByNameOrId("invites"); err == nil {
			col.Fields.RemoveByName("role")
			if err := app.Save(col); err != nil {
				return err
			}
		}
		if col, err := app.FindCollectionByNameOrId("app_settings"); err == nil {
			col.Fields.RemoveByName("sso_groups_claim")
			if err := app.Save(col); err != nil {
				return err
			}
		}
		return nil
	})
}

func createRBACUsers(app core.App) error {
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		col = core.NewAuthCollection("users")
	}

	if col.Fields.GetByName("name") == nil {
		col.Fields.Add(&core.TextField{Name: "name"})
	}
	if col.Fields.GetByName("role") == nil {
		col.Fields.Add(roleSelectField())
	}
	if col.Fields.GetByName("disabled") == nil {
		col.Fields.Add(&core.BoolField{Name: "disabled"})
	}
	if col.Fields.GetByName("protected") == nil {
		col.Fields.Add(&core.BoolField{Name: "protected"})
	}

	col.AuthRule = strPtr("disabled = false || disabled = null")
	col.ListRule = strPtr(rbacAdminRule)
	col.ViewRule = strPtr("id = @request.auth.id || @request.auth.role = 'admin'")
	col.CreateRule = strPtr(rbacAdminRule)
	col.UpdateRule = strPtr(rbacAdminRule)
	col.DeleteRule = nil

	return app.Save(col)
}

func serviceAccountRoleSelectField() *core.SelectField {
	return &core.SelectField{
		Name:      "role",
		Required:  false,
		MaxSelect: 1,
		Values:    []string{"viewer", "operator"},
	}
}

func createServiceAccounts(app core.App) error {
	if _, err := app.FindCollectionByNameOrId("service_accounts"); err == nil {
		return nil
	}

	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}

	col := core.NewBaseCollection("service_accounts")
	col.Fields.Add(&core.TextField{Name: "name", Required: true})
	col.Fields.Add(&core.TextField{Name: "description", Required: true})
	col.Fields.Add(serviceAccountRoleSelectField())
	col.Fields.Add(&core.BoolField{Name: "enabled"})
	col.Fields.Add(&core.RelationField{Name: "created_by", CollectionId: users.Id, MaxSelect: 1})
	
	// Embedded API Key fields
	col.Fields.Add(&core.TextField{Name: "key_hash", Hidden: true})
	col.Fields.Add(&core.TextField{Name: "key_prefix"})
	col.Fields.Add(&core.DateField{Name: "key_expires_at"})
	col.Fields.Add(&core.DateField{Name: "key_last_used_at"})
	col.Fields.Add(&core.BoolField{Name: "key_revoked"})

	addAutoDateFields(col)

	col.AddIndex("idx_service_accounts_key_hash_unique", true, "key_hash", "key_hash != ''")
	col.AddIndex("idx_service_accounts_name_unique", true, "name", "")

	col.ListRule = strPtr(rbacAdminRule)
	col.ViewRule = strPtr(rbacAdminRule)
	col.CreateRule = strPtr(rbacAdminRule)
	col.UpdateRule = strPtr(rbacAdminRule)
	col.DeleteRule = nil

	return app.Save(col)
}

func addRoleToSSOUsers(app core.App) error {
	col, err := app.FindCollectionByNameOrId("sso_users")
	if err != nil {
		return err
	}
	if col.Fields.GetByName("role") == nil {
		col.Fields.Add(roleSelectField())
	}
	oidc.HydrateClientSecretForValidation(col)
	return app.Save(col)
}

func addRoleToInvites(app core.App) error {
	col, err := app.FindCollectionByNameOrId("invites")
	if err != nil {
		return err
	}
	if col.Fields.GetByName("role") == nil {
		col.Fields.Add(roleSelectField())
	}
	return app.Save(col)
}

func createSSOGroupRoles(app core.App) error {
	if _, err := app.FindCollectionByNameOrId("sso_group_roles"); err == nil {
		return nil
	}

	col := core.NewBaseCollection("sso_group_roles")
	col.Fields.Add(&core.TextField{Name: "group", Required: true})
	col.Fields.Add(roleSelectField())
	addAutoDateFields(col)
	col.AddIndex("idx_sso_group_roles_group_unique", true, "group", "")

	col.ListRule = strPtr(rbacAdminRule)
	col.ViewRule = strPtr(rbacAdminRule)
	col.CreateRule = strPtr(rbacAdminRule)
	col.UpdateRule = strPtr(rbacAdminRule)
	col.DeleteRule = strPtr(rbacAdminRule)

	return app.Save(col)
}

func addSSOClaimSetting(app core.App) error {
	col, err := app.FindCollectionByNameOrId("app_settings")
	if err != nil {
		return err
	}
	if col.Fields.GetByName("sso_groups_claim") == nil {
		col.Fields.Add(&core.TextField{Name: "sso_groups_claim"})
	}
	if err := app.Save(col); err != nil {
		return err
	}

	records, err := app.FindAllRecords("app_settings")
	if err != nil {
		return err
	}
	for _, rec := range records {
		if rec.GetString("sso_groups_claim") == "" {
			rec.Set("sso_groups_claim", "groups")
			if err := app.Save(rec); err != nil {
				return err
			}
		}
	}
	return nil
}

func expandAuditIdentityEnums(app core.App) error {
	col, err := app.FindCollectionByNameOrId("audit_logs")
	if err != nil {
		return err
	}
	if field, ok := col.Fields.GetByName("actor_type").(*core.SelectField); ok {
		field.Values = []string{"anonymous", "user", "agent", "system", "worker"}
	}
	if field, ok := col.Fields.GetByName("origin").(*core.SelectField); ok {
		field.Values = []string{"api", "api_key", "setup", "system", "ui", "webhook", "worker"}
	}
	return app.Save(col)
}

func applyRBACRules(app core.App) error {
	rules := map[string]struct {
		list   *string
		view   *string
		create *string
		update *string
		delete *string
	}{
		"repositories":      {strPtr(rbacReadRule), strPtr(rbacReadRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule)},
		"repository_keys":   {strPtr(rbacOperatorRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule)},
		"workers":           {strPtr(rbacOperatorRule), strPtr(rbacOperatorRule), nil, nil, nil},
		"stacks":            {strPtr(rbacReadRule), strPtr(rbacReadRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule)},
		"sync_logs":         {strPtr(rbacReadRule), strPtr(rbacReadRule), nil, nil, strPtr(rbacOperatorRule)},
		"stack_services":    {strPtr(rbacReadRule), strPtr(rbacReadRule), nil, nil, nil},
		"stack_env_vars":    {strPtr(rbacReadRule), strPtr(rbacReadRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule)},
		"stack_sync_events": {strPtr(rbacAdminRule), strPtr(rbacAdminRule), strPtr(rbacAdminRule), strPtr(rbacAdminRule), strPtr(rbacAdminRule)},
		"stack_revisions":   {strPtr(rbacReadRule), strPtr(rbacReadRule), nil, nil, nil},
		"scheduled_jobs":    {strPtr(rbacReadRule), strPtr(rbacReadRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule)},
		"job_env_vars":      {strPtr(rbacReadRule), strPtr(rbacReadRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule), strPtr(rbacOperatorRule)},
		"job_runs":          {strPtr(rbacReadRule), strPtr(rbacReadRule), nil, nil, strPtr(rbacOperatorRule)},
		"audit_logs":        {strPtr(rbacAdminRule), strPtr(rbacAdminRule), nil, nil, nil},
		"integrations":      {strPtr(rbacAdminRule), strPtr(rbacAdminRule), nil, nil, nil},
		"app_settings":      {strPtr(rbacAdminRule), strPtr(rbacAdminRule), strPtr(rbacAdminRule), strPtr(rbacAdminRule), nil},
	}

	for name, rule := range rules {
		col, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			return err
		}
		col.ListRule = rule.list
		col.ViewRule = rule.view
		col.CreateRule = rule.create
		col.UpdateRule = rule.update
		col.DeleteRule = rule.delete
		if err := app.Save(col); err != nil {
			return fmt.Errorf("save %s rules: %w", name, err)
		}
	}
	return nil
}

func backfillUsersFromSuperusers(app core.App) error {
	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	superusers, err := app.FindAllRecords(core.CollectionNameSuperusers)
	if err != nil {
		return err
	}
	for _, superuser := range superusers {
		email := superuser.Email()
		if email == "" || email == core.DefaultInstallerEmail {
			continue
		}
		existing, _ := app.FindAllRecords("users", dbx.HashExp{"email": email})
		if len(existing) > 0 {
			continue
		}
		password, err := randomMigrationPassword()
		if err != nil {
			return err
		}
		user := core.NewRecord(usersCol)
		user.Set("email", email)
		user.Set("password", password)
		user.Set("verified", true)
		user.Set("role", "admin")
		if err := app.Save(user); err != nil {
			return err
		}
	}
	return nil
}

func roleSelectField() *core.SelectField {
	return &core.SelectField{
		Name:      "role",
		Required:  false,
		MaxSelect: 1,
		Values:    []string{"viewer", "operator", "admin"},
	}
}

func randomMigrationPassword() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
