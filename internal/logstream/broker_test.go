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
