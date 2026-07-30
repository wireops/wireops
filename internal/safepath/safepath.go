package safepath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateComposePath checks that a compose_path is safe (no traversal).
func ValidateComposePath(p string) error {
	if p == "" || p == "." {
		return nil
	}
	cleaned := filepath.Clean(p)
	if strings.Contains(cleaned, "..") {
		return fmt.Errorf("compose_path contains invalid traversal: %q", p)
	}
	if filepath.IsAbs(cleaned) {
		return fmt.Errorf("compose_path must be relative: %q", p)
	}
	return nil
}

// ValidateComposeFile checks that a compose_file is a .yml or .yaml file with no traversal.
func ValidateComposeFile(f string) error {
	if f == "" {
		return nil
	}
	cleaned := filepath.Clean(f)
	if strings.Contains(cleaned, "..") {
		return fmt.Errorf("compose_file contains invalid traversal: %q", f)
	}
	if filepath.IsAbs(cleaned) {
		return fmt.Errorf("compose_file must be relative: %q", f)
	}
	if strings.Contains(cleaned, string(filepath.Separator)) {
		return fmt.Errorf("compose_file must be a filename, not a path: %q", f)
	}
	ext := strings.ToLower(filepath.Ext(cleaned))
	if ext != ".yml" && ext != ".yaml" {
		return fmt.Errorf("compose_file must end in .yml or .yaml: %q", f)
	}
	return nil
}

// ValidateBackupKey checks that a backup archive key is a bare .zip filename
// with no path traversal or directory separators — it is interpolated into
// filesystem/S3 paths by internal/backup, so a caller-supplied key must never
// be able to escape the backups directory or address a different object.
func ValidateBackupKey(key string) error {
	if key == "" {
		return fmt.Errorf("backup key cannot be empty")
	}
	cleaned := filepath.Clean(key)
	if cleaned != key {
		return fmt.Errorf("backup key contains invalid characters: %q", key)
	}
	if strings.ContainsAny(cleaned, "/\\") {
		return fmt.Errorf("backup key must be a filename, not a path: %q", key)
	}
	if cleaned == "." || cleaned == ".." || strings.Contains(cleaned, "..") {
		return fmt.Errorf("backup key contains invalid traversal: %q", key)
	}
	if strings.ToLower(filepath.Ext(cleaned)) != ".zip" {
		return fmt.Errorf("backup key must end in .zip: %q", key)
	}
	return nil
}

// ValidateHostPath checks that a host path is absolute and does not contain traversal.
func ValidateHostPath(p string) error {
	if p == "" {
		return fmt.Errorf("host path cannot be empty")
	}
	cleaned := filepath.Clean(p)
	if strings.Contains(cleaned, "..") {
		return fmt.Errorf("host path contains invalid traversal: %q", p)
	}
	if !filepath.IsAbs(cleaned) {
		return fmt.Errorf("host path must be absolute: %q", p)
	}
	return nil
}

// ResolveContained resolves candidate and verifies it stays inside root,
// following symlinks on the way.
//
// The Validate* functions above only inspect the path *string*: they reject
// "..", absolute paths and separators, which is everything an attacker can
// express through a request parameter. They cannot see what the filesystem
// does with that string. A git repository can carry symlinks (mode 120000,
// preserved on checkout), so a repo containing
//
//	docker-compose.yml -> /etc/passwd
//
// passes every string check — the name really is a plain ".yml" filename —
// and then reads /etc/passwd. The extension check constrains the link's name,
// never its target. This function closes that gap, and must be used before
// any read whose path is influenced by repository content or request input.
//
// A candidate that does not exist yet is still checked: the deepest existing
// ancestor is resolved and the remaining segments appended, so a symlinked
// parent directory cannot smuggle the path out either.
//
// The returned path is the fully resolved one, safe to hand to the filesystem.
func ResolveContained(root, candidate string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("containment root cannot be empty")
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("cannot resolve root %q: %w", root, err)
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return "", fmt.Errorf("cannot resolve root %q: %w", root, err)
	}

	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path %q: %w", candidate, err)
	}

	resolved, err := resolveDeepest(absCandidate)
	if err != nil {
		return "", err
	}

	if resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves outside the permitted directory", candidate)
	}
	return resolved, nil
}

// resolveDeepest resolves symlinks in path, tolerating a path whose trailing
// segments do not exist yet by resolving the deepest existing ancestor and
// re-appending the rest.
func resolveDeepest(path string) (string, error) {
	remainder := ""
	current := filepath.Clean(path)

	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			if remainder == "" {
				return resolved, nil
			}
			// Clean the join so a ".." smuggled in through a non-existent
			// segment cannot survive re-appending.
			return filepath.Clean(filepath.Join(resolved, remainder)), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("cannot resolve path %q: %w", path, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached the filesystem root without finding anything that
			// exists — nothing to resolve against.
			return filepath.Clean(path), nil
		}
		remainder = filepath.Join(filepath.Base(current), remainder)
		current = parent
	}
}

// CleanRelativePath validates that a path is relative and does not contain traversal,
// returning the cleaned path or an error.
func CleanRelativePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	cleaned := filepath.Clean(p)
	if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("path is absolute or escapes base directory: %q", p)
	}
	return cleaned, nil
}
