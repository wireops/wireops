package compose

import (
	"bytes"
	"errors"
	"fmt"
)

// ErrOutputTooLarge is returned when a compose subprocess produces more output
// than the caller allowed. Callers can test for it with errors.Is to tell "this
// compose file is too big" apart from "docker failed".
var ErrOutputTooLarge = errors.New("compose output exceeds the configured limit")

// limitedBuffer accumulates a subprocess's output up to a fixed ceiling and
// fails past it.
//
// It deliberately errors rather than truncating: the buffer's contents get
// JSON-decoded and then linted or rendered, and a silently truncated config
// would parse as a *different, smaller* stack — dropping services, volumes, or
// the very policy-relevant fields a deploy is checked against. A hard failure
// is the only safe behaviour.
//
// Returning an error from Write also stops exec.Cmd's copy goroutine, so the
// child process gets a broken pipe instead of being allowed to keep streaming.
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int64
	// what names the stream in the error message ("stdout"/"stderr").
	what string
	// overflowed records that this buffer is the one that hit its limit, so
	// the caller can tell which stream failed. exec.Cmd surfaces only the
	// resulting error, and stdout and stderr have different limits with
	// different remedies — one is raisable via COMPOSE_MAX_KB, the other is
	// a fixed internal cap.
	overflowed bool
}

func newLimitedBuffer(what string, limit int64) *limitedBuffer {
	return &limitedBuffer{limit: limit, what: what}
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	if int64(w.buf.Len())+int64(len(p)) > w.limit {
		w.overflowed = true
		return 0, fmt.Errorf("%w: %s reached %d bytes", ErrOutputTooLarge, w.what, w.limit)
	}
	return w.buf.Write(p)
}

// Overflowed reports whether this buffer is the one that hit its limit.
func (w *limitedBuffer) Overflowed() bool { return w.overflowed }

func (w *limitedBuffer) String() string { return w.buf.String() }

func (w *limitedBuffer) Len() int { return w.buf.Len() }
