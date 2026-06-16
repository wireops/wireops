package ntfy

import (
	"github.com/wireops/wireops/internal/integrations"
)

// NtfyIntegration handles ntfy notifications as an integration.
type NtfyIntegration struct{}

func init() {
	integrations.Register(&NtfyIntegration{})
}

// Slug returns the unique identifier for this integration.
func (n *NtfyIntegration) Slug() string {
	return "ntfy"
}

// Name returns the human-readable name of the integration.
func (n *NtfyIntegration) Name() string {
	return "Ntfy"
}

// Category returns the category of the integration.
func (n *NtfyIntegration) Category() string {
	return "Notification"
}

// ResolveContainerActions returns no container actions as this is a notification integration.
func (n *NtfyIntegration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	return nil
}
