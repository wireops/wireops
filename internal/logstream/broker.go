// Package logstream is an in-process fan-out broker for sync_logs updates,
// used to turn GET /api/custom/stacks/{id}/stream from a one-shot historical
// replay into a live tail: PocketBase record hooks publish here on every
// sync_logs write, and the stream handler subscribes per-stack to forward
// new output to connected clients.
package logstream

import "sync"

// Event carries a sync_logs row's state at the time it was created/updated.
// Output is the full cumulative field value (not a delta) — the reconciler
// rewrites it whole on every save, so subscribers diff against what they've
// already sent.
type Event struct {
	RecordID string
	Output   string
	Status   string
}

// Broker fans out sync_logs events to subscribers, keyed by stack id.
type Broker struct {
	mu   sync.Mutex
	subs map[string][]chan Event
}

// New creates an empty Broker.
func New() *Broker {
	return &Broker{subs: make(map[string][]chan Event)}
}

// Subscribe registers a listener for events on stackID. The returned channel
// is buffered so a slow/no-op reader can't block Publish; call unsubscribe
// when done to stop delivery and release the channel.
func (b *Broker) Subscribe(stackID string) (ch <-chan Event, unsubscribe func()) {
	c := make(chan Event, 16)

	b.mu.Lock()
	b.subs[stackID] = append(b.subs[stackID], c)
	b.mu.Unlock()

	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subs[stackID]
		for i, sc := range subs {
			if sc == c {
				b.subs[stackID] = append(subs[:i], subs[i+1:]...)
				close(c)
				break
			}
		}
		if len(b.subs[stackID]) == 0 {
			delete(b.subs, stackID)
		}
	}
	return c, unsub
}

// Publish delivers ev to every current subscriber of stackID. Subscribers
// with a full buffer are skipped for this event rather than blocking the
// publisher (the caller is a PocketBase record hook, on the write path).
func (b *Broker) Publish(stackID string, ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Held for the whole send loop (not just the snapshot) so unsubscribe
	// can't close a channel concurrently with a send to it; sends stay
	// non-blocking via the select/default below, so this can't stall Publish.
	for _, c := range b.subs[stackID] {
		select {
		case c <- ev:
		default:
		}
	}
}
