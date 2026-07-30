package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireDocker skips when the docker CLI is unavailable. The rest of this
// package's tests are pure unit tests for exactly that reason; the limit is
// worth an integration test because it depends on how exec.Cmd propagates a
// writer error, which a fake cannot demonstrate.
func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI not available")
	}
}

// writeComposeWithServices produces a compose file whose resolved config is
// large enough to be worth bounding, without depending on a fixture.
func writeComposeWithServices(t *testing.T, dir string, count int) {
	t.Helper()
	var b strings.Builder
	b.WriteString("services:\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&b, "  svc%d:\n    image: nginx:1.25\n    environment:\n      PADDING: %q\n",
			i, strings.Repeat("x", 512))
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(b.String()), 0600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
}

func TestConfigRejectsOversizeComposeOutput(t *testing.T) {
	requireDocker(t)

	dir := t.TempDir()
	writeComposeWithServices(t, dir, 40)

	_, err := Config(context.Background(), ConfigOptions{
		WorkDir:        dir,
		MaxOutputBytes: 1024, // far below what 40 padded services resolve to
	}, true)

	if err == nil {
		t.Fatal("Config returned nil error for output past the limit")
	}
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("error = %v, want it to wrap ErrOutputTooLarge", err)
	}
	// The message has to tell an operator how to lift the limit, since a
	// legitimately large stack is the plausible cause.
	if !strings.Contains(err.Error(), "COMPOSE_MAX_KB") {
		t.Errorf("error = %q, want it to name the env var that raises the limit", err)
	}
}

func TestConfigAcceptsOutputWithinTheLimit(t *testing.T) {
	requireDocker(t)

	dir := t.TempDir()
	writeComposeWithServices(t, dir, 2)

	out, err := Config(context.Background(), ConfigOptions{
		WorkDir:        dir,
		MaxOutputBytes: 1 << 20,
	}, true)
	if err != nil {
		t.Fatalf("Config failed: %v", err)
	}

	cfg, err := ParseConfigJSON(out)
	if err != nil {
		t.Fatalf("ParseConfigJSON failed: %v", err)
	}
	services, ok := cfg["services"].(map[string]interface{})
	if !ok || len(services) != 2 {
		t.Fatalf("services = %v, want both services present and intact", cfg["services"])
	}
}

// TestConfigDefaultsToTheConfiguredLimit pins that callers passing a zero
// MaxOutputBytes get the COMPOSE_MAX_KB-derived bound rather than no bound.
func TestConfigDefaultsToTheConfiguredLimit(t *testing.T) {
	requireDocker(t)
	t.Setenv("COMPOSE_MAX_KB", "1")

	dir := t.TempDir()
	writeComposeWithServices(t, dir, 40)

	_, err := Config(context.Background(), ConfigOptions{WorkDir: dir}, true)
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("error = %v, want ErrOutputTooLarge from the configured default", err)
	}
}
