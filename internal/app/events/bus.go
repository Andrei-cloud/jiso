package events

import (
	"sync"
	"sync/atomic"
)

// DefaultBuffer is the per-subscriber channel capacity used by New when
// no WithBuffer option is given.
const DefaultBuffer = 256

// subscriber is one consumer's delivery queue.
type subscriber struct {
	ch      chan Event
	dropped *atomic.Uint64
}

// Bus is a small typed publish/subscribe hub. It is safe for concurrent
// publishers and subscribers and creates no goroutines of its own.
//
// # Delivery and drop policy
//
// Every subscriber owns a buffered channel. Publish never blocks: it
// performs a non-blocking send to each subscriber and, on a full queue,
// applies a drop-oldest policy - it evicts the oldest queued event to
// make room for the newest one (progress views care about the freshest
// state, not stale ticks). If even that room is lost to a concurrent
// publisher, the newest event itself is dropped instead. Every event a
// publisher hands to a subscriber is therefore either queued or counted
// in Dropped; with a single publisher and no concurrent reader the
// dropped events are exactly the oldest ones. Events published while no
// subscriber exists, or after Close, are discarded silently and not
// counted in Dropped.
type Bus struct {
	buffer int

	mu     sync.RWMutex
	subs   map[*subscriber]struct{}
	closed bool

	dropped atomic.Uint64
}

// New returns an open Bus.
func New() *Bus {
	return &Bus{buffer: DefaultBuffer, subs: make(map[*subscriber]struct{})}
}

// Subscribe returns a channel receiving every event published after this
// call, and an unsubscribe function. Unsubscribe closes the channel
// (consumers ranging over it observe the close and exit), is idempotent,
// and frees the subscriber's queue; the Bus retains nothing for
// unsubscribed consumers. Subscribing after Close returns an already
// closed channel and a no-op unsubscribe, so consumers terminate
// immediately.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()

		ch := make(chan Event)
		close(ch)

		return ch, func() {}
	}

	sub := &subscriber{ch: make(chan Event, b.buffer), dropped: &b.dropped}
	b.subs[sub] = struct{}{}
	b.mu.Unlock()

	return sub.ch, func() { b.unsubscribe(sub) }
}

func (b *Bus) unsubscribe(sub *subscriber) {
	b.mu.Lock()
	if _, ok := b.subs[sub]; ok {
		delete(b.subs, sub)
		close(sub.ch)
	}
	b.mu.Unlock()
}

// Publish delivers ev to every current subscriber without blocking; see
// the type documentation for the drop-oldest policy.
func (b *Bus) Publish(ev Event) {
	if ev == nil {
		return
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}

	for sub := range b.subs {
		sub.trySend(ev)
	}
}

// trySend queues ev, evicting the oldest queued event when the queue is
// full. Every eviction, and ev itself when even the freed slot is lost
// to a concurrent publisher, is counted in the shared dropped counter,
// so published == received + dropped holds at all times.
func (s *subscriber) trySend(ev Event) {
	select {
	case s.ch <- ev:
		return
	default:
	}

	// Queue full: evict the oldest queued event to make room for the
	// newest one.
	select {
	case <-s.ch:
		s.dropped.Add(1)
	default:
	}

	select {
	case s.ch <- ev:
	default:
		s.dropped.Add(1)
	}
}

// Subscribers returns the number of live subscribers. Used by tests and
// diagnostics to assert unsubscribe frees resources.
func (b *Bus) Subscribers() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.subs)
}

// Dropped returns the cumulative number of events discarded by the drop
// policy across all subscribers since the Bus was created.
func (b *Bus) Dropped() uint64 {
	return b.dropped.Load()
}

// Close closes the bus and every subscriber channel, releasing all
// subscriber queues. It is idempotent; publishes after Close are no-ops
// and Subscribe after Close yields an already-closed channel.
func (b *Bus) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()

		return
	}
	b.closed = true

	subs := b.subs
	b.subs = make(map[*subscriber]struct{})
	b.mu.Unlock()

	for sub := range subs {
		close(sub.ch)
	}
}
