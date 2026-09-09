package routes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wireops/wireops/internal/compose"
	"github.com/wireops/wireops/internal/config"
	"github.com/wireops/wireops/internal/manifest"
)

func writeTestFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func TestListYAMLFilesMatchesEmbeddedXWireops(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "docker-compose.yml", "x-wireops:\n  version: wireops.v1\n  name: api\nservices:\n  web:\n    image: nginx\n")
	writeTestFile(t, dir, "apps/other.yaml", "services:\n  web:\n    image: nginx\n")
	writeTestFile(t, dir, "node_modules/skip.yml", "x-wireops:\n  version: wireops.v1\n  name: skipped\n")
	writeTestFile(t, dir, "notes.txt", "x-wireops: not a yaml file by extension")

	var rr routeRegistrar
	got, err := rr.listYAMLFiles(dir, func(data []byte) bool {
		return compose.IsComposeFile(data) && manifest.HasEmbeddedXWireops(data)
	})
	if err != nil {
		t.Fatalf("listYAMLFiles: %v", err)
	}
	if len(got) != 1 || got[0] != "docker-compose.yml" {
		t.Fatalf("expected only docker-compose.yml to match, got %v", got)
	}
}

func TestListYAMLFilesSkipsOversizedCandidates(t *testing.T) {
	dir := t.TempDir()
	small := "x-wireops:\n  version: wireops.v1\n  name: api\n"
	writeTestFile(t, dir, "small.yml", small)

	t.Setenv("COMPOSE_MAX_KB", "1")
	oversized := strings.Repeat("a", 2048)
	writeTestFile(t, dir, "big.yml", "x-wireops:\n  version: wireops.v1\n  name: "+oversized+"\n")

	var rr routeRegistrar
	got, err := rr.listYAMLFiles(dir, func([]byte) bool { return true })
	if err != nil {
		t.Fatalf("listYAMLFiles: %v", err)
	}
	if len(got) != 1 || got[0] != "small.yml" {
		t.Fatalf("expected only small.yml to survive the size cap, got %v (maxBytes=%d)", got, config.GetComposeMaxBytes())
	}
}

func TestListYAMLFilesReturnsNilForNoMatches(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "plain.yaml", "services:\n  web:\n    image: nginx\n")

	var rr routeRegistrar
	got, err := rr.listYAMLFiles(dir, func([]byte) bool { return false })
	if err != nil {
		t.Fatalf("listYAMLFiles: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %v", got)
	}
}
