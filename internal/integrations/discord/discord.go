package discord

import (
	"github.com/wireops/wireops/internal/integrations"
)

// DiscordIntegration handles Discord notifications as an integration.
type DiscordIntegration struct{}

func init() {
	integrations.Register(&DiscordIntegration{})
}

// Slug returns the unique identifier for this integration.
func (d *DiscordIntegration) Slug() string {
	return "discord"
}

// Name returns the human-readable name of the integration.
func (d *DiscordIntegration) Name() string {
	return "Discord"
}

// Category returns the category of the integration.
func (d *DiscordIntegration) Category() string {
	return "Notification"
}

// ResolveContainerActions returns no container actions as this is a notification integration.
func (d *DiscordIntegration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	return nil
}
