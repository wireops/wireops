package sops

import (
	"github.com/wireops/wireops/internal/integrations"
)

// SopsIntegration exposes SOPS+age as a secret backend integration. Unlike
// Vault/Infisical, it has no connection config to configure — each
// repository gets its own auto-generated age keypair
// (internal/hooks/pb_hooks.go), and internal/sync.Reconciler decrypts
// secrets.yaml automatically whenever one is found next to a stack's
// wireops.yaml. Its integrations row is seeded locked+enabled by migration
// 53 and can't be toggled off (see internal/routes routeRegistrar.registerIntegrationRoutes).
type SopsIntegration struct{}

func init() {
	integrations.Register(&SopsIntegration{})
}

// Slug returns the unique identifier for this integration.
func (s *SopsIntegration) Slug() string {
	return "sops"
}

// Name returns the human-readable name of the integration.
func (s *SopsIntegration) Name() string {
	return "SOPS"
}

// Category returns the category of the integration.
func (s *SopsIntegration) Category() string {
	return "Secret Backend"
}

// ResolveContainerActions returns no container actions — this is a secret
// backend, not a container-action integration.
func (s *SopsIntegration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	return nil
}
