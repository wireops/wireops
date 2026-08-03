package termstream

import "testing"

// TestPublishBeforeSubscribeIsBuffered guards against the exact bug this
// buffer was added for: a worker can start emitting output (the shell's
// first prompt) before the browser's SSE request has subscribed, and that
// output must not be silently dropped.
func TestPublishBeforeSubscribeIsBuffered(t *testing.T) {
	b := New()

	b.PublishOutput("sess-1", []byte("first prompt\n"))
	b.PublishOutput("sess-1", []byte("second line\n"))

	ch, unsub := b.Subscribe("sess-1")
	defer unsub()

	first := <-ch
	if string(first.Data) != "first prompt\n" {
		t.Fatalf("expected buffered first chunk, got %q", first.Data)
	}
	second := <-ch
	if string(second.Data) != "second line\n" {
		t.Fatalf("expected buffered second chunk, got %q", second.Data)
	}
}

func TestPublishAfterSubscribeIsDeliveredLive(t *testing.T) {
	b := New()

	ch, unsub := b.Subscribe("sess-2")
	defer unsub()

	b.PublishOutput("sess-2", []byte("live chunk"))

	ev := <-ch
	if string(ev.Data) != "live chunk" {
		t.Fatalf("expected live chunk, got %q", ev.Data)
	}
}

func TestPendingBufferClearedAfterFirstSubscribe(t *testing.T) {
	b := New()

	b.PublishOutput("sess-3", []byte("buffered"))
	ch, unsub := b.Subscribe("sess-3")
	<-ch // drain the buffered event
	unsub()

	// A second subscriber (e.g. reconnect) must not replay what the first
	// subscriber already consumed.
	ch2, unsub2 := b.Subscribe("sess-3")
	defer unsub2()

	b.PublishOutput("sess-3", []byte("only this"))
	ev := <-ch2
	if string(ev.Data) != "only this" {
		t.Fatalf("expected only the new chunk, got %q", ev.Data)
	}
}

// TestPublishClosedIsReplayedToLateSubscriber guards against the exact bug
// this buffer exists for on the close path too: a session can fail (and
// publish its closed event) before the browser's GET .../stream request has
// subscribed — that closed event must not be dropped, or the SSE stream
// hangs open forever with nothing ever delivered.
func TestPublishClosedIsReplayedToLateSubscriber(t *testing.T) {
	b := New()

	b.PublishOutput("sess-4", []byte("orphaned"))
	b.PublishClosed("sess-4", 7, "boom")

	ch, unsub := b.Subscribe("sess-4")
	defer unsub()

	output := <-ch
	if string(output.Data) != "orphaned" {
		t.Fatalf("expected buffered output before close, got %q", output.Data)
	}
	closedEv := <-ch
	if !closedEv.Closed || closedEv.ExitCode != 7 || closedEv.Error != "boom" {
		t.Fatalf("expected closed event with exit code/error, got %+v", closedEv)
	}
}

func TestPendingBufferIsBounded(t *testing.T) {
	b := New()

	for i := 0; i < maxPendingEvents+10; i++ {
		b.PublishOutput("sess-5", []byte{byte(i)})
	}

	b.mu.Lock()
	got := len(b.pending["sess-5"])
	b.mu.Unlock()
	if got != maxPendingEvents {
		t.Fatalf("expected pending buffer capped at %d, got %d", maxPendingEvents, got)
	}
}
