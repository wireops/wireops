package executor

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf8"
)

func dockerLogFrame(stream byte, payload string) []byte {
	frame := make([]byte, 8+len(payload))
	frame[0] = stream
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	copy(frame[8:], payload)
	return frame
}

func TestFormatContainerLogsHandlesDockerFramesAndTTY(t *testing.T) {
	nonTTY := append(dockerLogFrame(1, "INFO ready\n"), dockerLogFrame(2, "ERROR failed\n")...)
	logs, truncated, err := formatContainerLogs(nonTTY, false, "ERROR", false)
	if err != nil {
		t.Fatalf("format non-TTY logs: %v", err)
	}
	if logs != "ERROR failed" || truncated {
		t.Fatalf("unexpected non-TTY result logs=%q truncated=%t", logs, truncated)
	}

	logs, truncated, err = formatContainerLogs([]byte("INFO ready\nERROR failed\n"), true, "ERROR", false)
	if err != nil {
		t.Fatalf("format TTY logs: %v", err)
	}
	if logs != "ERROR failed" || truncated {
		t.Fatalf("unexpected TTY result logs=%q truncated=%t", logs, truncated)
	}
}

func TestFormatContainerLogsCapsResponseAndRetainsSourceTruncation(t *testing.T) {
	input := "old-" + strings.Repeat("x", 6<<10) + strings.Repeat("é", 33<<10) + "newest!"
	logs, truncated, err := formatContainerLogs([]byte(input), true, "", false)
	if err != nil {
		t.Fatalf("format capped logs: %v", err)
	}
	if len(logs) > 64<<10 || !truncated {
		t.Fatalf("got len=%d truncated=%t", len(logs), truncated)
	}
	if !strings.HasSuffix(logs, "newest!") || !utf8.ValidString(logs) {
		t.Fatalf("capped logs must retain a valid newest tail: %q", logs[len(logs)-16:])
	}

	logs, truncated, err = formatContainerLogs([]byte("partial"), true, "", true)
	if err != nil || logs != "partial" || !truncated {
		t.Fatalf("source truncation was not retained: logs=%q truncated=%t err=%v", logs, truncated, err)
	}
}
