package compose

import (
	"errors"
	"strings"
	"testing"
)

func TestLimitedBufferAcceptsUpToTheLimit(t *testing.T) {
	w := newLimitedBuffer("stdout", 10)

	n, err := w.Write([]byte("12345"))
	if err != nil || n != 5 {
		t.Fatalf("first write = (%d, %v), want (5, nil)", n, err)
	}
	// Exactly reaching the limit must still succeed — the cap is inclusive.
	n, err = w.Write([]byte("67890"))
	if err != nil || n != 5 {
		t.Fatalf("second write = (%d, %v), want (5, nil)", n, err)
	}
	if got := w.String(); got != "1234567890" {
		t.Errorf("String() = %q, want the full content", got)
	}
}

func TestLimitedBufferRejectsRatherThanTruncating(t *testing.T) {
	w := newLimitedBuffer("stdout", 10)

	if _, err := w.Write([]byte("12345678")); err != nil {
		t.Fatalf("write under the limit failed: %v", err)
	}

	n, err := w.Write([]byte("XXXXX"))
	if err == nil {
		t.Fatal("write past the limit returned nil error, want a failure")
	}
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Errorf("error = %v, want it to wrap ErrOutputTooLarge", err)
	}
	// A partial write would leave a config that still parses but describes a
	// different, smaller stack — that must never happen.
	if n != 0 {
		t.Errorf("n = %d, want 0 — the write must not be partially applied", n)
	}
	if got := w.String(); got != "12345678" {
		t.Errorf("String() = %q, want the pre-limit content untouched", got)
	}
}

func TestLimitedBufferNamesTheStream(t *testing.T) {
	w := newLimitedBuffer("stderr", 4)

	_, err := w.Write([]byte("way too long"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "stderr") {
		t.Errorf("error = %q, want it to name the stream", err)
	}
	if !strings.Contains(err.Error(), "4") {
		t.Errorf("error = %q, want it to state the limit", err)
	}
}

func TestLimitedBufferRejectsASingleOversizeWrite(t *testing.T) {
	w := newLimitedBuffer("stdout", 4)

	if _, err := w.Write([]byte("12345")); !errors.Is(err, ErrOutputTooLarge) {
		t.Errorf("error = %v, want ErrOutputTooLarge for a first write already past the limit", err)
	}
	if w.Len() != 0 {
		t.Errorf("Len() = %d, want 0", w.Len())
	}
}
