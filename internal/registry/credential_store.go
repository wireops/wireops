// Package registry loads and decrypts reusable container registry
// credentials (registry_credentials collection) and renders them as the
// docker config.json auth payload a worker uses to authenticate an image
// pull. Mirrors internal/git's credential_store.go for repository_keys.
package registry

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/distribution/reference"
	"github.com/pocketbase/pocketbase/core"
	"gopkg.in/yaml.v3"

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

// NormalizeRegistryHost strips scheme/path and canonicalizes the hostname so
// registry URLs can be compared exactly with the domains extracted from Docker
// image references. Ports remain part of the key: registry.example.com and
// registry.example.com:5000 are separate registries.
func NormalizeRegistryHost(registryURL string) string {
	trimmed := strings.TrimSpace(registryURL)
	if trimmed == "" {
		return ""
	}
	parseable := trimmed
	if !strings.Contains(parseable, "://") {
		parseable = "//" + parseable
	}
	if u, err := url.Parse(parseable); err == nil && u.Hostname() != "" {
		host := strings.ToLower(u.Hostname())
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		if port := u.Port(); port != "" {
			return net.JoinHostPort(strings.Trim(host, "[]"), port)
		}
		return host
	}
	return ""
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

// RegistryHostsFromComposeYAML returns the normalized registry hostname for
// every image in a rendered Compose file. docker compose config has already
// interpolated environment variables and the renderer has already applied
// per-service overrides, so this is the exact image set the worker will pull.
func RegistryHostsFromComposeYAML(content []byte) ([]string, error) {
	var compose struct {
		Services map[string]struct {
			Image string `yaml:"image"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(content, &compose); err != nil {
		return nil, fmt.Errorf("parse rendered compose: %w", err)
	}

	hosts := make(map[string]struct{})
	for service, definition := range compose.Services {
		image := strings.TrimSpace(definition.Image)
		if image == "" {
			continue
		}
		named, err := reference.ParseNormalizedNamed(image)
		if err != nil {
			return nil, fmt.Errorf("parse image for service %q: %w", service, err)
		}
		host := NormalizeRegistryHost(reference.Domain(named))
		if host == "" {
			return nil, fmt.Errorf("image for service %q has no registry host", service)
		}
		hosts[host] = struct{}{}
	}

	result := make([]string, 0, len(hosts))
	for host := range hosts {
		result = append(result, host)
	}
	sort.Strings(result)
	return result, nil
}

// BuildDockerAuthForHosts renders a per-command docker config.json containing
// only credentials whose exact normalized registry host appears in hosts. A
// missing credential leaves the registry anonymous; a matching credential that
// cannot be read or decrypted fails closed instead of silently dispatching an
// unauthenticated pull.
func BuildDockerAuthForHosts(app core.App, hosts []string) (authB64 string, insecureHosts []string, err error) {
	wanted := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		if normalized := NormalizeRegistryHost(host); normalized != "" {
			wanted[normalized] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return "", nil, nil
	}

	records, err := app.FindAllRecords("registry_credentials")
	if err != nil {
		return "", nil, fmt.Errorf("list registry credentials: %w", err)
	}

	entries := make(map[string]dockerConfigAuth)
	insecure := make(map[string]struct{})
	for _, record := range records {
		host := NormalizeRegistryHost(record.GetString("registry_host"))
		if host == "" {
			// registry_host was introduced after registry_credentials. Falling
			// back to registry_url keeps a partially migrated restored backup
			// from losing authentication before the next migration completes.
			host = NormalizeRegistryHost(record.GetString("registry_url"))
		}
		if _, matches := wanted[host]; !matches {
			continue
		}
		if _, duplicate := entries[host]; duplicate {
			return "", nil, fmt.Errorf("multiple credentials match registry %q", host)
		}

		credential, err := LoadCredentialByID(app, record.Id)
		if err != nil {
			return "", nil, fmt.Errorf("load credential for registry %q: %w", host, err)
		}
		_, auth, err := BuildAuthEntry(credential)
		if err != nil {
			return "", nil, fmt.Errorf("build credential for registry %q: %w", host, err)
		}
		entries[host] = dockerConfigAuth{Auth: auth}
		if credential.Insecure {
			insecure[host] = struct{}{}
		}
	}

	if len(entries) == 0 {
		return "", nil, nil
	}

	encoded, err := json.Marshal(dockerConfig{Auths: entries})
	if err != nil {
		return "", nil, fmt.Errorf("marshal docker config: %w", err)
	}
	for host := range insecure {
		insecureHosts = append(insecureHosts, host)
	}
	sort.Strings(insecureHosts)
	return base64.StdEncoding.EncodeToString(encoded), insecureHosts, nil
}
