package traefik

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/wireops/wireops/internal/integrations"
)

// TraefikIntegration extracts Traefik router rules to create links to the application
type TraefikIntegration struct{}

func init() {
	integrations.Register(&TraefikIntegration{})
}

func (t *TraefikIntegration) Slug() string {
	return "traefik"
}

func (t *TraefikIntegration) Name() string {
	return "Traefik"
}

func (t *TraefikIntegration) Category() string {
	return "Reverse Proxy"
}

func (t *TraefikIntegration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	scheme := "https"
	if s, ok := config["scheme"].(string); ok && s != "" {
		scheme = s
	}
	port := ""
	if p, ok := config["port"].(string); ok && p != "" && p != "80" && p != "443" {
		port = ":" + p
	}

	// Regex to match traefik.http.routers.<name>.rule=Host(`...`)
	ruleRegex := regexp.MustCompile(`Host\(\x60([^\x60]+)\x60\)`)

	var actions []integrations.ContainerAction

	// Find the first Host rule
	for key, value := range ctx.Labels {
		if strings.HasPrefix(key, "traefik.http.routers.") && strings.HasSuffix(key, ".rule") {
			matches := ruleRegex.FindStringSubmatch(value)
			if len(matches) > 1 {
				hostsStr := matches[1]
				// Host(`a.com`, `b.com`) -> take the first one or just return the exact match
				// Submatch 1 will have the content inside the backticks, e.g. "example.com"
				host := strings.Split(hostsStr, "`")[0] // simplistic split if there are multiple

				url := fmt.Sprintf("%s://%s%s", scheme, host, port)
				actions = append(actions, integrations.ContainerAction{
					IntegrationSlug: t.Slug(),
					Kind:            integrations.ActionKindReverseProxy,
					Label:           "Open",
					URL:             url,
					Icon:            "i-lucide-external-link",
				})
				// One action is usually enough per container
				break
			}
		}
	}

	return actions
}
