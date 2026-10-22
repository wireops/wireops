package executor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wireops/wireops/internal/protocol"
)

func TestCappedWriterTruncatesWithoutShortWriting(t *testing.T) {
	var buf bytes.Buffer
	w := limitWriter(&buf, 5)

	// A short write would make stdcopy report an error and cut the command
	// off before its exit status, so the excess is reported as written and
	// dropped instead.
	n, err := w.Write([]byte("abc"))
	if err != nil || n != 3 {
		t.Fatalf("Write = (%d, %v), want (3, nil)", n, err)
	}
	n, err = w.Write([]byte("defgh"))
	if err != nil || n != 5 {
		t.Fatalf("Write = (%d, %v), want (5, nil)", n, err)
	}
	if got := buf.String(); got != "abcde" {
		t.Fatalf("buffer = %q, want %q", got, "abcde")
	}

	// Past the cap every write still succeeds and writes nothing.
	n, err = w.Write([]byte("ijk"))
	if err != nil || n != 3 {
		t.Fatalf("Write past the cap = (%d, %v), want (3, nil)", n, err)
	}
	if buf.Len() != 5 {
		t.Fatalf("buffer grew past the cap to %d bytes", buf.Len())
	}
	if !w.dropped {
		t.Error("expected the writer to record that it dropped bytes")
	}
}

func TestCappedWriterExactlyAtTheCapIsNotTruncated(t *testing.T) {
	var buf bytes.Buffer
	w := limitWriter(&buf, 5)

	// Exactly at the cap nothing was dropped, so reporting the stream as cut
	// short would tell the caller its complete output is incomplete.
	if _, err := w.Write([]byte("abcde")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.dropped {
		t.Error("output exactly at the cap must not be reported as truncated")
	}
	// A zero-length write past the cap drops nothing either.
	if _, err := w.Write(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.dropped {
		t.Error("an empty write past the cap must not count as dropped output")
	}
}

func TestCappedWriterPassesThroughUnderTheCap(t *testing.T) {
	var buf bytes.Buffer
	w := limitWriter(&buf, 1024)
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.String() != "hello" {
		t.Fatalf("buffer = %q", buf.String())
	}
	if w.dropped {
		t.Error("a write under the cap must not be reported as truncated")
	}
}

func TestExecOneShotRejectsAnEmptyArgv(t *testing.T) {
	// Checked before any Docker call, which is what lets this run without a
	// daemon — and is also the right order: there is nothing to exec.
	result := ExecOneShot(t.Context(), execCommandWithoutArgv())
	if result.Error == "" {
		t.Fatal("expected an error for an empty argv")
	}
	if !strings.Contains(result.Error, "non-empty cmd") {
		t.Errorf("unexpected error %q", result.Error)
	}
	if result.CommandID != "exec-no-argv" {
		t.Errorf("command_id = %q, want exec-no-argv", result.CommandID)
	}
}

// execCommandWithoutArgv is the one ExecCommand shape that can be exercised
// without a Docker daemon.
func execCommandWithoutArgv() protocol.ExecCommand {
	return protocol.ExecCommand{
		CommandID:   "exec-no-argv",
		StackID:     "stack-1",
		ContainerID: "c-web",
	}
}
