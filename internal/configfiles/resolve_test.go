package configfiles

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, rel string, content []byte) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, content, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func byRelPath(files []ResolvedFile) map[string]ResolvedFile {
	m := make(map[string]ResolvedFile, len(files))
	for _, f := range files {
		key := f.Name
		if f.RelPath != "" {
			key = f.Name + "/" + f.RelPath
		}
		m[key] = f
	}
	return m
}

func TestResolveHappyPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "conf/nginx.conf", []byte("server {}\n"))
	writeFile(t, dir, "settings.json", []byte(`{"a":1}`))

	resolved, err := Resolve(dir, []Source{
		{Name: "nginx-conf", Path: "conf/nginx.conf"},
		{Name: "settings", Path: "settings.json"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 resolved files, got %d", len(resolved))
	}

	byName := byRelPath(resolved)
	nginx, ok := byName["nginx-conf"]
	if !ok {
		t.Fatalf("missing nginx-conf entry")
	}
	if string(nginx.Content) != "server {}\n" {
		t.Errorf("unexpected content: %q", nginx.Content)
	}
	if nginx.SourcePath != "conf/nginx.conf" {
		t.Errorf("unexpected source path: %q", nginx.SourcePath)
	}
	if nginx.RelPath != "" {
		t.Errorf("expected empty RelPath for a plain file source, got %q", nginx.RelPath)
	}
	if nginx.Checksum == "" {
		t.Errorf("expected non-empty checksum")
	}

	// Checksum must be deterministic for identical content.
	resolvedAgain, err := Resolve(dir, []Source{{Name: "nginx-conf", Path: "conf/nginx.conf"}})
	if err != nil {
		t.Fatalf("Resolve (second) returned error: %v", err)
	}
	if resolvedAgain[0].Checksum != nginx.Checksum {
		t.Errorf("checksum not deterministic: %q vs %q", resolvedAgain[0].Checksum, nginx.Checksum)
	}
}

func TestResolveDirectoryExpandsRecursively(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "confd/a.conf", []byte("a"))
	writeFile(t, dir, "confd/nested/b.conf", []byte("b"))

	resolved, err := Resolve(dir, []Source{{Name: "confd", Path: "confd"}})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 files from directory expansion, got %d", len(resolved))
	}

	byName := byRelPath(resolved)
	a, ok := byName["confd/a.conf"]
	if !ok {
		t.Fatalf("missing confd/a.conf, got keys: %v", keysOf(byName))
	}
	if string(a.Content) != "a" || a.RelPath != "a.conf" {
		t.Errorf("unexpected a.conf entry: content=%q relpath=%q", a.Content, a.RelPath)
	}

	b, ok := byName["confd/nested/b.conf"]
	if !ok {
		t.Fatalf("missing confd/nested/b.conf, got keys: %v", keysOf(byName))
	}
	if string(b.Content) != "b" || b.RelPath != "nested/b.conf" {
		t.Errorf("unexpected nested/b.conf entry: content=%q relpath=%q", b.Content, b.RelPath)
	}
}

func keysOf(m map[string]ResolvedFile) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestResolveEmptySources(t *testing.T) {
	dir := t.TempDir()
	resolved, err := Resolve(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(resolved))
	}
}

func TestResolveMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := Resolve(dir, []Source{{Name: "missing", Path: "does/not/exist.conf"}})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should reference config name: %v", err)
	}
}

func TestResolvePathTraversal(t *testing.T) {
	dir := t.TempDir()
	_, err := Resolve(dir, []Source{{Name: "evil", Path: "../../../etc/passwd"}})
	if err == nil {
		t.Fatal("expected error for traversal path")
	}
}

func TestResolveAbsolutePathRejected(t *testing.T) {
	dir := t.TempDir()
	_, err := Resolve(dir, []Source{{Name: "evil", Path: "/etc/passwd"}})
	if err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestResolvePerFileSizeLimit(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, MaxFileBytes+1)
	writeFile(t, dir, "big.bin", big)

	_, err := Resolve(dir, []Source{{Name: "big", Path: "big.bin"}})
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected size limit error, got: %v", err)
	}
}

func TestResolvePerFileSizeLimitInsideDirectory(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, MaxFileBytes+1)
	writeFile(t, dir, "confd/big.bin", big)

	_, err := Resolve(dir, []Source{{Name: "confd", Path: "confd"}})
	if err == nil {
		t.Fatal("expected error for oversized file inside directory")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected size limit error, got: %v", err)
	}
}

func TestResolveTotalSizeLimit(t *testing.T) {
	dir := t.TempDir()
	chunk := make([]byte, MaxFileBytes)
	writeFile(t, dir, "a.bin", chunk)
	writeFile(t, dir, "b.bin", chunk)
	writeFile(t, dir, "c.bin", chunk)
	writeFile(t, dir, "d.bin", chunk)
	writeFile(t, dir, "e.bin", chunk)
	writeFile(t, dir, "f.bin", chunk)

	sources := []Source{
		{Name: "a", Path: "a.bin"},
		{Name: "b", Path: "b.bin"},
		{Name: "c", Path: "c.bin"},
		{Name: "d", Path: "d.bin"},
		{Name: "e", Path: "e.bin"},
		{Name: "f", Path: "f.bin"},
	}
	_, err := Resolve(dir, sources)
	if err == nil {
		t.Fatal("expected error for total size limit")
	}
	if !strings.Contains(err.Error(), "total") {
		t.Errorf("expected total size limit error, got: %v", err)
	}
}

func TestResolveChecksumChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.conf", []byte("v1"))
	resolvedV1, err := Resolve(dir, []Source{{Name: "a", Path: "a.conf"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	writeFile(t, dir, "a.conf", []byte("v2"))
	resolvedV2, err := Resolve(dir, []Source{{Name: "a", Path: "a.conf"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resolvedV1[0].Checksum == resolvedV2[0].Checksum {
		t.Error("expected checksum to change when content changes")
	}
}
