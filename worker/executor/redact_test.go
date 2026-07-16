package executor

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseEnvValues(t *testing.T) {
	data := []byte("# comment\nAPI_KEY=supersecretvalue\nSHORT=ab\nQUOTED=\"quotedsecret\"\n\nEMPTY=\nDB_URL=postgres://user:pass@host/db\n")
	values := parseEnvValues(data)

	want := map[string]bool{
		"supersecretvalue":             true,
		"quotedsecret":                 true,
		"postgres://user:pass@host/db": true,
	}
	if len(values) != len(want) {
		t.Fatalf("parseEnvValues() = %v, want %d values matching %v", values, len(want), want)
	}
	for _, v := range values {
		if !want[v] {
			t.Errorf("unexpected value in parseEnvValues(): %q", v)
		}
	}
	for _, v := range values {
		if v == "ab" {
			t.Errorf("short value %q should have been skipped", v)
		}
	}
}

func TestRedactSecrets(t *testing.T) {
	secrets := []string{"supersecretvalue", "postgres://user:pass@host/db"}
	text := "Pulling image...\nConnecting with token supersecretvalue\nDB dsn=postgres://user:pass@host/db ok\n"

	got := redactSecrets(text, secrets)

	if got == text {
		t.Fatalf("redactSecrets() did not change output")
	}
	for _, s := range secrets {
		if strings.Contains(got, s) {
			t.Errorf("redacted output still contains secret %q: %s", s, got)
		}
	}
	if !strings.Contains(got, redactedPlaceholder) {
		t.Errorf("redacted output missing placeholder %q: %s", redactedPlaceholder, got)
	}
}

func TestRedactSecretsSkipsShortValues(t *testing.T) {
	got := redactSecrets("value is 1 and on", []string{"1", "on"})
	if got != "value is 1 and on" {
		t.Errorf("redactSecrets() should skip values shorter than %d chars, got %q", minRedactableSecretLen, got)
	}
}

func TestApplyEnvFileReturnsSecretsForRedaction(t *testing.T) {
	workDir := t.TempDir()
	envContent := "TOKEN=verysecrettoken1234\nDEBUG=on\n"
	envB64 := base64.StdEncoding.EncodeToString([]byte(envContent))

	secrets, err := applyEnvFile(workDir, envB64)
	if err != nil {
		t.Fatalf("applyEnvFile() error = %v", err)
	}

	found := false
	for _, s := range secrets {
		if s == "verysecrettoken1234" {
			found = true
		}
		if s == "on" {
			t.Errorf("short/non-secret value %q should not be collected", s)
		}
	}
	if !found {
		t.Errorf("expected secret value to be collected, got %v", secrets)
	}
}
