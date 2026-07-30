package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/wireops/wireops/internal/config"
)

// ConfigOptions represents options for `docker compose config`
type ConfigOptions struct {
	WorkDir     string
	ComposeFile string
	EnvVars     []string
	// MaxOutputBytes caps the resolved config the server will buffer and
	// parse. Zero means "use config.GetComposeMaxBytes()", which is what
	// every caller wants — set it explicitly only in tests.
	MaxOutputBytes int64
}

// maxStderrBytes caps the diagnostic output kept from a failed run. It is
// small on purpose: stderr only ever ends up inside an error message, so
// there is no reason to let a chatty or hostile subprocess push megabytes
// through it.
const maxStderrBytes = 64 << 10

// Config runs `docker compose config` and returns the output, optionally formatted as JSON.
func Config(ctx context.Context, opts ConfigOptions, formatJSON bool) (string, error) {
	composeFile := opts.ComposeFile
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}

	fullPath := filepath.Join(opts.WorkDir, composeFile)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		altFile := "compose.yml"
		altPath := filepath.Join(opts.WorkDir, altFile)
		if _, err2 := os.Stat(altPath); os.IsNotExist(err2) {
			return "", fmt.Errorf("compose file not found in %s", opts.WorkDir)
		}
		composeFile = altFile
	}

	args := []string{"compose", "-f", composeFile, "config", "--no-interpolate"}
	if formatJSON {
		args = append(args, "--format", "json")
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = opts.WorkDir

	env := os.Environ()
	if len(opts.EnvVars) > 0 {
		env = append(env, opts.EnvVars...)
	}
	cmd.Env = env

	maxOutput := opts.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = config.GetComposeMaxBytes()
	}

	stdout := newLimitedBuffer("stdout", maxOutput)
	stderr := newLimitedBuffer("stderr", maxStderrBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		// A limit breach surfaces as the copy error from cmd.Run, but it is a
		// property of the compose file rather than a docker failure — report
		// it as itself so callers can tell the two apart and the user gets an
		// actionable message instead of a broken-pipe error.
		if errors.Is(err, ErrOutputTooLarge) {
			return "", fmt.Errorf("compose config for %s is larger than the %d byte limit (raise COMPOSE_MAX_KB if this is legitimate): %w",
				composeFile, maxOutput, ErrOutputTooLarge)
		}
		return "", fmt.Errorf("docker compose config failed: %w\nstderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// ParseConfigJSON parses the output of `docker compose config --format json` into a map.
func ParseConfigJSON(output string) (map[string]interface{}, error) {
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(output), &config); err != nil {
		return nil, fmt.Errorf("failed to parse compose config JSON: %w", err)
	}
	return config, nil
}

// IsComposeFile reports whether data looks like a Docker Compose file
// by requiring a non-empty "services" map at the top level.
func IsComposeFile(data []byte) bool {
	var doc struct {
		Services map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false
	}
	return len(doc.Services) > 0
}

// ExpectedServiceNames extracts the top-level service names from a rendered
// compose file, used by post-deploy checks to know which containers should
// exist after `docker compose up`.
func ExpectedServiceNames(data []byte) ([]string, error) {
	var doc struct {
		Services map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse compose services: %w", err)
	}
	names := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		names = append(names, name)
	}
	return names, nil
}
