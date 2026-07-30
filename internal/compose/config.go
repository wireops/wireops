package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	// Root is the directory the compose file must resolve inside once
	// symlinks are followed. Empty means WorkDir, so a compose file can
	// never escape its own directory even when a caller forgets to set it.
	// Callers whose WorkDir is itself built from request input should set
	// this to the repository workspace.
	Root string
}

// maxStderrBytes caps the diagnostic output kept from a failed run. It is
// small on purpose: stderr only ever ends up inside an error message, so
// there is no reason to let a chatty or hostile subprocess push megabytes
// through it.
const maxStderrBytes = 64 << 10

// containmentRoot returns the directory a compose file must resolve inside.
//
// Defaulting to workDir rather than to "unchecked" is deliberate: a caller
// that forgets to set Root still gets a file that cannot escape its own
// working directory via a symlink. Callers whose workDir is itself derived
// from request input (the lint route's compose_path) pass the repository
// workspace instead, so the directory cannot escape either.
func containmentRoot(root, workDir string) string {
	if root == "" {
		return workDir
	}
	return root
}

// ResolveFile returns the compose filename Config would actually use inside
// workDir, applying the same "docker-compose.yml, else compose.yml" fallback.
//
// Candidates are opened through os.Root (see openContained), so a repository
// whose docker-compose.yml is a symlink pointing outside root is rejected
// rather than followed. Repository content is attacker-influenced — git
// preserves symlinks on checkout — and safepath's ".yml" extension check
// constrains the link's *name*, never its target.
//
// Exported so that callers wanting to show the source alongside the resolved
// config — the create-stack lint preview — read the very file that was linted,
// rather than guessing and risking showing one file while reporting on another.
func ResolveFile(root, workDir, composeFile string) (string, error) {
	if composeFile == "" {
		composeFile = "docker-compose.yml"
	}

	for _, candidate := range []string{composeFile, "compose.yml"} {
		f, _, err := openContained(root, workDir, candidate)
		if err != nil {
			// Either it escapes the root, is not a regular file, or does not
			// exist. Try the fallback name.
			continue
		}
		_ = f.Close()
		return candidate, nil
	}
	return "", fmt.Errorf("compose file not found in %s", workDir)
}

// openContained opens workDir/name for reading, refusing to leave root.
//
// It goes through os.Root rather than resolving a path and re-opening it:
// os.Root performs the traversal itself and refuses symlinks that escape,
// which closes the gap between checking a path and opening it. A check
// followed by a separate open is a race — the repository checkout is shared
// between concurrent requests and rewritten by every git fetch, so a path
// validated a moment ago can be a symlink by the time it is read.
//
// The caller gets the open file, so the size check and the read both act on
// the descriptor rather than re-consulting the path.
func openContained(root, workDir, name string) (*os.File, os.FileInfo, error) {
	rootDir := containmentRoot(root, workDir)

	rel, err := filepath.Rel(rootDir, filepath.Join(workDir, name))
	if err != nil {
		return nil, nil, fmt.Errorf("compose file %q is not reachable from the repository: %w", name, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, nil, fmt.Errorf("compose file %q resolves outside the repository", name)
	}

	r, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open repository directory: %w", err)
	}
	defer r.Close()

	f, err := r.Open(rel)
	if err != nil {
		return nil, nil, err
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	// A directory, fifo or device node would make the read below misbehave
	// (block forever, or never terminate).
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, fmt.Errorf("compose file %q is not a regular file", name)
	}
	return f, info, nil
}

// ReadFile returns the raw bytes of the compose file Config would use, bounded
// by the same limit that applies to a resolved config so a large file cannot
// be read into memory just because it is being previewed.
//
// The file is opened through os.Root and then stat'd and read from that one
// descriptor — this content is returned to API callers, so following a symlink
// out of the repository would be an arbitrary file read, and re-opening a
// path that was checked a moment earlier would leave a race in its place.
func ReadFile(root, workDir, composeFile string, maxBytes int64) ([]byte, string, error) {
	resolved, err := ResolveFile(root, workDir, composeFile)
	if err != nil {
		return nil, "", err
	}
	if maxBytes <= 0 {
		maxBytes = config.GetComposeMaxBytes()
	}

	f, info, err := openContained(root, workDir, resolved)
	if err != nil {
		return nil, "", fmt.Errorf("compose file not readable: %w", err)
	}
	defer f.Close()

	if info.Size() > maxBytes {
		return nil, "", fmt.Errorf("compose file %s is %d bytes, over the %d byte limit (raise COMPOSE_MAX_KB if this is legitimate): %w",
			resolved, info.Size(), maxBytes, ErrOutputTooLarge)
	}

	// Read from the descriptor with its own ceiling rather than trusting the
	// size reported above: the file can grow between the stat and the read,
	// and on some filesystems the reported size is a hint.
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("compose file not readable: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", fmt.Errorf("compose file %s exceeds the %d byte limit: %w", resolved, maxBytes, ErrOutputTooLarge)
	}
	return data, resolved, nil
}

// Config runs `docker compose config` and returns the output, optionally formatted as JSON.
func Config(ctx context.Context, opts ConfigOptions, formatJSON bool) (string, error) {
	composeFile, err := ResolveFile(opts.Root, opts.WorkDir, opts.ComposeFile)
	if err != nil {
		return "", err
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
