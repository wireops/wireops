package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// GetDataDir returns the root directory for all wireops runtime data.
func GetDataDir() string {
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir != "" {
		return dataDir
	}

	// Backward compatibility for older deployments that only set PB_DATA_DIR.
	if pbDataDir := strings.TrimSpace(os.Getenv("PB_DATA_DIR")); pbDataDir != "" {
		return filepath.Dir(pbDataDir)
	}

	return "./data"
}

// GetPocketBaseDataDir returns the PocketBase data directory.
func GetPocketBaseDataDir() string {
	if pbDataDir := strings.TrimSpace(os.Getenv("PB_DATA_DIR")); pbDataDir != "" {
		return pbDataDir
	}

	return filepath.Join(GetDataDir(), "pb_data")
}

// GetReposWorkspace returns the repository clone workspace path.
func GetReposWorkspace() string {
	if repoWorkspace := strings.TrimSpace(os.Getenv("REPOS_WORKSPACE")); repoWorkspace != "" {
		return repoWorkspace
	}

	return filepath.Join(GetDataDir(), "repos")
}

// GetStacksStoragePath returns the directory used for rendered stack artifacts.
func GetStacksStoragePath() string {
	if stackStorage := strings.TrimSpace(os.Getenv("STACKS_STORAGE_PATH")); stackStorage != "" {
		return stackStorage
	}

	return filepath.Join(GetDataDir(), "stacks")
}

// GetAppURL returns the configured APP_URL or constructs a default based on the bind address
func GetAppURL() string {
	appURL := strings.TrimSpace(os.Getenv("APP_URL"))
	if appURL != "" {
		// Remove trailing slash if present
		appURL = strings.TrimRight(appURL, "/")
		if !strings.Contains(appURL, "://") {
			appURL = "http://" + appURL
		}
		return appURL
	}

	// Default to localhost with default PocketBase port
	return "http://localhost:8090"
}

// GetScanPeriod returns the global interval at which every stack's repository
// is polled for changes. Configured via SCAN_PERIOD (seconds), default 10s.
func GetScanPeriod() time.Duration {
	const defaultSeconds = 10
	if raw := strings.TrimSpace(os.Getenv("SCAN_PERIOD")); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil && val > 0 {
			return time.Duration(val) * time.Second
		}
	}
	return defaultSeconds * time.Second
}

// GetDeployTimeout returns the global default deploy timeout applied when a
// stack does not declare its own deploy_timeout_seconds (via wireops.yaml's
// timeout field). Configured via DEPLOY_TIMEOUT (seconds), default 5m.
func GetDeployTimeout() time.Duration {
	const defaultSeconds = 5 * 60
	if raw := strings.TrimSpace(os.Getenv("DEPLOY_TIMEOUT")); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil && val > 0 {
			return time.Duration(val) * time.Second
		}
	}
	return defaultSeconds * time.Second
}

// GetWebhookURL returns the full webhook URL for a given stack ID
func GetWebhookURL(stackID string) string {
	appURL := GetAppURL()
	return appURL + "/api/custom/webhook/" + stackID
}
