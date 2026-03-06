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
