package safepath

import (
	"fmt"
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
