package events

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// recv reads exactly n events, failing on channel close or timeout, and
// asserts nothing extra is queued afterwards.
func recv(t *testing.T, ch <-chan Event, n int) []Event {
	t.Helper()

	got := make([]Event, 0, n)
	for i := 0; i < n; i++ {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed after %d of %d events", i, n)
			}
			got = append(got, ev)
		case <-time.After(time.Second):
			t.Fatalf("timed out after %d of %d events", i, n)
		}
	}

	select {
	case extra := <-ch:
		t.Errorf("unexpected extra event %+v", extra)
	default:
	}

	return got
}

func TestPublishSubscribeRoundTrip(t *testing.T) {
	t.Parallel()

	bus := New()
	defer bus.Close()

	ch, unsub := bus.Subscribe()
	defer unsub()

	if n := bus.Subscribers(); n != 1 {
		t.Fatalf("Subscribers = %d, want 1", n)
	}

	want := []Event{
		WorkerStarted{ID: "w1", Kind: "stress"},
		WorkerProgress{ID: "w1", Done: 3, Total: 10, Note: "12 tps"},
		WorkerStopped{ID: "w1", Reason: "done"},
		ConnectionEvent{State: StateConnected, Detail: "127.0.0.1:8583"},
		Logf{Level: "info", Msg: "hello"},
	}
	for _, ev := range want {
		bus.Publish(ev)
	}

	got := recv(t, ch, len(want))
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestMultipleSubscribersEachReceive(t *testing.T) {
	t.Parallel()

	bus := New()
	defer bus.Close()

	ch1, unsub1 := bus.Subscribe()
	defer unsub1()
	ch2, unsub2 := bus.Subscribe()
	defer unsub2()

	bus.Publish(Logf{Level: "info", Msg: "both"})

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev != Event(Logf{Level: "info", Msg: "both"}) {
				t.Errorf("subscriber %d got %+v", i, ev)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d received nothing", i)
		}
	}
}

func TestSlowSubscriberDropsOldest(t *testing.T) {
	t.Parallel()

	bus := New()
	bus.buffer = 2
	defer bus.Close()

	ch, unsub := bus.Subscribe()
	defer unsub()

	const published = 5
	for i := 0; i < published; i++ {
		bus.Publish(Logf{Level: "info", Msg: fmt.Sprint(i)})
	}

	got := recv(t, ch, 2)
	want := []Event{Logf{Level: "info", Msg: "3"}, Logf{Level: "info", Msg: "4"}}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("queued events = %v, want the two newest %v (drop-oldest)", got, want)
	}
	if d := bus.Dropped(); d != published-2 {
		t.Errorf("Dropped = %d, want %d", d, published-2)
	}
}

func TestUnsubscribeStopsDeliveryAndFreesResources(t *testing.T) {
	t.Parallel()

	bus := New()
	defer bus.Close()

	ch, unsub := bus.Subscribe()
	if n := bus.Subscribers(); n != 1 {
		t.Fatalf("Subscribers = %d, want 1", n)
	}

	unsub()
	unsub() // idempotent

	if n := bus.Subscribers(); n != 0 {
		t.Errorf("Subscribers after unsubscribe = %d, want 0", n)
	}

	if _, ok := <-ch; ok {
		t.Error("channel still open after unsubscribe")
	}

	// Publishing after unsubscribe must not panic and must not deliver.
	bus.Publish(Logf{Msg: "after"})

	// The bus keeps nothing for the unsubscribed consumer.
	if n := bus.Subscribers(); n != 0 {
		t.Errorf("Subscribers after post-unsubscribe publish = %d, want 0", n)
	}
}

func TestNoGoroutineLeaksAfterUnsubscribeAndClose(t *testing.T) {
	t.Parallel()

	// goleak-style counting without the dependency: churn subscribers
	// with live consumers and assert the goroutine count returns to the
	// baseline once every consumer observes its channel close.
	before := runtime.NumGoroutine()

	for i := 0; i < 25; i++ {
		bus := New()
		bus.buffer = 4

		ch, unsub := bus.Subscribe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			for range ch { //nolint:revive // draining consumer
			}
		}()

		bus.Publish(Logf{Msg: fmt.Sprint(i)})
		unsub()
		bus.Close()
		<-done
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= before+2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: baseline %d, now %d after churn", before, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCloseIsIdempotentAndQuiesces(t *testing.T) {
	t.Parallel()

	bus := New()
	bus.buffer = 4

	ch, unsub := bus.Subscribe()

	bus.Close()
	bus.Close() // must not panic
	unsub()     // unsubscribe after Close must not panic either

	if _, ok := <-ch; ok {
		t.Error("subscriber channel still open after Close")
	}
	if n := bus.Subscribers(); n != 0 {
		t.Errorf("Subscribers after Close = %d, want 0", n)
	}

	// Publish after Close is a silent no-op.
	bus.Publish(Logf{Msg: "ignored"})
	if d := bus.Dropped(); d != 0 {
		t.Errorf("Dropped after closed-bus publish = %d, want 0 (silently discarded)", d)
	}

	// Subscribe after Close yields an already-closed channel and a no-op
	// unsubscribe.
	ch2, unsub2 := bus.Subscribe()
	if _, ok := <-ch2; ok {
		t.Error("Subscribe after Close returned an open channel")
	}
	unsub2()
	unsub2()

	if err := bus.Subscribers(); err != 0 {
		t.Errorf("Subscribers after closed-bus subscribe = %d, want 0", err)
	}
}

func TestConcurrentPublishersAccountExactly(t *testing.T) {
	t.Parallel()

	const (
		publishers = 8
		perPub     = 100
	)

	bus := New()
	bus.buffer = 64

	ch, unsub := bus.Subscribe()

	var (
		wg       sync.WaitGroup
		consumed []Event
		cm       sync.Mutex
	)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			cm.Lock()
			consumed = append(consumed, ev)
			cm.Unlock()
		}
	}()

	for p := 0; p < publishers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for i := 0; i < perPub; i++ {
				bus.Publish(Logf{Level: "info", Msg: fmt.Sprintf("p%d-%d", p, i)})
			}
		}(p)
	}
	wg.Wait()

	unsub() // closes ch after all publishes; consumer drains and exits
	<-done

	// Drop-oldest accounting invariant: every published event was
	// either received or counted as dropped.
	want := uint64(publishers * perPub)
	if got := uint64(len(consumed)) + bus.Dropped(); got != want {
		t.Errorf("received %d + dropped %d = %d, want %d", len(consumed), bus.Dropped(), got, want)
	}
	if len(consumed) == 0 {
		t.Error("consumer received nothing")
	}

	bus.Close()
}
