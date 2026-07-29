package github

import (
	"github.com/wireops/wireops/internal/integrations"
)

// GithubIntegration surfaces the native GitHub OAuth connection
// (internal/gitprovider/github) as a Settings → Integrations card. Its
// enabled state is not a manual toggle — it's computed at request time from
// whether GITHUB_OAUTH_CLIENT_ID/GITHUB_OAUTH_CLIENT_SECRET are set (see
// internal/routes routeRegistrar.registerIntegrationRoutes), and it's
// always locked, same as SopsIntegration. Unlike SopsIntegration, the
// actual connection (account/token) lives in a repository_keys row, not in
// this integration's config blob — this card is status + a connection
// test only; connecting/reconnecting happens via ConnectGithubButton, used
// both here and in RepositoryCreateModal.vue.
type GithubIntegration struct{}

func init() {
	integrations.Register(&GithubIntegration{})
}

// Slug returns the unique identifier for this integration.
func (g *GithubIntegration) Slug() string {
	return "github"
}

// Name returns the human-readable name of the integration.
func (g *GithubIntegration) Name() string {
	return "GitHub"
}

// Category returns the category of the integration.
func (g *GithubIntegration) Category() string {
	return "Source Control"
}

// ResolveContainerActions returns no container actions — this is a source
// control connection, not a container-action integration.
func (g *GithubIntegration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	return nil
}
