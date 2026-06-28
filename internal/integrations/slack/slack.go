package slack

import (
	"github.com/wireops/wireops/internal/integrations"
)

// SlackIntegration handles Slack notifications as an integration.
type SlackIntegration struct{}

func init() {
	integrations.Register(&SlackIntegration{})
}

// Slug returns the unique identifier for this integration.
func (s *SlackIntegration) Slug() string {
	return "slack"
}

// Name returns the human-readable name of the integration.
func (s *SlackIntegration) Name() string {
	return "Slack"
}

// Category returns the category of the integration.
func (s *SlackIntegration) Category() string {
	return "Notification"
}

// ResolveContainerActions returns no container actions as this is a notification integration.
func (s *SlackIntegration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	return nil
}
