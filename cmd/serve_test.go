package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestValidateOIDCURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		fieldName string
		appURL    string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid https URL",
			url:       "https://idp.example.com/oauth2/authorize",
			fieldName: "OIDC_AUTH_URL",
			appURL:    "https://app.example.com",
			wantErr:   false,
		},
		{
			name:      "valid http URL in development",
			url:       "http://localhost:9000/oauth2/authorize",
			fieldName: "OIDC_AUTH_URL",
			appURL:    "http://localhost:8090",
			wantErr:   false,
		},
		{
			name:      "http URL rejected in production",
			url:       "http://idp.example.com/oauth2/authorize",
			fieldName: "OIDC_AUTH_URL",
			appURL:    "https://app.example.com",
			wantErr:   true,
			errMsg:    "must use HTTPS in production",
		},
		{
			name:      "empty URL",
			url:       "",
			fieldName: "OIDC_AUTH_URL",
			appURL:    "http://localhost:8090",
			wantErr:   true,
			errMsg:    "is required",
		},
		{
			name:      "empty OIDC_USER_INFO_URL optional",
			url:       "",
			fieldName: "OIDC_USER_INFO_URL",
			appURL:    "http://localhost:8090",
			wantErr:   false,
		},
		{
			name:      "invalid scheme",
			url:       "ftp://idp.example.com/oauth2",
			fieldName: "OIDC_TOKEN_URL",
			appURL:    "http://localhost:8090",
			wantErr:   true,
			errMsg:    "must use http or https",
		},
		{
			name:      "missing host",
			url:       "https:///oauth2/authorize",
			fieldName: "OIDC_AUTH_URL",
			appURL:    "http://localhost:8090",
			wantErr:   true,
			errMsg:    "must have a host",
		},
		{
			name:      "URL with port",
			url:       "https://idp.example.com:8443/oauth2/authorize",
			fieldName: "OIDC_AUTH_URL",
			appURL:    "https://app.example.com",
			wantErr:   false,
		},
		{
			name:      "URL with path and query",
			url:       "https://idp.example.com/oauth2/authorize?client_id=test",
			fieldName: "OIDC_AUTH_URL",
			appURL:    "https://app.example.com",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set APP_URL environment variable
			os.Setenv("APP_URL", tt.appURL)
			defer os.Unsetenv("APP_URL")

			err := validateOIDCURL(tt.url, tt.fieldName)

			if tt.wantErr {
				if err == nil {
					t.Errorf("validateOIDCURL(%q) expected error, got nil", tt.url)
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateOIDCURL(%q) error = %q, want to contain %q", tt.url, err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateOIDCURL(%q) unexpected error: %v", tt.url, err)
				}
			}
		})
	}
}
