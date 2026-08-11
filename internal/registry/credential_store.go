// Package registry loads and decrypts reusable container registry
// credentials (registry_credentials collection) and renders them as the
// docker config.json auth payload a worker uses to authenticate an image
// pull. Mirrors internal/git's credential_store.go for repository_keys.
package registry

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/constants"
	"github.com/wireops/wireops/internal/crypto"
)

// AuthType mirrors the registry_credentials.auth_type select values. The
// value only changes how the UI labels/validates the "password" field —
// on the wire it is always rendered as HTTP Basic auth (username:password),
// which is also how Docker registries expect a GCP service-account JSON
// key to be sent (username "_json_key", password = the JSON key content).
type AuthType string

const (
	AuthTypeBasic             AuthType = "basic"
	AuthTypeToken             AuthType = "token"
	AuthTypeGCPServiceAccount AuthType = "gcp_service_account"
)

// Credential is a decrypted registry_credentials record.
type Credential struct {
	ID          string
	Name        string
	RegistryURL string
	AuthType    AuthType
	Username    string
	Password    string
	Insecure    bool
}

// LoadCredentialByID loads and decrypts a reusable registry credential.
func LoadCredentialByID(app core.App, id string) (*Credential, error) {
	record, err := app.FindRecordById("registry_credentials", id)
	if err != nil {
		return nil, fmt.Errorf("find registry credential: %w", err)
	}

	secretKey := crypto.NormalizeSecretKey(os.Getenv(constants.EnvSecretKey))
	if len(secretKey) != 32 {
		return nil, fmt.Errorf("SECRET_KEY must be exactly 32 bytes")
	}

	password := ""
	if raw := record.GetString("password"); raw != "" {
		plain, err := crypto.Decrypt(raw, secretKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt password: %w", err)
		}
		password = string(plain)
	}

	return &Credential{
		ID:          record.Id,
		Name:        record.GetString("name"),
		RegistryURL: record.GetString("registry_url"),
		AuthType:    AuthType(record.GetString("auth_type")),
		Username:    record.GetString("username"),
		Password:    password,
		Insecure:    record.GetBool("insecure"),
	}, nil
}

// NormalizeRegistryHost strips scheme/path so the result matches the host
// key docker's config.json and the registry HTTP API expect, e.g.
// "https://ghcr.io/" -> "ghcr.io", "registry.example.com:5000" unchanged,
// "registry.example.com/v2" -> "registry.example.com" (schemeless input with
// a path: prepend "//" so url.Parse treats it as an authority, not a bare
// relative path — otherwise the whole string, path included, would leak
// into the host key and break auth lookups keyed by host).
func NormalizeRegistryHost(registryURL string) string {
	trimmed := strings.TrimSpace(registryURL)
	if trimmed == "" {
		return ""
	}
	parseable := trimmed
	if !strings.Contains(parseable, "://") {
		parseable = "//" + parseable
	}
	if u, err := url.Parse(parseable); err == nil && u.Host != "" {
		return u.Host
	}
	return strings.TrimRight(trimmed, "/")
}

// dockerConfig mirrors the subset of ~/.docker/config.json wireops renders
// per-deploy, so the worker can authenticate a pull without touching the
// host's global docker credential store.
type dockerConfig struct {
	Auths map[string]dockerConfigAuth `json:"auths"`
}

type dockerConfigAuth struct {
	Auth string `json:"auth"`
}

// BuildAuthEntry renders a single credential as a base64 "user:pass" auth
// string plus its normalized host, for embedding in a docker config.json.
func BuildAuthEntry(cred *Credential) (host, auth string, err error) {
	host = NormalizeRegistryHost(cred.RegistryURL)
	if host == "" {
		return "", "", fmt.Errorf("registry credential %s has no registry_url", cred.ID)
	}
	raw := fmt.Sprintf("%s:%s", cred.Username, cred.Password)
	return host, base64.StdEncoding.EncodeToString([]byte(raw)), nil
}

// BuildDockerAuth loads and decrypts the given credential and renders it as
// a base64-encoded docker config.json (protocol.DeployCommand.RegistryAuthB64),
// along with the registry host if the credential is marked insecure (nil
// otherwise, for protocol.DeployCommand.InsecureRegistries).
func BuildDockerAuth(app core.App, credentialID string) (authB64 string, insecureHosts []string, err error) {
	if credentialID == "" {
		return "", nil, nil
	}
	cred, err := LoadCredentialByID(app, credentialID)
	if err != nil {
		return "", nil, err
	}

	host, auth, err := BuildAuthEntry(cred)
	if err != nil {
		return "", nil, err
	}

	cfg := dockerConfig{Auths: map[string]dockerConfigAuth{host: {Auth: auth}}}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		return "", nil, fmt.Errorf("marshal docker config: %w", err)
	}

	if cred.Insecure {
		insecureHosts = []string{host}
	}
	return base64.StdEncoding.EncodeToString(encoded), insecureHosts, nil
}
