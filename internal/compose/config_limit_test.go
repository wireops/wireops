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

// writeExpandingCompose produces a *small* compose file whose resolved config
// is far larger, using a YAML anchor aliased into many services. It is what
// makes the output limit testable independently of the source limit — and why
// that limit is load-bearing rather than redundant: a compact file can expand
// by more than an order of magnitude.
func writeExpandingCompose(t *testing.T, dir string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("x-env: &env\n")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "  KEY%d: %q\n", i, strings.Repeat("v", 80))
	}
	b.WriteString("services:\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "  svc%d:\n    image: nginx:1.25\n    environment: *env\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(b.String()), 0600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
}

func TestConfigRejectsOversizeComposeSource(t *testing.T) {
	// No requireDocker: ReadFile rejects the oversize source before Config
	// reaches the docker subprocess, so this case never touches Docker.
	dir := t.TempDir()
	writeComposeWithServices(t, dir, 40)

	_, err := Config(context.Background(), ConfigOptions{
		WorkDir:        dir,
		MaxOutputBytes: 1024, // far below what 40 padded services occupy on disk
	}, true)

	if err == nil {
		t.Fatal("Config returned nil error for a source past the limit")
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

// TestConfigRejectsOversizeComposeOutput covers the limit on what docker
// *emits*, which the source check above cannot stand in for: the file here is
// a few KB and resolves to well over a hundred.
func TestConfigRejectsOversizeComposeOutput(t *testing.T) {
	requireDocker(t)

	dir := t.TempDir()
	writeExpandingCompose(t, dir)

	info, err := os.Stat(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Above the source, below the expansion — so only the output limit can
	// trip, proving the two ceilings are enforced independently.
	limit := info.Size() + 4096

	_, err = Config(context.Background(), ConfigOptions{
		WorkDir:        dir,
		MaxOutputBytes: limit,
	}, true)

	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("error = %v, want ErrOutputTooLarge from the resolved output", err)
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

// TestReadFileRefusesASymlinkedComposeFile is the vector that the string-level
// path validation cannot see. A repository can ship a compose file that is a
// symlink — git preserves mode 120000 on checkout — and its name still passes
// every extension and traversal check, because the check constrains the link's
// name and not its target. Since ReadFile's bytes are returned to API callers,
// following it would be an arbitrary file read.
func TestReadFileRefusesASymlinkedComposeFile(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	secret := filepath.Join(base, "outside-secret.yml")
	if err := os.WriteFile(secret, []byte("SECRET=leaked"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(repo, "docker-compose.yml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	data, _, err := ReadFile(repo, repo, "docker-compose.yml", 0)
	if err == nil {
		t.Fatalf("ReadFile followed the symlink and returned %q, want it rejected", data)
	}
	if strings.Contains(string(data), "leaked") {
		t.Fatal("ReadFile leaked the contents of a file outside the repository")
	}
}

// TestReadFileRefusesAComposePathSymlinkedOutOfTheRoot covers the directory
// form: compose_path names a directory inside the repo that is itself a link.
func TestReadFileRefusesAComposePathSymlinkedOutOfTheRoot(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{repo, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "docker-compose.yml"), []byte("SECRET=leaked"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// Root is the repo checkout; workDir is repo/escape, which is what a
	// compose_path of "escape" resolves to in the lint route.
	data, _, err := ReadFile(repo, filepath.Join(repo, "escape"), "docker-compose.yml", 0)
	if err == nil {
		t.Fatalf("ReadFile followed a symlinked directory and returned %q, want it rejected", data)
	}
}

// TestReadFileReadsARegularFileInsideTheRepo is the positive counterpart —
// containment must not break the ordinary case.
func TestReadFileReadsARegularFileInsideTheRepo(t *testing.T) {
	repo := t.TempDir()
	want := "services:\n  web:\n    image: nginx:1.25\n"
	if err := os.WriteFile(filepath.Join(repo, "docker-compose.yml"), []byte(want), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, name, err := ReadFile(repo, repo, "docker-compose.yml", 0)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(data) != want {
		t.Errorf("content = %q, want %q", data, want)
	}
	if name != "docker-compose.yml" {
		t.Errorf("filename = %q, want docker-compose.yml", name)
	}
}

// TestResolveFileFallsBackToComposeYml keeps the documented fallback working
// now that resolution goes through the containment check.
func TestResolveFileFallsBackToComposeYml(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "compose.yml"), []byte("services: {}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := ResolveFile(repo, repo, "docker-compose.yml")
	if err != nil {
		t.Fatalf("ResolveFile failed: %v", err)
	}
	if got != "compose.yml" {
		t.Errorf("ResolveFile = %q, want compose.yml", got)
	}
}
