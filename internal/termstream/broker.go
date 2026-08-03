// Package termstream is an in-process fan-out broker for interactive
// terminal session output, keyed by session id. It mirrors internal/logstream's
// shape but carries raw bytes with no cumulative-replay buffer: a terminal
// session's client is expected to stay connected for the session's lifetime,
// unlike a deploy log tail that a client may join late. Phase-1 scope only —
// no reconnect replay, no persisted transcript (see docs/TERMINAL.md).
package termstream

import "sync"

// Event is one unit of terminal session activity delivered to subscribers.
// Exactly one of Data or Closed is meaningful per event.
type Event struct {
	Data     []byte
	Closed   bool
	ExitCode int
	Error    string
}

// maxPendingEvents bounds the pre-subscribe buffer (see pending below) so a
// session whose browser side never connects (open() dispatched, tab closed
// before the SSE request lands) can't grow it unbounded.
const maxPendingEvents = 256

// Broker fans out terminal session events to subscribers, keyed by session id.
type Broker struct {
	mu   sync.Mutex
	subs map[string][]chan Event

	// pending buffers events published for a session before its first
	// subscriber has attached. The worker can start emitting output (shell
	// banner/prompt) as soon as it opens the exec session, which routinely
	// races the browser's POST-then-GET-SSE open sequence — without this,
	// that first output is simply dropped (no subscriber yet to receive it),
	// which is what made a session's first line invisible until something
	// else (e.g. pressing enter) caused the shell to re-print a prompt.
	// Cleared the moment a subscriber shows up; only ever needed once per
	// session, since after that a live subscriber is already listening.
	pending map[string][]Event
}

// New creates an empty Broker.
func New() *Broker {
	return &Broker{
		subs:    make(map[string][]chan Event),
		pending: make(map[string][]Event),
	}
}

// Subscribe registers a listener for events on sessionID. The returned
// channel is buffered so a slow/no-op reader can't block Publish; call
// unsubscribe when done to stop delivery and release the channel. Any
// events published for sessionID before this call (see pending) are
// delivered first, in order, ahead of anything published afterward.
func (b *Broker) Subscribe(sessionID string) (ch <-chan Event, unsubscribe func()) {
	c := make(chan Event, 64)

	b.mu.Lock()
	for _, ev := range b.pending[sessionID] {
		select {
		case c <- ev:
		default:
			// Buffered backlog exceeds the subscriber channel's own
			// capacity — extremely unlikely (pending is capped well below
			// it) but drop rather than block under the lock.
		}
	}
	delete(b.pending, sessionID)
	b.subs[sessionID] = append(b.subs[sessionID], c)
	b.mu.Unlock()

	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subs[sessionID]
		for i, sc := range subs {
			if sc == c {
				b.subs[sessionID] = append(subs[:i], subs[i+1:]...)
				close(c)
				break
			}
		}
		if len(b.subs[sessionID]) == 0 {
			delete(b.subs, sessionID)
		}
	}

	return c, unsub
}

// PublishOutput delivers a chunk of terminal output to sessionID's subscribers.
func (b *Broker) PublishOutput(sessionID string, data []byte) {
	b.publish(sessionID, Event{Data: data})
}

// PublishClosed notifies sessionID's subscribers that the session ended.
func (b *Broker) PublishClosed(sessionID string, exitCode int, errMsg string) {
	b.publish(sessionID, Event{Closed: true, ExitCode: exitCode, Error: errMsg})
	b.mu.Lock()
	delete(b.pending, sessionID)
	b.mu.Unlock()
}

func (b *Broker) publish(sessionID string, ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subs[sessionID]
	if len(subs) == 0 {
		buf := b.pending[sessionID]
		if len(buf) >= maxPendingEvents {
			buf = buf[1:]
		}
		b.pending[sessionID] = append(buf, ev)
		return
	}

	for _, c := range subs {
		select {
		case c <- ev:
		default:
			// Slow subscriber — drop rather than block the worker callback
			// goroutine; the session itself isn't paced by this broker.
		}
	}
}
