package config

import (
	"math"
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

// GetTerminalSessionsStoragePath returns the directory used for recorded
// interactive terminal session transcripts (see docs/TERMINAL.md). Nested
// under the PocketBase data dir (not DATA_DIR directly) so transcripts ride
// along with pb_data in backups/volume mounts instead of being a second,
// easy-to-forget top-level directory to include.
func GetTerminalSessionsStoragePath() string {
	if p := strings.TrimSpace(os.Getenv("TERMINAL_SESSIONS_STORAGE_PATH")); p != "" {
		return p
	}

	return filepath.Join(GetPocketBaseDataDir(), "terminal_sessions")
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

// GetComposeMaxBytes returns the maximum size of a resolved compose config the
// server will buffer and parse from `docker compose config`. Configured via
// COMPOSE_MAX_KB (kilobytes), default 512 KB.
//
// The bound matters because the size is driven by repository content: a stack's
// compose file decides how much the server allocates to hold, JSON-decode, and
// walk the resolved config. 512 KB is far above any realistic stack (a large
// one resolves to tens of KB) while keeping a pathological or hostile repo from
// turning a sync or a lint into unbounded memory use. Operators with a genuinely
// enormous compose file can raise it.
func GetComposeMaxBytes() int64 {
	const defaultKB = 512
	const maxKB = math.MaxInt64 / 1024 // cap so kb*1024 can't overflow int64
	kb := int64(defaultKB)
	if raw := strings.TrimSpace(os.Getenv("COMPOSE_MAX_KB")); raw != "" {
		if val, err := strconv.ParseInt(raw, 10, 64); err == nil && val > 0 && val <= maxKB {
			kb = val
		}
	}
	return kb * 1024
}

// GetBackupUploadMaxBytes returns the maximum size accepted for an uploaded
// backup archive. Configured via BACKUP_UPLOAD_MAX_MB (megabytes), default
// 4096 MB (4 GiB) — generous for a DATA_DIR dump while still bounding disk
// usage from an unauthenticated-adjacent-risk upload endpoint.
func GetBackupUploadMaxBytes() int64 {
	const defaultMB = 4096
	const maxMB = math.MaxInt64 / (1024 * 1024) // cap so mb*1024*1024 can't overflow int64
	mb := int64(defaultMB)
	if raw := strings.TrimSpace(os.Getenv("BACKUP_UPLOAD_MAX_MB")); raw != "" {
		if val, err := strconv.ParseInt(raw, 10, 64); err == nil && val > 0 && val <= maxMB {
			mb = val
		}
	}
	return mb * 1024 * 1024
}

// GetBackupMaxCount returns the maximum number of backups (any source:
// manual create, scheduled cron, or upload) allowed to exist in the backups
// filesystem at once. Configured via BACKUP_MAX_COUNT, default 100 — manual
// creation has no automatic retention (unlike the cron job's
// cron_max_keep), so without a cap a CapManageSettings user could
// repeatedly create named backups and exhaust disk/S3 storage.
func GetBackupMaxCount() int {
	const defaultCount = 100
	if raw := strings.TrimSpace(os.Getenv("BACKUP_MAX_COUNT")); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil && val > 0 {
			return val
		}
	}
	return defaultCount
}

// GetWebhookURL returns the full webhook URL for a given stack ID
func GetWebhookURL(stackID string) string {
	appURL := GetAppURL()
	return appURL + "/api/custom/webhook/" + stackID
}

// GetGitHubOAuthClientID returns the GitHub OAuth App client ID, configured
// via GITHUB_OAUTH_CLIENT_ID. Empty means the GitHub provider is unconfigured.
func GetGitHubOAuthClientID() string {
	return strings.TrimSpace(os.Getenv("GITHUB_OAUTH_CLIENT_ID"))
}

// GetGitHubOAuthClientSecret returns the GitHub OAuth App client secret,
// configured via GITHUB_OAUTH_CLIENT_SECRET.
func GetGitHubOAuthClientSecret() string {
	return strings.TrimSpace(os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"))
}

// GetGitProviderCallbackURL returns the server-computed OAuth callback URL
// for a given provider slug. Never accept a client-supplied redirect_uri —
// OAuth Apps register exactly one callback URL anyway, and this removes an
// open-redirect-shaped parameter entirely.
func GetGitProviderCallbackURL(slug string) string {
	return GetAppURL() + "/api/custom/git-providers/" + slug + "/callback"
}
