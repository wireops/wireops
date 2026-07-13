package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	infisical "github.com/infisical/go-sdk"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/crypto"
)

// infisicalClientTimeout bounds Universal Auth login + secret retrieval.
// Kept short (like vaultClientTimeout) so one unreachable Infisical instance
// fails fast per secret instead of consuming the whole deploy's time budget.
const infisicalClientTimeout = 10 * time.Second

// DefaultInfisicalSiteURL is used when a backend's config omits site_url.
const DefaultInfisicalSiteURL = "https://app.infisical.com"

// InfisicalSecretProvider resolves secrets stored in Infisical via Universal
// Auth (machine identity). rawValue is a
// "<project-id>/<environment>/<secret-path>#<SECRET_NAME>" reference, e.g.
// "64f1.../production//db#DB_PASS" or "64f1.../production#DB_PASS" (path
// defaults to "/").
type InfisicalSecretProvider struct {
	app core.App
}

// NewInfisicalProvider creates an InfisicalSecretProvider backed by
// connection config stored in the integrations collection (slug
// "infisical").
func NewInfisicalProvider(app core.App) *InfisicalSecretProvider {
	return &InfisicalSecretProvider{app: app}
}

// Name implements SecretProvider.
func (p *InfisicalSecretProvider) Name() string { return "infisical" }

// Resolve authenticates against Infisical via Universal Auth and retrieves
// the plaintext value of the secret encoded in rawValue.
func (p *InfisicalSecretProvider) Resolve(ctx context.Context, rawValue string) (string, error) {
	projectID, environment, secretPath, secretName, err := parseInfisicalReference(rawValue)
	if err != nil {
		return "", err
	}

	client, _, allowedProjectID, cancel, err := BuildInfisicalClient(ctx, p.app)
	if err != nil {
		return "", err
	}
	defer cancel()
	if allowedProjectID != "" && projectID != allowedProjectID {
		return "", fmt.Errorf("infisical: project %q is not permitted (backend is restricted to project %q)", projectID, allowedProjectID)
	}

	secret, err := client.Secrets().Retrieve(infisical.RetrieveSecretOptions{
		SecretKey:   secretName,
		ProjectID:   projectID,
		Environment: environment,
		SecretPath:  secretPath,
	})
	if err != nil {
		return "", fmt.Errorf("infisical: failed to retrieve secret %q: %w", secretName, err)
	}
	return secret.SecretValue, nil
}

// parseInfisicalReference splits a
// "<project-id>/<environment>/<secret-path>#<SECRET_NAME>" rawValue into its
// parts. secret-path is optional and defaults to "/".
func parseInfisicalReference(rawValue string) (projectID, environment, secretPath, secretName string, err error) {
	idx := strings.LastIndex(rawValue, "#")
	if idx == -1 || idx == len(rawValue)-1 {
		return "", "", "", "", fmt.Errorf(`infisical: invalid reference %q, expected "<project-id>/<environment>/<secret-path>#<SECRET_NAME>"`, rawValue)
	}
	secretName = rawValue[idx+1:]
	locator := rawValue[:idx]

	parts := strings.SplitN(locator, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", "", fmt.Errorf(`infisical: invalid reference %q, expected "<project-id>/<environment>/<secret-path>#<SECRET_NAME>"`, rawValue)
	}
	projectID = parts[0]
	environment = parts[1]
	secretPath = "/"
	if len(parts) == 3 && parts[2] != "" {
		secretPath = "/" + strings.TrimPrefix(parts[2], "/")
	}
	return projectID, environment, secretPath, secretName, nil
}

// BuildInfisicalClient loads the Infisical connection config from the
// integrations collection (slug "infisical"), authenticates via Universal
// Auth, and returns a ready-to-use client plus the configured site URL (for
// callers that also need to hit REST endpoints the SDK doesn't wrap, like
// project/environment listing) and the configured allowed-project
// restriction (empty means unrestricted). Shared by
// InfisicalSecretProvider.Resolve and the Infisical browse routes so both
// use identical connection/decrypt logic. The returned cancel func must be
// deferred by the caller — it bounds the client's requests to
// infisicalClientTimeout and must stay live for as long as the client is
// used.
func BuildInfisicalClient(ctx context.Context, app core.App) (infisical.InfisicalClientInterface, string, string, context.CancelFunc, error) {
	if app == nil {
		return nil, "", "", nil, errors.New("infisical: app is not configured")
	}

	rec, err := app.FindFirstRecordByFilter("integrations", "slug = {:slug}", map[string]any{"slug": "infisical"})
	if err != nil {
		return nil, "", "", nil, errors.New("infisical: backend is not configured")
	}
	if !rec.GetBool("enabled") {
		return nil, "", "", nil, errors.New("infisical: backend is disabled")
	}

	var cfg struct {
		SiteURL          string `json:"site_url"`
		ClientID         string `json:"client_id"`
		ClientSecret     string `json:"client_secret"`
		AllowedProjectID string `json:"allowed_project_id"`
	}
	if err := rec.UnmarshalJSONField("config", &cfg); err != nil {
		return nil, "", "", nil, fmt.Errorf("infisical: failed to read backend config: %w", err)
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, "", "", nil, errors.New("infisical: backend config is missing client_id or client_secret")
	}
	siteURL := cfg.SiteURL
	if siteURL == "" {
		siteURL = DefaultInfisicalSiteURL
	}

	secretKeyBytes := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	clientSecretBytes, err := crypto.Decrypt(cfg.ClientSecret, secretKeyBytes)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("infisical: failed to decrypt client_secret: %w", err)
	}

	client, cancel, err := buildInfisicalClientFromParts(ctx, siteURL, cfg.ClientID, string(clientSecretBytes))
	if err != nil {
		return nil, "", "", nil, err
	}

	return client, siteURL, cfg.AllowedProjectID, cancel, nil
}

// NewInfisicalClientForConfig authenticates against Infisical via Universal
// Auth using raw site URL/client ID/client secret values, without touching
// the integrations collection. Used by the test-connection route to
// validate unsaved form input. The returned cancel func must be deferred by
// the caller.
func NewInfisicalClientForConfig(ctx context.Context, siteURL, clientID, clientSecret string) (infisical.InfisicalClientInterface, context.CancelFunc, error) {
	return buildInfisicalClientFromParts(ctx, siteURL, clientID, clientSecret)
}

// buildInfisicalClientFromParts authenticates against Infisical via
// Universal Auth using raw site URL/client ID/client secret values, without
// touching the integrations collection. Used by BuildInfisicalClient and the
// test-connection route, which validates unsaved form input. The returned
// cancel func must be deferred by the caller.
func buildInfisicalClientFromParts(ctx context.Context, siteURL, clientID, clientSecret string) (infisical.InfisicalClientInterface, context.CancelFunc, error) {
	callCtx, cancel := context.WithTimeout(ctx, infisicalClientTimeout)

	// AutoTokenRefresh is disabled: this client is one-shot (built fresh per
	// call, per the no-caching design), so the SDK's background
	// token-refresh goroutine would otherwise leak for the lifetime of ctx.
	client := infisical.NewInfisicalClient(callCtx, infisical.Config{
		SiteUrl:          siteURL,
		AutoTokenRefresh: infisical.BoolPtr(false),
		SilentMode:       true,
	})

	if _, err := client.Auth().UniversalAuthLogin(clientID, clientSecret); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("infisical: universal auth login failed: %w", err)
	}

	return client, cancel, nil
}
