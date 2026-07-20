// Package utils holds small helper functions shared across mcp tools.
package utils

import "strings"

// ToInterfaceSlice converts a []string into []interface{} so it matches the
// type assertions ValidateComposeConfig performs on decoded YAML/JSON maps.
func ToInterfaceSlice(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// VolumeSource extracts the source (left side) of a "source:target[:mode]"
// bind-mount/volume spec, mirroring the parsing internal/policy.ValidateComposeConfig
// applies when checking volumes against worker policy.
func VolumeSource(spec string) string {
	spec = strings.TrimSpace(spec)
	if isWindowsDriveLetterPath(spec) {
		rest := spec[2:]
		if idx := strings.Index(rest, ":"); idx != -1 {
			return spec[:2+idx]
		}
		return spec
	}
	parts := strings.Split(spec, ":")
	if len(parts) > 1 {
		return strings.TrimSpace(parts[0])
	}
	p := strings.TrimSpace(parts[0])
	if IsHostPath(p) {
		return p
	}
	return ""
}

// isWindowsDriveLetterPath reports whether spec starts with a Windows drive
// letter (e.g. "C:\" or "C:/"), whose colon must not be treated as the
// source:target separator that strings.Split(spec, ":") assumes.
func isWindowsDriveLetterPath(spec string) bool {
	if len(spec) < 3 || spec[1] != ':' {
		return false
	}
	c := spec[0]
	isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	return isLetter && (spec[2] == '\\' || spec[2] == '/')
}

// IsHostPath reports whether a volume source string looks like a host filesystem
// path (bind mount) rather than a named volume reference.
func IsHostPath(src string) bool {
	return strings.HasPrefix(src, "/") || strings.HasPrefix(src, "./") || strings.HasPrefix(src, "../") || strings.HasPrefix(src, "~")
}
