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

// ValidateConfigName checks that a config entry name is a bare identifier
// safe to use as a Docker Compose `configs:` key and as a directory entry
// name on the worker filesystem (job config staging).
func ValidateConfigName(name string) error {
	if name == "" {
		return fmt.Errorf("config name cannot be empty")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("config name must not contain path separators: %q", name)
	}
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return fmt.Errorf("config name must not start with '.': %q", name)
	}
	return nil
}

// ValidateContainerMountPath checks that an in-container mount target is a
// clean absolute path (no traversal), used for job.yaml config targets.
func ValidateContainerMountPath(target string) error {
	if target == "" {
		return fmt.Errorf("mount target cannot be empty")
	}
	if !filepath.IsAbs(target) {
		return fmt.Errorf("mount target must be absolute: %q", target)
	}
	cleaned := filepath.Clean(target)
	if cleaned != target || cleaned == "/" {
		return fmt.Errorf("mount target is not a clean absolute path: %q", target)
	}
	return nil
}

// OpenRoot resolves dir against root (root defaults to dir itself when
// empty — a caller with no meaningful containment boundary still gets a dir
// that can't escape itself) and opens an os.Root scoped to root, so every
// subsequent path resolved against it follows in-root symlinks but refuses
// any that escape root. Returns dir's root-relative path alongside it, for
// joining a filename onto before an r.Open/r.Lstat/r.ReadFile call.
//
// This exists because a plain os.Lstat/os.Open on a path built by joining
// caller-influenced segments onto a directory follows symlinked
// *intermediate* path components silently — a check on the final filename
// alone (e.g. rejecting a symlinked "docker-compose.yml") doesn't stop a
// symlinked parent directory from redirecting the whole read elsewhere. Repo
// checkout content is exactly this kind of attacker-influenced input: git
// preserves symlinks verbatim, and compose_path (validated by
// ValidateComposePath as a string, never against what it resolves to on
// disk) can select any directory in that checkout.
//
// The returned *os.Root must be Closed by the caller when the error is nil.
// A root that doesn't exist on disk yet (e.g. a repository never cloned)
// surfaces as a plain wrapped error here; callers that want to treat that
// case as "nothing found" rather than a failure can check
// errors.Is(err, fs.ErrNotExist).
func OpenRoot(root, dir string) (*os.Root, string, error) {
	rootDir := root
	if rootDir == "" {
		rootDir = dir
	}
	rel, err := filepath.Rel(rootDir, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, "", fmt.Errorf("path %q resolves outside %q", dir, rootDir)
	}

	r, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, "", fmt.Errorf("cannot open root %q: %w", rootDir, err)
	}
	return r, rel, nil
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
