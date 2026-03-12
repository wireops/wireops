package traefik

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/wireops/wireops/internal/integrations"
)

const u60 = "\x60"

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
	// Regex to match backticked identities like `example.com` inside Host(...)
	ruleRegex := regexp.MustCompile(u60 + "([^" + u60 + "]+)" + u60)

	var actions []integrations.ContainerAction

	// Find all Host(...) rules
	for key, value := range ctx.Labels {
		if strings.HasPrefix(key, "traefik.http.routers.") && strings.HasSuffix(key, ".rule") {
			// FindAllStringSubmatch to capture all backticked tokens in Host(`a.com`, `b.com`)
			allMatches := ruleRegex.FindAllStringSubmatch(value, -1)
			if len(allMatches) > 0 {
				// Take the first captured group from the first match
				host := strings.TrimSpace(allMatches[0][1])
				if host == "" {
					continue
				}

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
