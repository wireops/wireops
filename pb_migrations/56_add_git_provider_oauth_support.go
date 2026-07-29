package pb_migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Migration 56: native git provider OAuth support (GitHub first, agnostic
// for Gitea/Forgejo/GitLab later).
//
// repository_keys gains a new auth_type value "oauth_token" plus the fields
// needed to store a provider-issued token: oauth_provider (which
// internal/gitprovider registry entry issued it), oauth_token/
// oauth_refresh_token (hidden, AES-GCM encrypted like ssh_private_key/
// git_password), oauth_token_expires_at (empty = does not expire, as with
// GitHub's classic OAuth Apps), and oauth_account_login (cosmetic "connected
// as @user" display). repository_keys is already a reusable, many-to-one
// credential store (migration 38) — the right home for "one connected
// account, many repos picked from it," no new collection needed.
func init() {
	m.Register(func(app core.App) error {
		return addGitProviderOAuthFields(app)
	}, func(app core.App) error {
		return removeGitProviderOAuthFields(app)
	})
}

func addGitProviderOAuthFields(app core.App) error {
	col, err := app.FindCollectionByNameOrId("repository_keys")
	if err != nil {
		return err
	}

	if authField, ok := col.Fields.GetByName("auth_type").(*core.SelectField); ok {
		authField.Values = []string{"ssh_key", "basic", "oauth_token"}
	}

	if col.Fields.GetByName("oauth_provider") == nil {
		col.Fields.Add(&core.TextField{Name: "oauth_provider"})
	}
	if col.Fields.GetByName("oauth_token") == nil {
		col.Fields.Add(&core.TextField{Name: "oauth_token", Hidden: true})
	}
	if col.Fields.GetByName("oauth_refresh_token") == nil {
		col.Fields.Add(&core.TextField{Name: "oauth_refresh_token", Hidden: true})
	}
	if col.Fields.GetByName("oauth_token_expires_at") == nil {
		col.Fields.Add(&core.DateField{Name: "oauth_token_expires_at"})
	}
	if col.Fields.GetByName("oauth_account_login") == nil {
		col.Fields.Add(&core.TextField{Name: "oauth_account_login"})
	}

	return app.Save(col)
}

func removeGitProviderOAuthFields(app core.App) error {
	col, err := app.FindCollectionByNameOrId("repository_keys")
	if err != nil {
		return err
	}

	if authField, ok := col.Fields.GetByName("auth_type").(*core.SelectField); ok {
		authField.Values = []string{"ssh_key", "basic"}
	}

	for _, name := range []string{"oauth_provider", "oauth_token", "oauth_refresh_token", "oauth_token_expires_at", "oauth_account_login"} {
		if field := col.Fields.GetByName(name); field != nil {
			col.Fields.RemoveById(field.GetId())
		}
	}

	return app.Save(col)
}
