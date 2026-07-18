package cmd

import (
	"encoding/hex"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func TestGetAllowedOriginsIncludesLoopbackDevOrigins(t *testing.T) {
	originalOrigins := allowedOrigins
	originalOnce := allowedOriginsOnce
	allowedOrigins = nil
	allowedOriginsOnce = sync.Once{}
	t.Cleanup(func() {
		allowedOrigins = originalOrigins
		allowedOriginsOnce = originalOnce
	})

	os.Setenv("APP_URL", "http://127.0.0.1:8090")
	defer os.Unsetenv("APP_URL")

	origins := getAllowedOrigins()

	for _, expected := range []string{
		"http://127.0.0.1:8090",
		"http://localhost:3000",
		"http://localhost:5173",
		"http://127.0.0.1:3000",
		"http://127.0.0.1:5173",
	} {
		if !slices.Contains(origins, expected) {
			t.Fatalf("expected allowed origins to contain %q, got %v", expected, origins)
		}
	}
}

func TestValidateStartupSecretKey(t *testing.T) {
	raw := "12345678901234567890123456789012"
	hexKey := hex.EncodeToString([]byte(raw))

	tests := []struct {
		name    string
		key     string
		wantErr string
	}{
		{
			name: "raw key",
			key:  raw,
		},
		{
			name: "hex key",
			key:  hexKey,
		},
		{
			name:    "missing key",
			key:     "",
			wantErr: "invalid SECRET_KEY: SECRET_KEY is required",
		},
		{
			name:    "invalid key",
			key:     "too-short",
			wantErr: "invalid SECRET_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SECRET_KEY", tt.key)

			err := validateStartupSecretKey()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateStartupSecretKey() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateStartupSecretKey() expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateStartupSecretKey() error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

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

// Regression test: syncSuperusers must copy the real bcrypt hash so the
// mirrored "_superusers" record can authenticate with the admin's actual
// password. It previously read/wrote a nonexistent "passwordHash" field
// (PocketBase stores it under the "password" field, read via the
// "password:hash" getter key), silently leaving the mirrored record's
// password unset.
func TestSyncSuperusersCopiesRealPasswordHash(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("new test app: %v", err)
	}
	t.Cleanup(func() { app.Cleanup() })

	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}

	admin := core.NewRecord(users)
	admin.Set("email", "admin@example.com")
	admin.Set("password", "correct-horse-battery-staple")
	admin.Set("role", "admin")
	admin.Set("verified", true)
	if err := app.Save(admin); err != nil {
		t.Fatalf("save admin user: %v", err)
	}

	syncSuperusers(app)

	superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "admin@example.com")
	if err != nil {
		t.Fatalf("expected mirrored superuser to exist: %v", err)
	}
	if !superuser.ValidatePassword("correct-horse-battery-staple") {
		t.Fatal("expected mirrored superuser to authenticate with the admin's real password")
	}
}
