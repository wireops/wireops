// Package registry loads and decrypts reusable container registry
// credentials (registry_credentials collection) and renders them as the
// docker config.json auth payload a worker uses to authenticate an image
// pull. Mirrors internal/git's credential_store.go for repository_keys.
package registry

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/interpolation"
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

const (
	dockerHubRegistryHost  = "docker.io"
	dockerHubAuthConfigKey = "https://index.docker.io/v1/"
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
		// Docker accepts both names for Docker Hub, but credentials must use
		// one canonical match key. Docker image references use docker.io.
		if host == "index.docker.io" {
			host = dockerHubRegistryHost
		}
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

// dockerAuthConfigKey is the config.json key Docker itself requires for a
// Docker Hub credential. Matching always uses NormalizeRegistryHost; this is
// only the wire representation sent to Docker.
func dockerAuthConfigKey(host string) string {
	if NormalizeRegistryHost(host) == dockerHubRegistryHost {
		return dockerHubAuthConfigKey
	}
	return NormalizeRegistryHost(host)
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

	cfg := dockerConfig{Auths: map[string]dockerConfigAuth{dockerAuthConfigKey(host): {Auth: auth}}}
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
// every image in a rendered Compose file. It is kept for callers whose Compose
// file has no external environment. Deploy paths should use
// RegistryHostsFromComposeYAMLWithEnv so image interpolation matches the .env
// file sent to the worker.
func RegistryHostsFromComposeYAML(content []byte) ([]string, error) {
	return registryHostsFromComposeYAML(content, nil)
}

// RegistryHostsFromComposeYAMLWithEnv returns registry hosts after applying
// the Docker Compose interpolation rules to the exact .env values dispatched
// with a command. This matters because rendered revisions deliberately retain
// placeholders (for example ${REGISTRY}/api) until the worker runs Compose.
func RegistryHostsFromComposeYAMLWithEnv(content []byte, envVars []string) ([]string, error) {
	values := make(map[string]string, len(envVars))
	for _, entry := range envVars {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid compose environment entry %q", entry)
		}
		// The generated .env file preserves order, and Docker Compose uses the
		// last definition when a key appears more than once.
		values[key] = value
	}
	return registryHostsFromComposeYAML(content, func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
}

func registryHostsFromComposeYAML(content []byte, lookup interpolation.LookupValue) ([]string, error) {
	var compose map[string]interface{}
	if err := yaml.Unmarshal(content, &compose); err != nil {
		return nil, fmt.Errorf("parse rendered compose: %w", err)
	}
	if lookup != nil {
		var err error
		compose, err = interpolation.Interpolate(compose, interpolation.Options{LookupValue: lookup})
		if err != nil {
			return nil, fmt.Errorf("interpolate rendered compose: %w", err)
		}
	}

	services, ok := compose["services"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("parse rendered compose: services must be a mapping")
	}

	hosts := make(map[string]struct{})
	for service, rawDefinition := range services {
		definition, ok := rawDefinition.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("parse rendered compose: service %q must be a mapping", service)
		}
		rawImage, exists := definition["image"]
		if !exists || rawImage == nil {
			continue
		}
		imageValue, ok := rawImage.(string)
		if !ok {
			return nil, fmt.Errorf("parse image for service %q: image must be a string", service)
		}
		image := strings.TrimSpace(imageValue)
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
	// Small, focused unit-test applications and a restored pre-registry
	// database may not have the optional collection yet. Production startup
	// applies the migrations before reconciliation, but in either of these
	// cases there cannot be a matching credential, so preserve anonymous pulls.
	// Do not swallow a real storage failure once the collection exists.
	if _, err := app.FindCollectionByNameOrId("registry_credentials"); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, nil
		}
		return "", nil, fmt.Errorf("find registry credentials collection: %w", err)
	}

	records, err := app.FindAllRecords("registry_credentials")
	if err != nil {
		return "", nil, fmt.Errorf("list registry credentials: %w", err)
	}

	entries := make(map[string]dockerConfigAuth)
	matched := make(map[string]struct{})
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
		if _, duplicate := matched[host]; duplicate {
			return "", nil, fmt.Errorf("multiple credentials match registry %q", host)
		}

		credential, err := LoadCredentialByID(app, record.Id)
		if err != nil {
			return "", nil, fmt.Errorf("load credential for registry %q: %w", host, err)
		}
		credentialHost, auth, err := BuildAuthEntry(credential)
		if err != nil {
			return "", nil, fmt.Errorf("build credential for registry %q: %w", host, err)
		}
		if credentialHost != host {
			return "", nil, fmt.Errorf("credential for registry %q has mismatched registry_url host %q", host, credentialHost)
		}
		entries[dockerAuthConfigKey(host)] = dockerConfigAuth{Auth: auth}
		matched[host] = struct{}{}
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
