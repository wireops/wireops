package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ConfigOptions represents options for `docker compose config`
type ConfigOptions struct {
	WorkDir     string
	ComposeFile string
	EnvVars     []string
}

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

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
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
