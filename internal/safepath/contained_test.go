package safepath_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wireops/wireops/internal/safepath"
)

func TestResolveContainedAllowsPathsInsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "svc"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "svc", "docker-compose.yml"), []byte("services: {}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cases := []string{
		filepath.Join(root, "svc"),
		filepath.Join(root, "svc", "docker-compose.yml"),
		// Not created — a path that does not exist yet is still allowed as
		// long as it would land inside the root.
		filepath.Join(root, "svc", "compose.yml"),
		filepath.Join(root, "does", "not", "exist", "yet.yml"),
	}

	for _, candidate := range cases {
		t.Run(filepath.Base(candidate), func(t *testing.T) {
			got, err := safepath.ResolveContained(root, candidate)
			if err != nil {
				t.Fatalf("ResolveContained(%q) = %v, want it allowed", candidate, err)
			}
			if !strings.HasPrefix(got, root) {
				// EvalSymlinks may rewrite the temp dir prefix (e.g. /var ->
				// /private/var), so compare against the resolved root.
				resolvedRoot, _ := filepath.EvalSymlinks(root)
				if !strings.HasPrefix(got, resolvedRoot) {
					t.Errorf("resolved %q, want it under %q", got, resolvedRoot)
				}
			}
		})
	}
}

// TestResolveContainedBlocksSymlinkedFile is the case the string validators
// cannot catch: git preserves symlinks on checkout, so a repository can ship a
// docker-compose.yml that is a link to somewhere else entirely. The name
// passes every extension and traversal check; only resolving it catches this.
func TestResolveContainedBlocksSymlinkedFile(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	secret := filepath.Join(base, "outside-secret.yml")
	if err := os.WriteFile(secret, []byte("SECRET=leaked"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	link := filepath.Join(root, "docker-compose.yml")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := safepath.ResolveContained(root, link); err == nil {
		t.Fatal("ResolveContained followed a symlink out of the root, want it rejected")
	}
}

func TestResolveContainedBlocksSymlinkedDirectory(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "docker-compose.yml"), []byte("services: {}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// A symlinked *directory* inside the repo, reached via a compose_path
	// that passes ValidateComposePath since it contains no "..".
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := safepath.ResolveContained(root, filepath.Join(root, "escape", "docker-compose.yml")); err == nil {
		t.Fatal("ResolveContained followed a symlinked directory out of the root, want it rejected")
	}
}

func TestResolveContainedBlocksTraversalAndAbsolutePaths(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cases := []struct {
		name      string
		candidate string
	}{
		{"parent traversal", filepath.Join(root, "..", "elsewhere.yml")},
		{"absolute elsewhere", "/etc/passwd"},
		{"root's own parent", base},
		// A traversal hidden behind segments that do not exist must not
		// survive the re-append in resolveDeepest.
		{"traversal through a missing segment", filepath.Join(root, "missing", "..", "..", "escaped.yml")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := safepath.ResolveContained(root, tc.candidate); err == nil {
				t.Errorf("ResolveContained(%q) was allowed, want it rejected", tc.candidate)
			}
		})
	}
}

func TestResolveContainedAllowsTheRootItself(t *testing.T) {
	root := t.TempDir()

	got, err := safepath.ResolveContained(root, root)
	if err != nil {
		t.Fatalf("ResolveContained(root, root) = %v, want it allowed", err)
	}
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if got != resolvedRoot {
		t.Errorf("got %q, want %q", got, resolvedRoot)
	}
}

func TestResolveContainedRejectsEmptyRoot(t *testing.T) {
	if _, err := safepath.ResolveContained("", "/tmp/x.yml"); err == nil {
		t.Error("empty root was accepted, want an error — an empty root would contain nothing meaningfully")
	}
}

// TestResolveContainedIsNotFooledByASiblingPrefix guards the containment
// comparison: "/data/repos-evil" must not count as inside "/data/repos".
func TestResolveContainedIsNotFooledByASiblingPrefix(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repos")
	sibling := filepath.Join(base, "repos-evil")
	for _, dir := range []string{root, sibling} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	if _, err := safepath.ResolveContained(root, filepath.Join(sibling, "x.yml")); err == nil {
		t.Error("a sibling directory sharing the root's name prefix was accepted, want it rejected")
	}
}
