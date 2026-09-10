package executor

import (
	"encoding/base64"
	"errors"
	"slices"
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

func TestMultilineRedaction(t *testing.T) {
	value := "  first-secret\nsecond-secret\r\nquote\" and $TOKEN literal\\n"
	data := []byte(`VALUE="  first-secret\nsecond-secret\r\nquote\" and \$TOKEN literal\\n"` + "\n")
	values := parseEnvValues(data)
	if !slices.Contains(values, value) {
		t.Fatalf("full decoded value missing: %q", values)
	}
	if got := redactSecrets(value, values); got != redactedPlaceholder {
		t.Fatalf("full output: %q", got)
	}
	for _, line := range strings.Split(value, "\n") {
		if got := redactSecrets(line, values); strings.Contains(got, "secret") || strings.Contains(got, "$TOKEN") {
			t.Fatalf("streamed line not redacted: %q", got)
		}
	}
	if got := redactSecrets("first-secret", values); got != redactedPlaceholder {
		t.Fatal("trimmed line leaked")
	}
	jobValues := redactionCandidates([]string{value})
	if got := redactSecrets("second-secret", jobValues); got != redactedPlaceholder {
		t.Fatal("job line leaked")
	}
}

func TestMultilineWorkDirOutputRedaction(t *testing.T) {
	previousDir := stackDir
	stackDir = t.TempDir()
	t.Cleanup(func() { stackDir = previousDir })
	content := `VALUE="first-secret\nsecond-secret"` + "\n"
	var lines []string
	output, err := runInWorkDir("redaction-test", "command", base64.StdEncoding.EncodeToString([]byte("services: {}\n")), base64.StdEncoding.EncodeToString([]byte(content)), "test",
		func(line string) { lines = append(lines, line) },
		func(_, _ string, onLine func(string)) (string, error) {
			onLine("first-secret")
			onLine("second-secret")
			return "first-secret\nsecond-secret", errors.New(`failure: first-secret\nsecond-secret`)
		})
	if output != redactedPlaceholder || err == nil || err.Error() != "failure: "+redactedPlaceholder {
		t.Fatalf("output/error redaction failed: %q, %v", output, err)
	}
	if !slices.Equal(lines, []string{redactedPlaceholder, redactedPlaceholder}) {
		t.Fatalf("stream redaction failed: %q", lines)
	}
}

func TestDecodeEnvValue(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`"one\ntwo\\n"`, "one\ntwo\\n"},
		{`'literal\n$TOKEN'`, `literal\n$TOKEN`},
		{`"\"quoted\"\t\$TOKEN\\"`, "\"quoted\"\t$TOKEN\\"},
		{`plain`, `plain`},
		{`"unknown\z"`, `unknown\z`},
	} {
		if got := decodeEnvValue(tc.input); got != tc.want {
			t.Errorf("decode %q = %q, want %q", tc.input, got, tc.want)
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
