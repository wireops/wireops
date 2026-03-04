package config

import (
	"os"
	"strings"
)

// GetAppURL returns the configured APP_URL or constructs a default based on the bind address
func GetAppURL() string {
	appURL := strings.TrimSpace(os.Getenv("APP_URL"))
	if appURL != "" {
		// Remove trailing slash if present
		return strings.TrimRight(appURL, "/")
	}

	// Default to localhost with default PocketBase port
	return "http://localhost:8090"
}

// GetWebhookURL returns the full webhook URL for a given stack ID
func GetWebhookURL(stackID string) string {
	appURL := GetAppURL()
	return appURL + "/api/custom/webhook/" + stackID
}
