package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryHelpers(t *testing.T) {
	t.Run("GetDataDir", func(t *testing.T) {
		tests := []struct {
			name      string
			env       map[string]string
			unset     []string
			expected  string
		}{
			{
				name:     "RespectsDataDirEnv",
				env:      map[string]string{"DATA_DIR": "/srv/wireops"},
				unset:    []string{"PB_DATA_DIR"},
				expected: "/srv/wireops",
			},
			{
				name: "DataDirTakesPrecedenceOverPocketBaseDataDir",
				env: map[string]string{
					"DATA_DIR":    "/srv/wireops",
					"PB_DATA_DIR": "/srv/wireops/pb_override",
				},
				expected: "/srv/wireops",
			},
			{
				name:     "UsesPocketBaseParentDirForBackwardCompatibility",
				env:      map[string]string{"PB_DATA_DIR": "/srv/legacy/pb_data"},
				unset:    []string{"DATA_DIR"},
				expected: filepath.Dir("/srv/legacy/pb_data"),
			},
			{
				name:     "DefaultsWhenUnset",
				unset:    []string{"DATA_DIR", "PB_DATA_DIR"},
				expected: "./data",
			},
			{
				name:     "TrimsWhitespace",
				env:      map[string]string{"DATA_DIR": "  /srv/trimmed  "},
				unset:    []string{"PB_DATA_DIR"},
				expected: "/srv/trimmed",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				applyEnv(t, tt.env, tt.unset...)
				if got := GetDataDir(); got != tt.expected {
					t.Fatalf("GetDataDir() = %q, want %q", got, tt.expected)
				}
			})
		}
	})

	t.Run("GetPocketBaseDataDir", func(t *testing.T) {
		tests := []struct {
			name     string
			env      map[string]string
			unset    []string
			expected string
		}{
			{
				name:     "RespectsPocketBaseDataDirEnv",
				env:      map[string]string{"PB_DATA_DIR": "/srv/custom/pb_data"},
				unset:    []string{"DATA_DIR"},
				expected: "/srv/custom/pb_data",
			},
			{
				name:     "FallsBackToDataDir",
				env:      map[string]string{"DATA_DIR": "/srv/wireops"},
				unset:    []string{"PB_DATA_DIR"},
				expected: filepath.Join("/srv/wireops", "pb_data"),
			},
			{
				name:     "DefaultsWhenUnset",
				unset:    []string{"DATA_DIR", "PB_DATA_DIR"},
				expected: filepath.Join("./data", "pb_data"),
			},
			{
				name:     "TrimsWhitespace",
				env:      map[string]string{"PB_DATA_DIR": "  /srv/trimmed/pb_data  "},
				unset:    []string{"DATA_DIR"},
				expected: "/srv/trimmed/pb_data",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				applyEnv(t, tt.env, tt.unset...)
				if got := GetPocketBaseDataDir(); got != tt.expected {
					t.Fatalf("GetPocketBaseDataDir() = %q, want %q", got, tt.expected)
				}
			})
		}
	})

	t.Run("GetReposWorkspace", func(t *testing.T) {
		tests := []struct {
			name     string
			env      map[string]string
			unset    []string
			expected string
		}{
			{
				name:     "RespectsReposWorkspaceEnv",
				env:      map[string]string{"REPOS_WORKSPACE": "/srv/repos"},
				unset:    []string{"DATA_DIR", "PB_DATA_DIR"},
				expected: "/srv/repos",
			},
			{
				name:     "FallsBackToDataDir",
				env:      map[string]string{"DATA_DIR": "/srv/wireops"},
				unset:    []string{"REPOS_WORKSPACE", "PB_DATA_DIR"},
				expected: filepath.Join("/srv/wireops", "repos"),
			},
			{
				name:     "UsesBackwardCompatibleDataDirFallback",
				env:      map[string]string{"PB_DATA_DIR": "/srv/legacy/pb_data"},
				unset:    []string{"REPOS_WORKSPACE", "DATA_DIR"},
				expected: filepath.Join(filepath.Dir("/srv/legacy/pb_data"), "repos"),
			},
			{
				name:     "DefaultsWhenUnset",
				unset:    []string{"REPOS_WORKSPACE", "DATA_DIR", "PB_DATA_DIR"},
				expected: filepath.Join("./data", "repos"),
			},
			{
				name:     "TrimsWhitespace",
				env:      map[string]string{"REPOS_WORKSPACE": "  /srv/trimmed/repos  "},
				unset:    []string{"DATA_DIR", "PB_DATA_DIR"},
				expected: "/srv/trimmed/repos",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				applyEnv(t, tt.env, tt.unset...)
				if got := GetReposWorkspace(); got != tt.expected {
					t.Fatalf("GetReposWorkspace() = %q, want %q", got, tt.expected)
				}
			})
		}
	})

	t.Run("GetStacksStoragePath", func(t *testing.T) {
		tests := []struct {
			name     string
			env      map[string]string
			unset    []string
			expected string
		}{
			{
				name:     "RespectsStacksStoragePathEnv",
				env:      map[string]string{"STACKS_STORAGE_PATH": "/srv/stacks"},
				unset:    []string{"DATA_DIR", "PB_DATA_DIR"},
				expected: "/srv/stacks",
			},
			{
				name:     "FallsBackToDataDir",
				env:      map[string]string{"DATA_DIR": "/srv/wireops"},
				unset:    []string{"STACKS_STORAGE_PATH", "PB_DATA_DIR"},
				expected: filepath.Join("/srv/wireops", "stacks"),
			},
			{
				name:     "UsesBackwardCompatibleDataDirFallback",
				env:      map[string]string{"PB_DATA_DIR": "/srv/legacy/pb_data"},
				unset:    []string{"STACKS_STORAGE_PATH", "DATA_DIR"},
				expected: filepath.Join(filepath.Dir("/srv/legacy/pb_data"), "stacks"),
			},
			{
				name:     "DefaultsWhenUnset",
				unset:    []string{"STACKS_STORAGE_PATH", "DATA_DIR", "PB_DATA_DIR"},
				expected: filepath.Join("./data", "stacks"),
			},
			{
				name:     "TrimsWhitespace",
				env:      map[string]string{"STACKS_STORAGE_PATH": "  /srv/trimmed/stacks  "},
				unset:    []string{"DATA_DIR", "PB_DATA_DIR"},
				expected: "/srv/trimmed/stacks",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				applyEnv(t, tt.env, tt.unset...)
				if got := GetStacksStoragePath(); got != tt.expected {
					t.Fatalf("GetStacksStoragePath() = %q, want %q", got, tt.expected)
				}
			})
		}
	})
}

