package sync

import (
	"bufio"
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

	var sb strings.Builder
	for _, kv := range envVars {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			// Malformed env var — write as-is followed by newline.
			sb.WriteString(kv)
			sb.WriteByte('\n')
			continue
		}
		key := kv[:idx]
		val := kv[idx+1:]
		sb.WriteString(key)
		sb.WriteByte('=')
		sb.WriteString(quoteEnvValue(val))
		sb.WriteByte('\n')
	}

	path := filepath.Join(workDir, envFileName)
	if err := os.WriteFile(path, []byte(sb.String()), 0600); err != nil {
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

// quoteEnvValue returns a safely quoted .env value.
// Values that contain whitespace, quotes, $, #, or = characters are wrapped in
// double quotes, with any embedded double-quotes backslash-escaped.
// Simple alphanumeric-and-punctuation values are returned as-is.
func quoteEnvValue(val string) string {
	needsQuote := false
	for _, c := range val {
		if c == ' ' || c == '\t' || c == '"' || c == '\'' ||
			c == '$' || c == '#' || c == '\\' || c == '\n' || c == '\r' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return val
	}
	// Escape backslashes and double-quotes, then wrap.
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(val)
	return `"` + escaped + `"`
}
