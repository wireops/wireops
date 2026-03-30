package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type RunOptions struct {
	WorkDir     string
	ComposeFile string
}

func RunUp(ctx context.Context, opts RunOptions) (string, error) {
	composeFile := opts.ComposeFile
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}

	// Check existence using full path (relative to process CWD)
	fullPath := filepath.Join(opts.WorkDir, composeFile)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		altFile := "compose.yml"
		altPath := filepath.Join(opts.WorkDir, altFile)
		if _, err2 := os.Stat(altPath); os.IsNotExist(err2) {
			return "", fmt.Errorf("compose file not found in %s", opts.WorkDir)
		}
		composeFile = altFile
	}

	// Use just the filename for -f since cmd.Dir is set to WorkDir
	cmd := exec.CommandContext(ctx, "docker", "compose",
		"-f", composeFile,
		"up", "-d", "--remove-orphans",
	)
	cmd.Dir = opts.WorkDir
	cmd.Env = os.Environ()

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("docker compose up failed: %w", err)
	}

	return buf.String(), nil
}

type ForceUpOptions struct {
	RunOptions
	RecreateContainers bool
	RecreateVolumes    bool
	RecreateNetworks   bool
}

func RunForceUp(ctx context.Context, opts ForceUpOptions) (string, error) {
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

	env := os.Environ()

	var allOutput strings.Builder

	// If recreating networks, we need to bring everything down first
	if opts.RecreateNetworks {
		downArgs := []string{"compose", "-f", composeFile, "down"}
		if opts.RecreateVolumes {
			downArgs = append(downArgs, "-v")
		}
		downArgs = append(downArgs, "--remove-orphans")
		downCmd := exec.CommandContext(ctx, "docker", downArgs...)
		downCmd.Dir = opts.WorkDir
		downCmd.Env = env
		var downBuf bytes.Buffer
		downCmd.Stdout = &downBuf
		downCmd.Stderr = &downBuf
		if err := downCmd.Run(); err != nil {
			return downBuf.String(), fmt.Errorf("docker compose down failed: %w", err)
		}
		allOutput.WriteString(downBuf.String())
		allOutput.WriteString("\n--- recreating ---\n")
	}

	upArgs := []string{"compose", "-f", composeFile, "up", "-d", "--remove-orphans"}
	if opts.RecreateContainers {
		upArgs = append(upArgs, "--force-recreate")
	}
	if opts.RecreateVolumes && !opts.RecreateNetworks {
		upArgs = append(upArgs, "--renew-anon-volumes")
	}

	cmd := exec.CommandContext(ctx, "docker", upArgs...)
	cmd.Dir = opts.WorkDir
	cmd.Env = env

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		allOutput.WriteString(buf.String())
		return allOutput.String(), fmt.Errorf("docker compose up failed: %w", err)
	}

	allOutput.WriteString(buf.String())
	return allOutput.String(), nil
}

func RunDown(ctx context.Context, opts RunOptions) (string, error) {
	composeFile := opts.ComposeFile
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}

	cmd := exec.CommandContext(ctx, "docker", "compose",
		"-f", composeFile,
		"down",
	)
	cmd.Dir = opts.WorkDir
	cmd.Env = os.Environ()

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("docker compose down failed: %w", err)
	}

	return buf.String(), nil
}

func RunDownPurge(ctx context.Context, opts RunOptions) (string, error) {
	composeFile := opts.ComposeFile
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}

	cmd := exec.CommandContext(ctx, "docker", "compose",
		"-f", composeFile,
		"down", "-v", "--remove-orphans",
	)
	cmd.Dir = opts.WorkDir
	cmd.Env = os.Environ()

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("docker compose down --purge failed: %w", err)
	}

	return buf.String(), nil
}

func ProjectName(workDir string) string {
	base := filepath.Base(workDir)
	return strings.ToLower(strings.ReplaceAll(base, " ", "_"))
}

// RunPs runs `docker compose ps --format json` and returns the names of services
// (in any state) that have containers for the given compose project.
// A nil/empty slice means no containers currently exist.
func RunPs(ctx context.Context, opts RunOptions) ([]string, error) {
	composeFile := opts.ComposeFile
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}

	cmd := exec.CommandContext(ctx, "docker", "compose",
		"-f", composeFile,
		"ps", "--format", "json", "--all",
	)
	cmd.Dir = opts.WorkDir
	cmd.Env = os.Environ()

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	// `docker compose ps` exits non-zero when there are no containers on some
	// versions — treat that as "empty, not an error".
	_ = cmd.Run()
	output := strings.TrimSpace(buf.String())
	if output == "" || output == "[]" || output == "null" {
		return nil, nil
	}

	// docker compose ps --format json outputs either a JSON array of objects
	// or one JSON object per line (NDJSON), depending on the Compose version.
	// We just look for "Service" fields to collect unique service names.
	type psEntry struct {
		Service string `json:"Service"`
		Name    string `json:"Name"` // container name, fallback
	}

	var services []string
	seen := make(map[string]bool)

	// Try JSON array first
	var entries []psEntry
	if err := json.Unmarshal([]byte(output), &entries); err == nil {
		for _, e := range entries {
			key := e.Service
			if key == "" {
				key = e.Name
			}
			if key != "" && !seen[key] {
				seen[key] = true
				services = append(services, key)
			}
		}
		return services, nil
	}

	// Fall back to NDJSON (one object per line)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e psEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		key := e.Service
		if key == "" {
			key = e.Name
		}
		if key != "" && !seen[key] {
			seen[key] = true
			services = append(services, key)
		}
	}
	return services, nil
}
