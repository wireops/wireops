package vault

import (
	"github.com/wireops/wireops/internal/integrations"
)

// VaultIntegration exposes HashiCorp Vault as a secret backend integration.
// Its connection config (address/token) is stored in the integrations
// collection and consumed by internal/secrets.VaultSecretProvider — it has
// no container actions of its own.
type VaultIntegration struct{}

func init() {
	integrations.Register(&VaultIntegration{})
}

// Slug returns the unique identifier for this integration.
func (v *VaultIntegration) Slug() string {
	return "vault"
}

// Name returns the human-readable name of the integration.
func (v *VaultIntegration) Name() string {
	return "HashiCorp Vault"
}

// Category returns the category of the integration.
func (v *VaultIntegration) Category() string {
	return "Secret Backend"
}

// ResolveContainerActions returns no container actions — this is a secret
// backend, not a container-action integration.
func (v *VaultIntegration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	return nil
}