func applyEnv(t *testing.T, set map[string]string, unset ...string) {
	t.Helper()
	for _, key := range unset {
		t.Setenv(key, "")
	}
	for key, value := range set {
		t.Setenv(key, value)
	}
}

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
		{
			name:     "NoSchemeHostAndPort",
			envValue: "10.0.0.100:8090",
			expected: "http://10.0.0.100:8090",
		},
		{
			name:     "NoSchemeHostOnly",
			envValue: "wireops.example.com",
			expected: "http://wireops.example.com",
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

func TestGetBackupUploadMaxBytes(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("BACKUP_UPLOAD_MAX_MB", "")
		os.Unsetenv("BACKUP_UPLOAD_MAX_MB")
		want := int64(4096) * 1024 * 1024
		if got := GetBackupUploadMaxBytes(); got != want {
			t.Errorf("GetBackupUploadMaxBytes() = %d, want %d", got, want)
		}
	})

	t.Run("configured", func(t *testing.T) {
		t.Setenv("BACKUP_UPLOAD_MAX_MB", "10")
		want := int64(10) * 1024 * 1024
		if got := GetBackupUploadMaxBytes(); got != want {
			t.Errorf("GetBackupUploadMaxBytes() = %d, want %d", got, want)
		}
	})

	t.Run("invalid falls back to default", func(t *testing.T) {
		t.Setenv("BACKUP_UPLOAD_MAX_MB", "not-a-number")
		want := int64(4096) * 1024 * 1024
		if got := GetBackupUploadMaxBytes(); got != want {
			t.Errorf("GetBackupUploadMaxBytes() = %d, want %d", got, want)
		}
	})
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
