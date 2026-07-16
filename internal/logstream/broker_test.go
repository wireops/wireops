package logstream

import (
	"testing"
	"time"
)

func TestSubscribeReceivesPublishedEvent(t *testing.T) {
	b := New()
	ch, unsubscribe := b.Subscribe("stack1")
	defer unsubscribe()

	b.Publish("stack1", Event{RecordID: "r1", Output: "hello", Status: "running"})

	select {
	case ev := <-ch:
		if ev.Output != "hello" || ev.RecordID != "r1" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published event")
	}
}

func TestPublishOnlyReachesMatchingStack(t *testing.T) {
	b := New()
	chA, unsubA := b.Subscribe("stackA")
	defer unsubA()
	chB, unsubB := b.Subscribe("stackB")
	defer unsubB()

	b.Publish("stackA", Event{RecordID: "r1", Output: "for A"})

	select {
	case ev := <-chA:
		if ev.Output != "for A" {
			t.Fatalf("unexpected event on stackA: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stackA event")
	}

	select {
	case ev := <-chB:
		t.Fatalf("stackB should not receive stackA's event, got %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	b := New()
	ch, unsubscribe := b.Subscribe("stack1")
	unsubscribe()

	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
}

func TestPublishWithNoSubscribersDoesNotBlock(t *testing.T) {
	b := New()
	done := make(chan struct{})
	go func() {
		b.Publish("nobody-subscribed", Event{Output: "x"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked with no subscribers")
	}
}

func TestPublishLineAccumulatesCumulativeOutput(t *testing.T) {
	b := New()
	ch, unsubscribe := b.Subscribe("stack1")
	defer unsubscribe()

	b.PublishLine("stack1", "cmd-1", "compose_up", "Pulling image", 1)
	b.PublishLine("stack1", "cmd-1", "compose_up", "Starting container", 2)

	var last Event
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch:
			last = ev
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for published line event")
		}
	}

	want := "[compose_up] Pulling image\n[compose_up] Starting container\n"
	if last.Output != want {
		t.Fatalf("cumulative output = %q, want %q", last.Output, want)
	}
	if last.RecordID != "live:cmd-1" {
		t.Fatalf("RecordID = %q, want live:cmd-1", last.RecordID)
	}
}

func TestPublishLineIgnoresOutOfOrderSeq(t *testing.T) {
	b := New()
	ch, unsubscribe := b.Subscribe("stack1")
	defer unsubscribe()

	b.PublishLine("stack1", "cmd-1", "", "second", 2)
	<-ch
	b.PublishLine("stack1", "cmd-1", "", "stale-retry", 1) // redelivered/out-of-order
	ev := <-ch

	if ev.Output != "second\n" {
		t.Fatalf("out-of-order line should have been dropped, got output %q", ev.Output)
	}
}

func TestForgetLiveCommandClearsBuffer(t *testing.T) {
	b := New()
	ch, unsubscribe := b.Subscribe("stack1")
	defer unsubscribe()

	b.PublishLine("stack1", "cmd-1", "", "first", 1)
	<-ch

	b.ForgetLiveCommand("cmd-1")

	b.PublishLine("stack1", "cmd-1", "", "after-forget", 1)
	ev := <-ch
	if ev.Output != "after-forget\n" {
		t.Fatalf("expected buffer to restart after ForgetLiveCommand, got %q", ev.Output)
	}
}

func TestMultipleSubscribersBothReceive(t *testing.T) {
	b := New()
	ch1, unsub1 := b.Subscribe("stack1")
	defer unsub1()
	ch2, unsub2 := b.Subscribe("stack1")
	defer unsub2()

	b.Publish("stack1", Event{Output: "broadcast"})

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Output != "broadcast" {
				t.Fatalf("subscriber %d: unexpected event: %+v", i, ev)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for event", i)
		}
	}
}
