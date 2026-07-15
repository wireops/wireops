package secrets

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/crypto"
)

// vaultClientTimeout bounds a single Vault HTTP call. It is intentionally
// shorter than config.GetDeployTimeout() so one unreachable Vault instance
// fails fast per secret instead of consuming the whole deploy's time budget.
const vaultClientTimeout = 10 * time.Second

// VaultSecretProvider resolves secrets stored in HashiCorp Vault's KV v2
// secrets engine. rawValue is a "<mount>/data/<path>#<field>" reference,
// e.g. "secret/data/myapp#DB_PASS".
type VaultSecretProvider struct {
	app core.App
}

// NewVaultProvider creates a VaultSecretProvider backed by connection config
// stored in the integrations collection (slug "vault").
func NewVaultProvider(app core.App) *VaultSecretProvider { return &VaultSecretProvider{app: app} }

// Name implements SecretProvider.
func (p *VaultSecretProvider) Name() string { return "vault" }

// vaultConn is the per-pass cached connection: a built client plus its
// allowed-mount restriction, keyed in the resolve cache under "vault".
type vaultConn struct {
	client       *vaultapi.Client
	allowedMount string
}

// Resolve reads the secret at the KV v2 path encoded in rawValue and returns
// the requested field's plaintext value.
func (p *VaultSecretProvider) Resolve(ctx context.Context, rawValue string) (string, error) {
	mountPath, field, err := parseVaultReference(rawValue)
	if err != nil {
		return "", err
	}

	conn, err := loadCachedConn(ctx, "vault", func() (vaultConn, error) {
		client, allowedMount, err := BuildVaultClient(p.app)
		return vaultConn{client: client, allowedMount: allowedMount}, err
	})
	if err != nil {
		return "", err
	}
	client, allowedMount := conn.client, conn.allowedMount
	if allowedMount != "" {
		mount := strings.SplitN(mountPath, "/", 2)[0]
		if mount != allowedMount {
			return "", fmt.Errorf("vault: mount %q is not permitted (backend is restricted to mount %q)", mount, allowedMount)
		}
	}

	secret, err := client.Logical().ReadWithContext(ctx, mountPath)
	if err != nil {
		return "", fmt.Errorf("vault: failed to read %q: %w", mountPath, err)
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("vault: no secret found at %q", mountPath)
	}

	// KV v2 wraps the actual key/value payload under a "data" envelope.
	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("vault: %q does not look like a KV v2 secret (missing data envelope)", mountPath)
	}

	value, ok := data[field]
	if !ok {
		return "", fmt.Errorf("vault: field %q not found at %q", field, mountPath)
	}
	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("vault: field %q at %q is not a string", field, mountPath)
	}
	return str, nil
}

// parseVaultReference splits a "<mount>/data/<path>#<field>" rawValue into
// the KV v2 read path ("<mount>/data/<path>") and the field name.
func parseVaultReference(rawValue string) (mountPath, field string, err error) {
	invalid := fmt.Errorf(`vault: invalid reference %q, expected "<mount>/data/<path>#<field>"`, rawValue)

	idx := strings.LastIndex(rawValue, "#")
	if idx == -1 || idx == len(rawValue)-1 {
		return "", "", invalid
	}
	mountPath = rawValue[:idx]
	field = rawValue[idx+1:]
	if mountPath == "" || field == "" {
		return "", "", invalid
	}

	dataIdx := strings.Index(mountPath, "/data/")
	if dataIdx == -1 {
		return "", "", invalid
	}
	mount := mountPath[:dataIdx]
	secretPath := mountPath[dataIdx+len("/data/"):]
	if mount == "" || secretPath == "" {
		return "", "", invalid
	}

	return mountPath, field, nil
}

// BuildVaultClient loads the Vault connection config from the
// integrations collection (slug "vault") and returns an authenticated
// client plus the configured allowed-mount restriction (empty means
// unrestricted). Shared by VaultSecretProvider.Resolve and the Vault browse
// routes so both use identical connection/decrypt logic.
func BuildVaultClient(app core.App) (*vaultapi.Client, string, error) {
	if app == nil {
		return nil, "", errors.New("vault: app is not configured")
	}

	rec, err := app.FindFirstRecordByFilter("integrations", "slug = {:slug}", map[string]any{"slug": "vault"})
	if err != nil {
		return nil, "", errors.New("vault: backend is not configured")
	}
	if !rec.GetBool("enabled") {
		return nil, "", errors.New("vault: backend is disabled")
	}

	var cfg struct {
		Address      string `json:"address"`
		Token        string `json:"token"`
		AllowedMount string `json:"allowed_mount"`
	}
	if err := rec.UnmarshalJSONField("config", &cfg); err != nil {
		return nil, "", fmt.Errorf("vault: failed to read backend config: %w", err)
	}
	if cfg.Address == "" || cfg.Token == "" {
		return nil, "", errors.New("vault: backend config is missing address or token")
	}

	secretKey := crypto.NormalizeSecretKey(os.Getenv("SECRET_KEY"))
	tokenBytes, err := crypto.Decrypt(cfg.Token, secretKey)
	if err != nil {
		return nil, "", fmt.Errorf("vault: failed to decrypt token: %w", err)
	}

	client, err := buildVaultClientFromParts(cfg.Address, string(tokenBytes))
	if err != nil {
		return nil, "", err
	}
	return client, strings.Trim(cfg.AllowedMount, "/"), nil
}

// NewVaultClientForConfig constructs an authenticated Vault client from raw
// address/token values, without touching the integrations collection. Used
// by the test-connection route to validate unsaved form input.
func NewVaultClientForConfig(address, token string) (*vaultapi.Client, error) {
	return buildVaultClientFromParts(address, token)
}

// buildVaultClientFromParts constructs an authenticated Vault client from
// raw address/token values, without touching the integrations collection.
// Used by BuildVaultClient and the test-connection route, which validates
// unsaved form input.
func buildVaultClientFromParts(address, token string) (*vaultapi.Client, error) {
	vc := vaultapi.DefaultConfig()
	vc.Address = address
	vc.Timeout = vaultClientTimeout
	vc.HttpClient = &http.Client{Timeout: vaultClientTimeout}

	client, err := vaultapi.NewClient(vc)
	if err != nil {
		return nil, fmt.Errorf("vault: failed to create client: %w", err)
	}
	client.SetToken(token)
	return client, nil
}
