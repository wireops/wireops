// Env file serialization. The server renders .env content and ships it to
// the worker as DeployCommand.EnvFileB64 — it never writes .env (or
// .gitignore) into the repository checkout, which is why nothing here
// touches the filesystem and why reconciles of stacks sharing a checkout
// directory need no lock between them.
package sync

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// serializeEnvContent renders envVars as .env file content and returns the
// result as a string. Each KEY=VALUE pair is written on its own line; values
// are quoted via quoteEnvValue when they contain special characters.
// Newlines and carriage returns are escaped, keeping each entry on one physical
// line while Docker Compose reconstructs the original value.
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
		sb.WriteString(key)
		sb.WriteByte('=')
		sb.WriteString(quoteEnvValue(val))
		sb.WriteByte('\n')
	}
	return sb.String(), nil
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
//   - Single-line values containing '$' but no single-quote or trailing
//     backslash are wrapped in single quotes to prevent interpolation.
//   - Values containing both '$' and a single-quote are wrapped in double quotes
//     with backslashes, double-quotes, and dollar signs escaped to prevent
//     interpolation.
//   - Other values that require quoting are wrapped in double quotes with
//     backslashes, double-quotes, and control whitespace escaped.
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
	if hasDollar && !hasSingleQuote && !strings.ContainsAny(val, "\r\n") && !strings.HasSuffix(val, `\`) {
		// Single-quote wrap: $ is preserved verbatim, no escaping needed.
		return "'" + val + "'"
	}
	// Double-quote wrap: escape \, ", and $ to prevent interpolation.
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(val)
	return `"` + escaped + `"`
}
