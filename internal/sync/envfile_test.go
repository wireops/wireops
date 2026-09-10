package sync

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wireops/wireops/internal/protocol"
)

var multilineValues = map[string]string{
	"gcp":                 "{\n  \"type\": \"service_account\",\n  \"private_key\": \"-----BEGIN PRIVATE KEY-----\\nFAKE\\n-----END PRIVATE KEY-----\\n\"\n}\n",
	"pem":                 "-----BEGIN PRIVATE KEY-----\nFAKE-CONTENT\n-----END PRIVATE KEY-----\n",
	"special":             "first\nsecond's $TOKEN ${OTHER} \\ literal\\n # x",
	"crlf":                "first\r\nsecond\r\n",
	"spaces":              "  first\nlast  ",
	"blank":               "\nline\n\n",
	"backslash":           "value $TOKEN\nlast\\",
	"singleLineBackslash": "value $TOKEN\\",
}

func TestEnvFileMultiline(t *testing.T) {
	for name, value := range multilineValues {
		t.Run(name, func(t *testing.T) {
			pairs := []string{"VALUE=" + value}
			content, err := serializeEnvContent(pairs)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(content, "\n") != 1 || strings.Contains(content, "\r") {
				t.Fatal("each entry must occupy one physical line")
			}
			encoded, err := BuildEnvFileB64(pairs)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || string(decoded) != content {
				t.Fatal("transport changed content")
			}
			dir := t.TempDir()
			if err := WriteEnvFile(dir, pairs); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, ".env"))
			if err != nil || string(data) != content {
				t.Fatal("disk content differs from transport")
			}
			info, err := os.Stat(filepath.Join(dir, ".env"))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("env file must remain private")
			}
		})
	}
	for _, entry := range []string{"MISSING_EQUALS", "BAD\nKEY=value", "=value"} {
		if _, err := serializeEnvContent([]string{entry}); err == nil {
			t.Fatalf("accepted invalid entry %q", entry)
		}
	}
}

// No daemon needed: exercise the real Compose parser rather than asserting
// only our serializer's own escape conventions.
func TestEnvFileComposeRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker CLI unavailable")
	}
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("Compose unavailable")
	}
	for name, value := range multilineValues {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := WriteEnvFile(dir, []string{"WIREOPS_TEST_VALUE=" + value}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "docker", "compose", "--project-name", "env-check", "--project-directory", dir, "--env-file", filepath.Join(dir, ".env"), "-f", "-", "config", "--environment")
			cmd.Stdin = strings.NewReader("services:\n  test:\n    image: unused\n    environment:\n      WIREOPS_TEST_VALUE: ${WIREOPS_TEST_VALUE}\n")
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), "WIREOPS_TEST_VALUE="+value+"\n") {
				t.Fatal("Compose did not reconstruct the original value")
			}
		})
	}
}

// Explicit opt-in: uses an already available image with sh/printf, creates only
// disposable containers, disables networking, and never pulls an image.
func TestEnvFileDockerContainers(t *testing.T) {
	image := os.Getenv("WIREOPS_DOCKER_TEST_IMAGE")
	if image == "" {
		t.Skip("set WIREOPS_DOCKER_TEST_IMAGE to run container checks")
	}
	for name, value := range multilineValues {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := WriteEnvFile(dir, []string{"VALUE=" + value}); err != nil {
				t.Fatal(err)
			}
			config := map[string]any{"services": map[string]any{"test": map[string]any{
				"image": image, "network_mode": "none", "environment": map[string]string{"VALUE": "${VALUE}"},
				"entrypoint": []string{"sh", "-c", `printf '%s' "$$VALUE"`},
			}}}
			data, _ := json.Marshal(config)
			file := filepath.Join(dir, "compose.json")
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			composeCmd := exec.CommandContext(ctx, "docker", "compose", "-p", "wireops-env-test", "--env-file", filepath.Join(dir, ".env"), "-f", file, "run", "--rm", "--no-deps", "--pull", "never", "-T", "test")
			out, err := composeCmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != value {
				t.Fatal("Compose container value differs")
			}
			job := protocol.RunJobCommand{JobRunID: fmt.Sprintf("env-test-%d-%d", os.Getpid(), time.Now().UnixNano()), Image: image, Network: "none", Env: map[string]string{"VALUE": value}, Command: []string{"sh", "-c", `printf '%s' "$VALUE"`}}
			args := job.BuildDockerRunArgs()
			args = append([]string{"run", "--pull=never"}, args[1:]...)
			out, err = exec.CommandContext(ctx, "docker", args...).Output()
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != value {
				t.Fatal("Job container value differs")
			}
		})
	}
}
