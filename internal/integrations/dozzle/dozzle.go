package dozzle

import (
	"fmt"
	"strings"

	"github.com/wireops/wireops/internal/integrations"
)

// DozzleIntegration replaces the standard container logs with a link to the self-hosted Dozzle instance
type DozzleIntegration struct{}

func init() {
	integrations.Register(&DozzleIntegration{})
}

func (d *DozzleIntegration) Slug() string {
	return "dozzle"
}

func (d *DozzleIntegration) Name() string {
	return "Dozzle"
}

func (d *DozzleIntegration) Category() string {
	return "Logging"
}

func (d *DozzleIntegration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	baseURL := ""
	if url, ok := config["url"].(string); ok && url != "" {
		baseURL = strings.TrimRight(url, "/")
	}

	if baseURL == "" {
		return nil // Dozzle requires a URL
	}

	return []integrations.ContainerAction{
		{
			IntegrationSlug: d.Slug(),
			Kind:            integrations.ActionKindLog,
			Label:           "Dozzle Logs",
			URL:             fmt.Sprintf("%s/container/%s", baseURL, ctx.ContainerID),
			Icon:            "i-lucide-activity",
		},
	}
}
