package executor

import (
	"sort"
	"strings"
)

// redactedPlaceholder replaces secret values found in worker-captured output
// before it is returned to the server (and eventually shown to operators).
const redactedPlaceholder = "[REDACTED]"

// minRedactableSecretLen avoids masking very short values (e.g. "1", "true")
// that are common in non-secret env vars and would otherwise blast holes
// through unrelated log lines.
const minRedactableSecretLen = 4

// parseEnvValues extracts the VALUE half of each KEY=VALUE line in a .env
// file's raw bytes. Comments and blank lines are skipped. Values are used
// as candidates for redaction in command output, not for any other purpose.
func parseEnvValues(data []byte) []string {
	var values []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		value := strings.TrimSpace(line[idx+1:])
		encoded := value
		value = decodeEnvValue(value)
		if len(value) >= minRedactableSecretLen {
			values = append(values, value)
		}
		// Keep the serialized form too: diagnostics may quote the .env entry
		// instead of printing the decoded environment value.
		if len(encoded) >= 2 && (encoded[0] == '"' || encoded[0] == '\'') && encoded[len(encoded)-1] == encoded[0] {
			encoded = encoded[1 : len(encoded)-1]
		}
		if encoded != value && len(encoded) >= minRedactableSecretLen {
			values = append(values, encoded)
		}
	}
	return redactionCandidates(values)
}

// decodeEnvValue reverses the quoting produced by the server's .env writer.
// Decode in one pass so a literal backslash-n never becomes a newline.
func decodeEnvValue(value string) string {
	if len(value) < 2 {
		return value
	}
	quote := value[0]
	if (quote != '\'' && quote != '"') || value[len(value)-1] != quote {
		return value
	}
	value = value[1 : len(value)-1]
	if quote == '\'' {
		return value
	}
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) {
			switch value[i+1] {
			case 'n':
				out.WriteByte('\n')
			case 'r':
				out.WriteByte('\r')
			case 't':
				out.WriteByte('\t')
			case '\\', '"', '$':
				out.WriteByte(value[i+1])
			default:
				out.WriteByte(value[i])
				continue
			}
			i++
		} else {
			out.WriteByte(value[i])
		}
	}
	return out.String()
}

// Collect full values and individual lines for streaming output. Longest first
// prevents a short candidate from partially replacing a longer secret.
func redactionCandidates(values []string) []string {
	seen := make(map[string]bool)
	var candidates []string
	add := func(value string) {
		if len(value) >= minRedactableSecretLen && !seen[value] {
			seen[value] = true
			candidates = append(candidates, value)
		}
	}
	for _, value := range values {
		add(value)
		for _, line := range strings.FieldsFunc(value, func(r rune) bool { return r == '\n' || r == '\r' }) {
			add(line)
			add(strings.TrimSpace(line))
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return len(candidates[i]) > len(candidates[j]) })
	return candidates
}

// redactSecrets replaces every occurrence of each secret value in text with
// a fixed placeholder. Values shorter than minRedactableSecretLen are
// ignored by the caller-side collectors, but this function re-checks length
// as defense in depth.
func redactSecrets(text string, secrets []string) string {
	if text == "" || len(secrets) == 0 {
		return text
	}
	for _, secret := range secrets {
		if len(secret) < minRedactableSecretLen {
			continue
		}
		text = strings.ReplaceAll(text, secret, redactedPlaceholder)
	}
	return text
}
