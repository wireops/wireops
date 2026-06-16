package webhook

import (
	"github.com/wireops/wireops/internal/integrations"
)

// WebhookIntegration handles webhook notifications as an integration.
type WebhookIntegration struct{}

func init() {
	integrations.Register(&WebhookIntegration{})
}

// Slug returns the unique identifier for this integration.
func (w *WebhookIntegration) Slug() string {
	return "webhook"
}

// Name returns the human-readable name of the integration.
func (w *WebhookIntegration) Name() string {
	return "Webhook"
}

// Category returns the category of the integration.
func (w *WebhookIntegration) Category() string {
	return "Notification"
}

// ResolveContainerActions returns no container actions as this is a notification integration.
func (w *WebhookIntegration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	return nil
}
