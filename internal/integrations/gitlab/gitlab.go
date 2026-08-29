package gitlab

import (
	"github.com/wireops/wireops/internal/integrations"
)

// descriptor describes the native GitLab OAuth connection
// (internal/gitprovider/gitlab) as a Settings → Integrations card. Its
// enabled state is not a manual toggle — it's computed at request time from
// whether GITLAB_OAUTH_CLIENT_ID/GITLAB_OAUTH_CLIENT_SECRET are set (see
// internal/routes routeRegistrar.registerIntegrationRoutes), and it's always
// locked, same as the github descriptor. The actual connection
// (account/token) lives in a repository_keys row, not in this integration's
// config blob — this card is status + a connection test only;
// connecting/reconnecting happens via ConnectGitlabButton, used both here
// and in RepositoryCreateModal.vue. Locked:true plus zero Fields/Capabilities
// reflects that there's nothing here to configure.
var descriptor = integrations.Descriptor{
	Slug:     "gitlab",
	Name:     "GitLab",
	Category: integrations.CategorySourceControl,
	Locked:   true,
}

func init() {
	integrations.Register(descriptor, nil)
}
