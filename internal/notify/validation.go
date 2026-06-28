package notify

import (
	"fmt"
	"net/url"
	"strings"
)

var notificationWebhookHosts = map[string]map[string]bool{
	"discord": {
		"discord.com":    true,
		"discordapp.com": true,
	},
	"slack": {
		"hooks.slack.com": true,
	},
}

// ValidateProviderURL verifies provider-specific webhook URL host allowlists.
func ValidateProviderURL(provider, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	allowedHosts, ok := notificationWebhookHosts[provider]
	if !ok {
		return nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s webhook URL is invalid", provider)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("%s webhook URL must use https", provider)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s webhook URL must not include credentials", provider)
	}
	if parsed.Port() != "" {
		return fmt.Errorf("%s webhook URL must not include a custom port", provider)
	}

	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if !allowedHosts[host] {
		return fmt.Errorf("%s webhook URL host %q is not allowed", provider, host)
	}

	path := strings.TrimRight(parsed.EscapedPath(), "/")
	switch provider {
	case "discord":
		if !strings.HasPrefix(path, "/api/webhooks/") {
			return fmt.Errorf("discord webhook URL must use the /api/webhooks path")
		}
	case "slack":
		if !strings.HasPrefix(path, "/services/") {
			return fmt.Errorf("slack webhook URL must use the /services path")
		}
	}

	return nil
}

// ValidateIntegrationConfig verifies notification configuration before save or test.
func ValidateIntegrationConfig(provider string, cfg map[string]interface{}) error {
	if cfg == nil {
		return nil
	}
	value, ok := cfg["url"]
	if !ok {
		return nil
	}
	rawURL, ok := value.(string)
	if !ok {
		return fmt.Errorf("%s webhook URL must be a string", provider)
	}
	return ValidateProviderURL(provider, rawURL)
}
