package executor

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// prepareRegistryAuth decodes a dispatched command's RegistryAuthB64 (a
// docker config.json built server-side from the stack's registry_credential,
// see internal/registry) into a per-command temp directory. The caller
// points DOCKER_CONFIG at this directory (compose.RunOptions.DockerConfigDir)
// so the credential authenticates only this one pull, never touching the
// worker host's global ~/.docker/config.json. Returns a no-op cleanup and no
// error when authB64 is empty — most deploys have no registry credential.
func prepareRegistryAuth(stackID, commandID, authB64 string) (dir string, cleanup func(), err error) {
	noop := func() {}
	if authB64 == "" {
		return "", noop, nil
	}

	content, err := base64.StdEncoding.DecodeString(authB64)
	if err != nil {
		return "", noop, fmt.Errorf("failed to decode registry auth: %w", err)
	}

	dir = filepath.Join(stackDir, "registry-auth", filepath.Base(stackID), "cmd-"+filepath.Base(commandID))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", noop, fmt.Errorf("failed to create docker config dir: %w", err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, content, 0600); err != nil {
		_ = os.RemoveAll(dir)
		return "", noop, fmt.Errorf("failed to write docker config: %w", err)
	}

	cleanup = func() {
		if keepWorkerWorkDir() {
			log.Printf("[executor] keeping docker config dir for debugging dir=%s", dir)
			return
		}
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("[executor] failed to clean docker config dir=%s error=%v", dir, err)
		}
	}
	return dir, cleanup, nil
}

type dockerAuthConfig struct {
	Auths map[string]struct {
		Auth string `json:"auth"`
	} `json:"auths"`
}

// validateRegistryAuth does a lightweight HTTP handshake against each
// registry host in authB64 before the real `docker compose pull`/`run` is
// attempted, so a bad credential surfaces as a clear "registry auth failed"
// CommandResult.Error instead of a generic docker daemon pull failure.
// insecureHosts entries skip TLS verification and fall back to plain HTTP.
func validateRegistryAuth(authB64 string, insecureHosts []string) error {
	if authB64 == "" {
		return nil
	}
	content, err := base64.StdEncoding.DecodeString(authB64)
	if err != nil {
		return fmt.Errorf("failed to decode registry auth: %w", err)
	}
	var cfg dockerAuthConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return fmt.Errorf("failed to parse registry auth: %w", err)
	}

	insecure := make(map[string]bool, len(insecureHosts))
	for _, h := range insecureHosts {
		insecure[h] = true
	}

	for host, entry := range cfg.Auths {
		if err := checkRegistryAuth(host, entry.Auth, insecure[host]); err != nil {
			return fmt.Errorf("registry auth check failed for %s: %w", host, err)
		}
	}
	return nil
}

func checkRegistryAuth(host, authHeader string, insecure bool) error {
	client := &http.Client{Timeout: 5 * time.Second}
	if insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}

	resp, err := doRegistryCheck(client, "https://"+host+"/v2/", authHeader)
	if err != nil && insecure {
		// Self-signed HTTPS still failed to connect — some insecure
		// registries only speak plain HTTP at all.
		resp, err = doRegistryCheck(client, "http://"+host+"/v2/", authHeader)
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNotFound:
		// 404 happens on some registries when /v2/ itself isn't a routed
		// path but auth still succeeded (no 401/403); treat as reachable.
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("authentication rejected (HTTP %d)", resp.StatusCode)
	default:
		return fmt.Errorf("unexpected response (HTTP %d)", resp.StatusCode)
	}
}

func doRegistryCheck(client *http.Client, url, authHeader string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Basic "+authHeader)
	return client.Do(req)
}

type dockerRegistryConfig struct {
	IndexConfigs map[string]struct {
		Insecure bool `json:"Insecure"`
	} `json:"IndexConfigs"`
}

// preflightInsecureRegistries confirms the worker's own Docker daemon
// already trusts every host in hosts (via insecure-registries in
// daemon.json) before a pull is attempted. Unlike registry auth, "insecure"
// is a daemon-level TLS/HTTP trust setting wireops cannot inject per-command
// — it must already be configured on the worker host — so this check turns
// what would otherwise be an opaque `x509: certificate signed by unknown
// authority` pull failure into an actionable error.
func preflightInsecureRegistries(hosts []string) error {
	if len(hosts) == 0 {
		return nil
	}
	dockerPath, err := lookPathSecure("docker")
	if err != nil {
		return fmt.Errorf("failed to find docker binary: %w", err)
	}
	cmd := exec.Command(dockerPath, "info", "--format", "{{json .RegistryConfig}}")
	cmd.Env = safeEnv()
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to query docker daemon registry config: %w", err)
	}
	return checkInsecureRegistriesConfigured(hosts, out)
}

// checkInsecureRegistriesConfigured is the pure parsing/matching half of
// preflightInsecureRegistries, split out so it's testable without shelling
// out to a real `docker info`.
func checkInsecureRegistriesConfigured(hosts []string, dockerInfoRegistryConfigJSON []byte) error {
	var cfg dockerRegistryConfig
	if err := json.Unmarshal(dockerInfoRegistryConfigJSON, &cfg); err != nil {
		return fmt.Errorf("failed to parse docker daemon registry config: %w", err)
	}

	for _, host := range hosts {
		entry, ok := cfg.IndexConfigs[host]
		if !ok || !entry.Insecure {
			return fmt.Errorf("registry %s is marked insecure but is not listed in this worker's Docker daemon insecure-registries — add it to /etc/docker/daemon.json and restart Docker", host)
		}
	}
	return nil
}
