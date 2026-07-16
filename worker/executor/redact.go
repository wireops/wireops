package executor

import "strings"

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
		value = strings.Trim(value, `"'`)
		if len(value) >= minRedactableSecretLen {
			values = append(values, value)
		}
	}
	return values
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
