package sync

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const envFileName = ".env"

// WriteEnvFile writes the given KEY=VALUE env vars to a .env file inside workDir.
// Values containing whitespace, quotes, or special shell characters are quoted
// with double quotes, with internal double-quotes escaped.
// If envVars is empty, RemoveEnvFile is called instead.
// The file is written with mode 0600 (owner-readable only).
func WriteEnvFile(workDir string, envVars []string) error {
	if len(envVars) == 0 {
		return RemoveEnvFile(workDir)
	}

	contents, err := serializeEnvContent(envVars)
	if err != nil {
		return fmt.Errorf("envfile: %w", err)
	}

	path := filepath.Join(workDir, envFileName)
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		return fmt.Errorf("envfile: failed to write %s: %w", path, err)
	}
	return nil
}

// RemoveEnvFile removes the .env file in workDir if it exists.
// If it does not exist, RemoveEnvFile returns nil.
func RemoveEnvFile(workDir string) error {
	path := filepath.Join(workDir, envFileName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("envfile: failed to remove %s: %w", path, err)
	}
	return nil
}

// serializeEnvContent renders envVars as .env file content and returns the
// result as a string. Each KEY=VALUE pair is written on its own line; values
// are quoted via quoteEnvValue when they contain special characters.
// Values that contain literal newlines or carriage returns are rejected because
// they cannot be represented on a single .env line without breaking parsers.
func serializeEnvContent(envVars []string) (string, error) {
	var sb strings.Builder
	for _, kv := range envVars {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			return "", fmt.Errorf("malformed entry %q: missing '='", kv)
		}
		key := kv[:idx]
		val := kv[idx+1:]
		if err := validateEnvKey(key); err != nil {
			return "", err
		}
		if strings.ContainsAny(val, "\r\n") {
			return "", fmt.Errorf("key %q contains a multiline value which is not supported in .env files", key)
		}
		sb.WriteString(key)
		sb.WriteByte('=')
		sb.WriteString(quoteEnvValue(val))
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// EnsureGitignoreHasEnv checks for a .gitignore file in the given directory.
// If it does not exist, it creates one with ".env" as the sole entry.
// If it exists but does not already contain a ".env" entry, it appends one.
// This prevents accidental commit of generated .env files.
func EnsureGitignoreHasEnv(dir string) error {
	gitignorePath := filepath.Join(dir, ".gitignore")

	// Check if .gitignore already contains a .env entry.
	if data, err := os.ReadFile(gitignorePath); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == ".env" || line == "/.env" {
				return nil // already present
			}
		}
		// File exists but no .env entry — append.
		// Ensure we start on a new line.
		appendStr := ".env\n"
		if len(data) > 0 && data[len(data)-1] != '\n' {
			appendStr = "\n" + appendStr
		}
		f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("envfile: failed to open .gitignore for append: %w", err)
		}
		defer f.Close()
		_, err = f.WriteString(appendStr)
		return err
	}

	// .gitignore does not exist — create it.
	return os.WriteFile(gitignorePath, []byte(".env\n"), 0644)
}

// BuildEnvFileB64 serializes envVars as a .env file and returns the base64-encoded
// content. If envVars is empty it returns ("", nil), which signals the worker to
// remove any existing .env file. This is the exported counterpart of the
// package-internal buildEnvFileB64 used by the reconciler.
func BuildEnvFileB64(envVars []string) (string, error) {
	if len(envVars) == 0 {
		return "", nil
	}
	content, err := serializeEnvContent(envVars)
	if err != nil {
		return "", fmt.Errorf("BuildEnvFileB64: %w", err)
	}
	return base64.StdEncoding.EncodeToString([]byte(content)), nil
}

// validateEnvKey returns an error if key is not a valid .env variable name.
// A valid key must be non-empty, must not have surrounding whitespace, and must
// not contain '=', newlines, carriage returns, or other control characters.
func validateEnvKey(key string) error {
	if key == "" {
		return fmt.Errorf("env key must not be empty")
	}
	if strings.TrimSpace(key) != key {
		return fmt.Errorf("env key %q must not have surrounding whitespace", key)
	}
	for _, c := range key {
		if c == '=' || c == '\n' || c == '\r' || c < 0x20 {
			return fmt.Errorf("env key %q contains invalid character %q", key, c)
		}
	}
	return nil
}

// quoteEnvValue returns a safely quoted .env value.
//   - Values containing '$' but no single-quote are wrapped in single quotes so
//     Docker Compose does not interpolate variables.
//   - Values containing both '$' and a single-quote are wrapped in double quotes
//     with backslashes, double-quotes, and dollar signs escaped to prevent
//     interpolation.
//   - Other values that require quoting are wrapped in double quotes with
//     backslashes and double-quotes escaped.
//   - Simple values with no special characters are returned as-is.
func quoteEnvValue(val string) string {
	hasDollar := strings.ContainsRune(val, '$')
	hasSingleQuote := strings.ContainsRune(val, '\'')

	needsQuote := hasDollar || hasSingleQuote
	if !needsQuote {
		for _, c := range val {
			if c == ' ' || c == '\t' || c == '"' ||
				c == '#' || c == '\\' || c == '\n' || c == '\r' {
				needsQuote = true
				break
			}
		}
	}
	if !needsQuote {
		return val
	}
	if hasDollar && !hasSingleQuote {
		// Single-quote wrap: $ is preserved verbatim, no escaping needed.
		return "'" + val + "'"
	}
	// Double-quote wrap: escape \, ", and $ to prevent interpolation.
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`).Replace(val)
	return `"` + escaped + `"`
}
