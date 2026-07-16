package compose

import (
	"reflect"
	"testing"
)

func TestLineWriterEmitsCompleteLines(t *testing.T) {
	var got []string
	lw := &lineWriter{onLine: func(line string) { got = append(got, line) }}

	_, _ = lw.Write([]byte("Pulling image\nStarting container\n"))
	want := []string{"Pulling image", "Starting container"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLineWriterBuffersPartialLineUntilFlush(t *testing.T) {
	var got []string
	lw := &lineWriter{onLine: func(line string) { got = append(got, line) }}

	_, _ = lw.Write([]byte("Container starting"))
	if len(got) != 0 {
		t.Fatalf("expected no lines emitted before newline/flush, got %v", got)
	}
	_, _ = lw.Write([]byte("... done\n"))
	if !reflect.DeepEqual(got, []string{"Container starting... done"}) {
		t.Fatalf("got %v, want single joined line", got)
	}

	_, _ = lw.Write([]byte("trailing partial"))
	lw.Flush()
	want := []string{"Container starting... done", "trailing partial"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLineWriterNilCallbackIsNoop(t *testing.T) {
	lw := &lineWriter{}
	n, err := lw.Write([]byte("some output\n"))
	if err != nil || n != len("some output\n") {
		t.Fatalf("Write() = %d, %v; want %d, nil", n, err, len("some output\n"))
	}
	lw.Flush() // must not panic
}
