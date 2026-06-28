package nginxproxymanager

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/wireops/wireops/internal/integrations"
)

// NginxProxyManagerIntegration creates application links from wireops proxy hint labels.
type NginxProxyManagerIntegration struct{}

func init() {
	integrations.Register(&NginxProxyManagerIntegration{})
}

func (n *NginxProxyManagerIntegration) Slug() string {
	return "nginx-proxy-manager"
}

func (n *NginxProxyManagerIntegration) Name() string {
	return "Nginx Proxy Manager"
}

func (n *NginxProxyManagerIntegration) Category() string {
	return "Reverse Proxy"
}

func (n *NginxProxyManagerIntegration) ResolveContainerActions(config map[string]interface{}, ctx integrations.ContainerContext) []integrations.ContainerAction {
	scheme := firstNonEmpty(
		labelValue(ctx.Labels, "dev.wireops.npm.scheme", "dev.wireops.proxy.scheme"),
		stringConfig(config, "scheme", "https"),
	)
	port := normalizedPort(stringConfig(config, "port", ""))
	adminURL := strings.TrimRight(stringConfig(config, "admin_url", ""), "/")
	allowLocalHosts := boolConfig(config, "allow_local_hosts", true)

	hosts := proxyHintHosts(ctx.Labels, allowLocalHosts)
	if len(hosts) == 0 {
		return nil
	}

	actions := make([]integrations.ContainerAction, 0, len(hosts)+1)
	for _, host := range hosts {
		actions = append(actions, integrations.ContainerAction{
			IntegrationSlug: n.Slug(),
			Kind:            integrations.ActionKindReverseProxy,
			Label:           "Open",
			URL:             buildProxyURL(scheme, host, port),
			Icon:            "i-lucide-external-link",
		})
	}
	if adminURL != "" {
		actions = append(actions, integrations.ContainerAction{
			IntegrationSlug: n.Slug(),
			Kind:            integrations.ActionKindReverseProxy,
			Label:           "NPM Admin",
			URL:             adminURL,
			Icon:            "i-lucide-settings",
		})
	}
	return actions
}

func proxyHintHosts(labels map[string]string, allowLocalHosts bool) []string {
	rawValues := []string{
		labelValue(labels, "dev.wireops.npm.host", "dev.wireops.npm.hosts"),
		labelValue(labels, "dev.wireops.proxy.host", "dev.wireops.proxy.hosts"),
	}

	seen := make(map[string]struct{})
	var hosts []string
	for _, raw := range rawValues {
		for _, host := range parseProxyHostList(raw) {
			if !validProxyHost(host, allowLocalHosts) {
				continue
			}
			if _, exists := seen[host]; exists {
				continue
			}
			seen[host] = struct{}{}
			hosts = append(hosts, host)
		}
	}
	sort.Strings(hosts)
	return hosts
}

func labelValue(labels map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(labels[key]); value != "" {
			return value
		}
	}
	return ""
}

func parseProxyHostList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})

	var hosts []string
	for _, part := range parts {
		host := normalizeProxyHost(part)
		if host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func normalizeProxyHost(raw string) string {
	raw = strings.Trim(strings.TrimSpace(raw), "\"'")
	raw = strings.TrimRight(raw, ",")
	if raw == "" || strings.ContainsAny(raw, "{}") || strings.HasPrefix(raw, ":") {
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

	if strings.Contains(raw, "/") {
		return ""
	}
	return strings.ToLower(raw)
}

func validProxyHost(host string, allowLocalHosts bool) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if !allowLocalHosts && isLocalHost(host) {
		return false
	}
	return strings.Contains(host, ".") || isLocalHost(host)
}

func isLocalHost(host string) bool {
	host = strings.ToLower(host)
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
	if port != "" {
		if hostOnly, _, err := net.SplitHostPort(host); err == nil && hostOnly != "" {
			host = hostOnly
		}
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, port)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
