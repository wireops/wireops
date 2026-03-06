package notify

import (
	"encoding/json"
	"net/http"
)

// Config represents the unified notification configuration.
// It supports both Webhook and Ntfy providers.
type Config struct {
	Provider     string   // "webhook" | "ntfy"
	URL          string
	Secret       string   // webhook: HMAC secret; ntfy: password
	Events       []string
	Headers      []Header // only used by webhook
	Enabled      bool
	NtfyUser     string // only used by ntfy (optional)
	NtfyTopic    string // only used by ntfy (required if provider=ntfy)
	NtfyTemplate string // only used by ntfy (optional Go template)
}

// Header is a key/value pair stored in the `headers` JSON field.
type Header struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Provider defines the interface for notification providers.
type Provider interface {
	// Send dispatches the notification.
	Send(cfg *Config, p Payload) error
}

// NewProvider returns the appropriate Provider implementation based on the config.
func NewProvider(client *http.Client, providerType string) Provider {
	switch providerType {
	case "ntfy":
		return &NtfyProvider{client: client}
	case "webhook":
		fallthrough
	default:
		return &WebhookProvider{client: client}
	}
}

// Helper to check if an event is subscribed.
func (c *Config) Subscribes(event string) bool {
	for _, e := range c.Events {
		if e == event {
			return true
		}
	}
	return false
}

// Helper to unmarshal headers from JSON string.
func UnmarshalHeaders(raw string) []Header {
	if raw == "" || raw == "null" {
		return nil
	}
	var headers []Header
	if err := json.Unmarshal([]byte(raw), &headers); err == nil {
		return headers
	}
	return nil
}
