package config

import (
	"os"
	"testing"
)

func TestGetAppURL(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "DefaultWhenNotSet",
			envValue: "",
			expected: "http://localhost:8090",
		},
		{
			name:     "CustomURL",
			envValue: "https://wireops.example.com",
			expected: "https://wireops.example.com",
		},
		{
			name:     "URLWithPort",
			envValue: "http://192.168.1.100:8090",
			expected: "http://192.168.1.100:8090",
		},
		{
			name:     "RemovesTrailingSlash",
			envValue: "https://wireops.example.com/",
			expected: "https://wireops.example.com",
		},
		{
			name:     "RemovesMultipleTrailingSlashes",
			envValue: "https://wireops.example.com///",
			expected: "https://wireops.example.com",
		},
		{
			name:     "HandlesWhitespace",
			envValue: "  https://wireops.example.com  ",
			expected: "https://wireops.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original value
			originalValue := os.Getenv("APP_URL")
			defer os.Setenv("APP_URL", originalValue)

			// Set test value
			if tt.envValue == "" {
				os.Unsetenv("APP_URL")
			} else {
				os.Setenv("APP_URL", tt.envValue)
			}

			// Test
			result := GetAppURL()
			if result != tt.expected {
				t.Errorf("GetAppURL() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetWebhookURL(t *testing.T) {
	tests := []struct {
		name     string
		stackID  string
		appURL   string
		expected string
	}{
		{
			name:     "DefaultAppURL",
			stackID:  "abc123",
			appURL:   "",
			expected: "http://localhost:8090/api/custom/webhook/abc123",
		},
		{
			name:     "CustomAppURL",
			stackID:  "xyz789",
			appURL:   "https://wireops.example.com",
			expected: "https://wireops.example.com/api/custom/webhook/xyz789",
		},
		{
			name:     "AppURLWithPort",
			stackID:  "test456",
			appURL:   "http://192.168.1.100:8090",
			expected: "http://192.168.1.100:8090/api/custom/webhook/test456",
		},
		{
			name:     "AppURLWithTrailingSlash",
			stackID:  "stack001",
			appURL:   "https://wireops.example.com/",
			expected: "https://wireops.example.com/api/custom/webhook/stack001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original value
			originalValue := os.Getenv("APP_URL")
			defer os.Setenv("APP_URL", originalValue)

			// Set test value
			if tt.appURL == "" {
				os.Unsetenv("APP_URL")
			} else {
				os.Setenv("APP_URL", tt.appURL)
			}

			// Test
			result := GetWebhookURL(tt.stackID)
			if result != tt.expected {
				t.Errorf("GetWebhookURL(%v) = %v, want %v", tt.stackID, result, tt.expected)
			}
		})
	}
}
