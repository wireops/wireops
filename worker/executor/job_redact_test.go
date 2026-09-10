package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wireops/wireops/internal/protocol"
)

// Opt-in integration check of the actual RunJob output path, including stderr
// and a non-zero exit. The image must already exist locally and support sh.
func TestRunJobMultilineOutputRedaction(t *testing.T) {
	image := os.Getenv("WIREOPS_DOCKER_TEST_IMAGE")
	if image == "" {
		t.Skip("set WIREOPS_DOCKER_TEST_IMAGE to run container checks")
	}
	dockerPath, err := lookPathSecure("docker")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := exec.CommandContext(ctx, dockerPath, "image", "inspect", image).Run(); err != nil {
		t.Fatalf("test image must already be available locally: %v", err)
	}
	for _, exitCode := range []int{0, 7} {
		t.Run(fmt.Sprintf("Exit%d", exitCode), func(t *testing.T) {
			jobID := fmt.Sprintf("redact-test-%d-%d", os.Getpid(), time.Now().UnixNano())
			command := `printf '%s\n' "$VALUE"; printf '%s\n' 'first-secret'; printf '%s\n' 'second-secret' >&2; printf '%s\n' "$SHORT"; exit ` + fmt.Sprint(exitCode)
			result := RunJob(ctx, protocol.RunJobCommand{
				JobRunID: jobID, CommandID: "multiline-redaction", Image: image,
				Network: "none", TimeoutSeconds: 20,
				Env:     map[string]string{"VALUE": "first-secret\nsecond-secret", "SHORT": "on"},
				Command: []string{"sh", "-c", command},
			})
			if result.Success != (exitCode == 0) || result.JobRunID != jobID {
				t.Fatalf("unexpected result: %+v", result)
			}
			if strings.Contains(result.Output, "first-secret") || strings.Contains(result.Output, "second-secret") {
				t.Fatalf("job output leaked a secret: %q", result.Output)
			}
			if strings.Count(result.Output, redactedPlaceholder) != 3 || !strings.Contains(result.Output, "\non\n") {
				t.Fatalf("expected full-value and per-line redaction, preserving short values: %q", result.Output)
			}
			if exitCode != 0 && !strings.Contains(result.Output, "exit status 7") {
				t.Fatalf("job failure status was lost: %q", result.Output)
			}
		})
	}
}
