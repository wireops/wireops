package github

import (
	"github.com/wireops/wireops/internal/integrations"
)

// descriptor describes the native GitHub OAuth connection
// (internal/gitprovider/github) as a Settings → Integrations card. Its
// enabled state is not a manual toggle — it's computed at request time from
// whether GITHUB_OAUTH_CLIENT_ID/GITHUB_OAUTH_CLIENT_SECRET are set (see
// internal/routes routeRegistrar.registerIntegrationRoutes), and it's
// always locked, same as the sops descriptor. Unlike sops, the actual
// connection (account/token) lives in a repository_keys row, not in this
// integration's config blob — this card is status + a connection test
// only; connecting/reconnecting happens via ConnectGithubButton, used both
// here and in RepositoryCreateModal.vue. Locked:true plus zero
// Fields/Capabilities reflects that there's nothing here to configure.
var descriptor = integrations.Descriptor{
	Slug:     "github",
	Name:     "GitHub",
	Category: integrations.CategorySourceControl,
	Locked:   true,
}

func init() {
	integrations.Register(descriptor, nil)
}
