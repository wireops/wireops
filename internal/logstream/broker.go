// Package logstream is an in-process fan-out broker for sync_logs updates,
// used to turn GET /api/custom/stacks/{id}/stream from a one-shot historical
// replay into a live tail: PocketBase record hooks publish here on every
// sync_logs write, and the stream handler subscribes per-stack to forward
// new output to connected clients.
package logstream

import (
	"sync"
	"time"
)

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

	liveMu sync.Mutex
	live   map[string]*liveCommand // commandID -> ephemeral cumulative buffer
}

// liveCommand accumulates incremental output lines for a single in-flight
// deploy/redeploy/teardown command, purely in memory. It exists only to let
// PublishLine reuse the same RecordID+cumulative-Output convention that the
// sync_logs-backed Event/streamHandoffState already understand, without
// persisting anything to the database — the final sync_logs row remains the
// sole durable record of a command's output.
type liveCommand struct {
	output    string
	lastSeq   int64
	updatedAt time.Time
}

// liveCommandRetention bounds how long an idle live command buffer is kept
// before opportunistic sweeping reclaims it, in case a final CommandResult
// is ever lost (e.g. worker crash mid-deploy) and no explicit cleanup happens.
const liveCommandRetention = 15 * time.Minute

// liveCommandMaxBytes caps a single command's live buffer so a runaway or
// malicious stream of lines can't grow server memory unbounded.
const liveCommandMaxBytes = 1 << 20 // 1 MiB

// New creates an empty Broker.
func New() *Broker {
	return &Broker{
		subs: make(map[string][]chan Event),
		live: make(map[string]*liveCommand),
	}
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

// liveRecordID is the synthetic RecordID used for a command's ephemeral live
// buffer, namespaced so it can never collide with a real sync_logs record id.
func liveRecordID(commandID string) string {
	return "live:" + commandID
}

// PublishLine appends a single incremental output line to commandID's ephemeral
// in-memory buffer and publishes the resulting cumulative Event to stackID's
// subscribers — reusing the same cumulative-Output convention the /stream
// handler's streamHandoffState already dedupes against. Nothing here is
// persisted to sync_logs; the final CommandResult remains the durable record.
func (b *Broker) PublishLine(stackID, commandID, phase, line string, seq int64) {
	if commandID == "" {
		return
	}

	b.liveMu.Lock()
	lc, ok := b.live[commandID]
	if !ok {
		lc = &liveCommand{}
		b.live[commandID] = lc
	}
	// Guard against a redelivered/out-of-order line (transport retries carry
	// no ordering guarantee beyond Seq): only append lines at or after the
	// highest sequence number already seen for this command.
	if seq > lc.lastSeq {
		if len(lc.output) < liveCommandMaxBytes {
			if phase != "" {
				lc.output += "[" + phase + "] " + line + "\n"
			} else {
				lc.output += line + "\n"
			}
		}
		lc.lastSeq = seq
	}
	lc.updatedAt = time.Now()
	output := lc.output
	b.sweepLiveLocked()
	b.liveMu.Unlock()

	b.Publish(stackID, Event{RecordID: liveRecordID(commandID), Output: output, Status: "running"})
}

// ForgetLiveCommand drops commandID's ephemeral live buffer once its final
// CommandResult has been recorded, so memory doesn't accumulate across the
// lifetime of a long-running server. Safe to call even if no buffer exists.
func (b *Broker) ForgetLiveCommand(commandID string) {
	b.liveMu.Lock()
	delete(b.live, commandID)
	b.liveMu.Unlock()
}

// sweepLiveLocked evicts live command buffers idle past liveCommandRetention.
// Called with liveMu already held; kept O(n) and cheap since command counts
// are bounded by concurrently in-flight deploys, not overall history.
func (b *Broker) sweepLiveLocked() {
	cutoff := time.Now().Add(-liveCommandRetention)
	for id, lc := range b.live {
		if lc.updatedAt.Before(cutoff) {
			delete(b.live, id)
		}
	}
}
