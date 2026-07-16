package compose

import (
	"bytes"
	"sync"
)

// lineWriter splits a byte stream into lines and invokes onLine for each
// complete line as it arrives, in addition to whatever else it's tee'd into.
// It buffers a trailing partial line until the next write or Flush.
// Used to give callers of RunUp/RunForceUp/RunDown/RunDownPurge incremental
// visibility into docker compose output without changing the final,
// combined-string return value those functions already provide.
//
// exec.Cmd copies stdout and stderr concurrently in separate goroutines when
// both are set, and a lineWriter is tee'd into both here — so Write/Flush
// must be safe for concurrent use.
type lineWriter struct {
	onLine func(string)
	mu     sync.Mutex
	buf    bytes.Buffer
}

func (w *lineWriter) Write(p []byte) (int, error) {
	if w.onLine == nil {
		return len(p), nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// Incomplete line: put it back and wait for more data.
			w.buf.Reset()
			w.buf.WriteString(line)
			break
		}
		w.onLine(trimNewline(line))
	}
	return len(p), nil
}

// Flush emits any buffered partial line once the process has exited.
func (w *lineWriter) Flush() {
	if w.onLine == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if remaining := w.buf.String(); remaining != "" {
		w.onLine(trimNewline(remaining))
		w.buf.Reset()
	}
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
