package infisical

import (
	"github.com/wireops/wireops/internal/integrations"
)

// InfisicalIntegration exposes Infisical as a secret backend integration.
// Its connection config (site_url/client_id/client_secret) is stored in the
// integrations collection and consumed by
// internal/secrets.InfisicalSecretProvider — it has no container actions of
// its own.
type InfisicalIntegration struct{}

func init() {
	integrations.Register(&InfisicalIntegration{})
}

// Slug returns the unique identifier for this integration.
func (i *InfisicalIntegration) Slug() string {
	return "infisical"
}

// Name returns the human-readable name of the integration.
func (i *InfisicalIntegration) Name() string {
	return "Infisical"
}

// Category returns the category of the integration.
func (i *InfisicalIntegration) Category() string {
	return "Secret Backend"
}

// ResolveContainerActions returns no container actions — this is a secret
// backend, not a container-action integration.
func (i *InfisicalIntegration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	return nil
}
