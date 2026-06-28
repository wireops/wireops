package caddy

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/wireops/wireops/internal/integrations"
)

var caddySiteLabel = regexp.MustCompile(`^caddy(?:_\d+)?$`)

// CaddyIntegration extracts caddy-docker-proxy site labels to create application links.
type CaddyIntegration struct{}

func init() {
	integrations.Register(&CaddyIntegration{})
}

func (c *CaddyIntegration) Slug() string {
	return "caddy"
}

func (c *CaddyIntegration) Name() string {
	return "Caddy"
}

func (c *CaddyIntegration) Category() string {
	return "Reverse Proxy"
}

func (c *CaddyIntegration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	scheme := stringConfig(config, "scheme", "https")
	port := normalizedPort(stringConfig(config, "port", ""))
	allowWildcards := boolConfig(config, "allow_wildcards", false)
	allowLocalHosts := boolConfig(config, "allow_local_hosts", true)

	hosts := caddyHosts(ctx.Labels, allowWildcards, allowLocalHosts)
	if len(hosts) == 0 {
		return nil
	}

	actions := make([]integrations.ContainerAction, 0, len(hosts))
	for _, host := range hosts {
		actions = append(actions, integrations.ContainerAction{
			IntegrationSlug: c.Slug(),
			Kind:            integrations.ActionKindReverseProxy,
			Label:           "Open",
			URL:             buildProxyURL(scheme, host, port),
			Icon:            "i-lucide-external-link",
		})
	}
	return actions
}

func caddyHosts(labels map[string]string, allowWildcards, allowLocalHosts bool) []string {
	if len(labels) == 0 {
		return nil
	}

	keys := make([]string, 0, len(labels))
	for key := range labels {
		if caddySiteLabel.MatchString(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	seen := make(map[string]struct{})
	var hosts []string
	for _, key := range keys {
		for _, host := range parseSiteAddressList(labels[key]) {
			if !validProxyHost(host, allowWildcards, allowLocalHosts) {
				continue
			}
			if _, exists := seen[host]; exists {
				continue
			}
			seen[host] = struct{}{}
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func parseSiteAddressList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "(") {
		return nil
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})

	var hosts []string
	for _, part := range parts {
		host := normalizeSiteAddress(part)
		if host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func normalizeSiteAddress(raw string) string {
	raw = strings.Trim(strings.TrimSpace(raw), "\"'")
	raw = strings.TrimRight(raw, ",")
	if raw == "" || strings.HasPrefix(raw, "(") || strings.ContainsAny(raw, "{}") {
		return ""
	}
	if strings.HasPrefix(raw, ":") {
		return ""
	}

	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return ""
		}
		return strings.ToLower(u.Host)
	}

	if host, _, err := net.SplitHostPort(raw); err == nil && host != "" {
		return strings.ToLower(host)
	}

	raw = strings.Trim(raw, "[]")
	if strings.Contains(raw, "/") {
		return ""
	}
	return strings.ToLower(raw)
}

func validProxyHost(host string, allowWildcards, allowLocalHosts bool) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.HasPrefix(host, "*.") && !allowWildcards {
		return false
	}
	if !allowLocalHosts && isLocalHost(host) {
		return false
	}
	return strings.Contains(host, ".") || isLocalHost(host)
}

func isLocalHost(host string) bool {
	host = strings.TrimPrefix(strings.ToLower(host), "*.")
	return host == "localhost" || strings.HasSuffix(host, ".local") || net.ParseIP(host) != nil
}

func stringConfig(config map[string]interface{}, key, fallback string) string {
	if value, ok := config[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func boolConfig(config map[string]interface{}, key string, fallback bool) bool {
	if value, ok := config[key].(bool); ok {
		return value
	}
	return fallback
}

func normalizedPort(port string) string {
	port = strings.TrimSpace(port)
	if port == "" || port == "80" || port == "443" {
		return ""
	}
	if strings.HasPrefix(port, ":") {
		return port
	}
	return ":" + port
}

func buildProxyURL(scheme, host, port string) string {
	if strings.Contains(host, "://") {
		return host
	}
	if port != "" {
		if hostOnly, _, err := net.SplitHostPort(host); err == nil && hostOnly != "" {
			host = hostOnly
		}
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, port)
}
