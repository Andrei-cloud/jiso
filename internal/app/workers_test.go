package app

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"jiso/internal/app/events"
)

// fakeSender stands in for the CLI's SendCommand: every send fails fast
// like a dial against a closed port, or succeeds after a small delay.
type fakeSender struct {
	mu    sync.Mutex
	calls int
	delay time.Duration
	// fail makes ExecuteBackground return an error (default true).
	fail bool
}

func newFakeSender(delay time.Duration, fail bool) *fakeSender {
	return &fakeSender{delay: delay, fail: fail}
}

func (f *fakeSender) StartClock() {}

func (f *fakeSender) ExecuteBackground(_ string, _ bool, _ string) (string, time.Duration, error) {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}

	f.mu.Lock()
	f.calls++
	f.mu.Unlock()

	if f.fail {
		return "ERROR", 0, errors.New("dial tcp 127.0.0.1:65535: connect: connection refused")
	}

	return "00", time.Millisecond, nil
}

func (f *fakeSender) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// newWorkerTestApp builds an App wired to the fake sender against a
// closed loopback port.
func newWorkerTestApp(t *testing.T, sender WorkerSender) *App {
	t.Helper()

	cfg := testConfig(t)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	t.Cleanup(func() {
		_ = a.Close()
	})

	a.SetWorkerSenderResolver(func() WorkerSender { return sender })

	return a
}

// drainWorkerEvents collects events for one worker until its
// WorkerStopped arrives (or timeout), returning everything seen for that
// ID in order.
func drainWorkerEvents(t *testing.T, ch <-chan events.Event, id string) []events.Event {
	t.Helper()

	var got []events.Event
	deadline := time.After(10 * time.Second)

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("event channel closed before WorkerStopped for %s (saw %d events)", id, len(got))
			}
			switch e := ev.(type) {
			case events.WorkerStarted:
				if e.ID == id {
					got = append(got, e)
				}
			case events.WorkerProgress:
				if e.ID == id {
					got = append(got, e)
				}
			case events.WorkerStopped:
				if e.ID == id {
					return append(got, e)
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for WorkerStopped of %s (saw %d events)", id, len(got))
		}
	}
}

func TestStressWorkerEventSequenceAndTotals(t *testing.T) {
	sender := newFakeSender(0, true)
	a := newWorkerTestApp(t, sender)

	// Subscribe before start so no lifecycle event can be missed.
	ch, unsub := a.Events().Subscribe()
	defer unsub()

	// One worker: the first send fires immediately, and after the
	// step-0 interval elapses the always-failing sender trips the
	// circuit breaker well before the test duration ends.
	names := []string{"TX_A", "TX_B"}
	id, err := a.StressStart(names, 50, 50*time.Millisecond, 2*time.Second, 1)
	if err != nil {
		t.Fatalf("StressStart failed: %v", err)
	}

	seq := drainWorkerEvents(t, ch, id)

	// Sequence: started first, stopped last, at least one progress in
	// between (the finish snapshot guarantees one).
	if _, ok := seq[0].(events.WorkerStarted); !ok {
		t.Fatalf("first event = %T, want WorkerStarted", seq[0])
	}
	if ws, ok := seq[0].(events.WorkerStarted); ok && ws.Kind != "stress" {
		t.Errorf("WorkerStarted.Kind = %q, want %q", ws.Kind, "stress")
	}
	stopped, ok := seq[len(seq)-1].(events.WorkerStopped)
	if !ok {
		t.Fatalf("last event = %T, want WorkerStopped", seq[len(seq)-1])
	}
	if !strings.HasPrefix(stopped.Reason, "failed") {
		t.Errorf("WorkerStopped.Reason = %q, want a circuit-breaker failure reason", stopped.Reason)
	}

	var progress []events.WorkerProgress
	for _, ev := range seq[1 : len(seq)-1] {
		p, ok := ev.(events.WorkerProgress)
		if !ok {
			t.Fatalf("middle event = %T, want WorkerProgress", ev)
		}
		progress = append(progress, p)
	}
	if len(progress) == 0 {
		t.Fatal("no WorkerProgress events between started and stopped")
	}
	for i := 1; i < len(progress); i++ {
		if progress[i].Done < progress[i-1].Done {
			t.Errorf("progress %d went backwards: %d after %d", i, progress[i].Done, progress[i-1].Done)
		}
	}

	// The always-failing sender trips the 10-failure circuit breaker.
	total := sender.Calls()
	if total < CircuitBreakerFailures {
		t.Fatalf("fake sender recorded %d calls, want at least %d", total, CircuitBreakerFailures)
	}
	if last := progress[len(progress)-1].Done; last != total {
		t.Errorf("final progress Done = %d, want %d (all completions recorded by stop)", last, total)
	}

	// StressStats aggregates the completed-but-still-tracked worker.
	agg, err := a.StressStats()
	if err != nil {
		t.Fatalf("StressStats failed: %v", err)
	}
	if agg.Sent != total || agg.Successful+agg.Failed != total {
		t.Errorf("aggregate totals sent=%d ok+err=%d, want %d", agg.Sent, agg.Successful+agg.Failed, total)
	}
	if agg.Failed != total {
		t.Errorf("aggregate failed = %d, want %d (every send failed)", agg.Failed, total)
	}
	if agg.Status != "completed" {
		t.Errorf("aggregate status = %q, want completed", agg.Status)
	}

	// Per-worker summary reflects the same totals and breakdown.
	summary, err := a.StressSummaryByID(id)
	if err != nil {
		t.Fatalf("StressSummaryByID failed: %v", err)
	}
	if summary.Status != "completed" {
		t.Errorf("summary status = %q, want completed", summary.Status)
	}
	if summary.Sent != total || summary.Failed != total {
		t.Errorf("summary sent=%d failed=%d, want %d/%d", summary.Sent, summary.Failed, total, total)
	}
	if summary.ConsecutiveFailures < CircuitBreakerFailures {
		t.Errorf("summary consecutive failures = %d, want >= %d", summary.ConsecutiveFailures, CircuitBreakerFailures)
	}
	if summary.ResponseCodes["ERROR"] != total {
		t.Errorf("response codes ERROR = %d, want %d", summary.ResponseCodes["ERROR"], total)
	}
	if len(summary.Transactions) != len(names) {
		t.Fatalf("summary transactions = %d, want %d", len(summary.Transactions), len(names))
	}
	var txTotal int
	for _, tx := range summary.Transactions {
		txTotal += tx.Successful + tx.Failed
	}
	if txTotal != total {
		t.Errorf("per-transaction totals sum = %d, want %d", txTotal, total)
	}
}

func TestStressStopGracefulNoGoroutineLeaks(t *testing.T) {
	before := runtime.NumGoroutine()

	sender := newFakeSender(2*time.Millisecond, true)
	a := newWorkerTestApp(t, sender)

	ch, unsub := a.Events().Subscribe()
	defer unsub()

	id, err := a.StressStart([]string{"TX"}, 20, time.Minute, time.Minute, 4)
	if err != nil {
		t.Fatalf("StressStart failed: %v", err)
	}

	// Let it ramp a moment, then stop gracefully.
	time.Sleep(120 * time.Millisecond)

	if err := a.StressStop(id); err != nil {
		t.Fatalf("StressStop failed: %v", err)
	}

	seq := drainWorkerEvents(t, ch, id)
	stopped, ok := seq[len(seq)-1].(events.WorkerStopped)
	if !ok {
		t.Fatalf("last event = %T %+v, want events.WorkerStopped", seq[len(seq)-1], seq[len(seq)-1])
	}
	if stopped.Reason != "stopped" {
		t.Errorf("WorkerStopped.Reason = %q, want %q", stopped.Reason, "stopped")
	}

	if _, err := a.StressSummaryByID(id); err != nil {
		t.Errorf("StressSummaryByID after stop: %v", err)
	}

	if err := a.StressStop(id); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("second StressStop err = %v, want a not-found error", err)
	}

	if err := a.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// goleak-style counting without the dependency, same pattern as the
	// events bus tests: every worker goroutine must be gone.
	deadline := time.Now().Add(5 * time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= before+2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: baseline %d, now %d after stress run", before, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWorkerStartStopLifecycle(t *testing.T) {
	sender := newFakeSender(time.Millisecond, false)
	a := newWorkerTestApp(t, sender)

	ch, unsub := a.Events().Subscribe()
	defer unsub()

	if _, err := a.WorkerStart("TX", 1, 0); err == nil {
		t.Error("WorkerStart with zero interval returned no error")
	}

	id, err := a.WorkerStart("TX", 1, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("WorkerStart failed: %v", err)
	}

	views := a.Workers()
	if len(views) != 1 || views[0].ID != id || views[0].Type != "background" {
		t.Fatalf("Workers() = %+v, want one background worker %s", views, id)
	}

	time.Sleep(80 * time.Millisecond)

	if err := a.WorkerStop(id); err != nil {
		t.Fatalf("WorkerStop failed: %v", err)
	}

	seq := drainWorkerEvents(t, ch, id)
	started, ok := seq[0].(events.WorkerStarted)
	if !ok || started.Kind != "bgsend" {
		t.Fatalf("first event = %T %+v, want WorkerStarted kind bgsend", seq[0], seq[0])
	}
	stopped, ok := seq[len(seq)-1].(events.WorkerStopped)
	if !ok {
		t.Fatalf("last event = %T %+v, want events.WorkerStopped", seq[len(seq)-1], seq[len(seq)-1])
	}
	if stopped.Reason != "stopped" {
		t.Errorf("WorkerStopped.Reason = %q, want stopped", stopped.Reason)
	}
	if len(seq) < 3 {
		t.Errorf("event sequence = %d events, want started + progress + stopped", len(seq))
	}

	if n := len(a.Workers()); n != 0 {
		t.Errorf("Workers() after stop = %d, want 0", n)
	}

	if err := a.WorkerStop("nope"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("WorkerStop(unknown) err = %v, want a not-found error", err)
	}
}

func TestWorkerStartWithoutSenderResolver(t *testing.T) {
	cfg := testConfig(t)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer func() {
		_ = a.Close()
	}()

	if _, err := a.WorkerStart("TX", 1, time.Second); err == nil || err.Error() != "send command not found or has wrong type" {
		t.Errorf("WorkerStart without resolver err = %v, want the legacy send-not-found text", err)
	}
	if _, err := a.StressStart([]string{"TX"}, 10, time.Second, time.Second, 1); err == nil || err.Error() != "send command not found or has wrong type" {
		t.Errorf("StressStart without resolver err = %v, want the legacy send-not-found text", err)
	}
}

func TestStressStartValidation(t *testing.T) {
	sender := newFakeSender(0, true)
	a := newWorkerTestApp(t, sender)

	if _, err := a.StressStart([]string{"TX"}, 0, time.Second, time.Second, 1); err == nil {
		t.Error("StressStart with zero TPS returned no error")
	}
	if _, err := a.StressStart([]string{"TX"}, 10, time.Second, time.Second, 0); err == nil {
		t.Error("StressStart with zero workers returned no error")
	}
	if _, err := a.StressStart([]string{"TX"}, 10, time.Second, time.Second, 51); err == nil {
		t.Error("StressStart with 51 workers returned no error")
	}
}

func TestStressStatsIdle(t *testing.T) {
	sender := newFakeSender(0, true)
	a := newWorkerTestApp(t, sender)

	agg, err := a.StressStats()
	if err != nil {
		t.Fatalf("StressStats failed: %v", err)
	}
	if agg.Status != "idle" || agg.Sent != 0 {
		t.Errorf("idle aggregate = %+v, want status idle and zero totals", agg)
	}
	if _, err := a.StressSummaryByID("missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("StressSummaryByID(missing) err = %v, want a not-found error", err)
	}
}

func TestStressProgressThrottleBounds(t *testing.T) {
	// A slow steady sender must not publish faster than the documented
	// ~250 ms interval per worker.
	sender := newFakeSender(10*time.Millisecond, false)
	a := newWorkerTestApp(t, sender)

	ch, unsub := a.Events().Subscribe()
	defer unsub()

	id, err := a.StressStart([]string{"TX"}, 100, 20*time.Millisecond, 30*time.Millisecond, 1)
	if err != nil {
		t.Fatalf("StressStart failed: %v", err)
	}

	seq := drainWorkerEvents(t, ch, id)

	var progresses []events.WorkerProgress
	for _, ev := range seq {
		if p, ok := ev.(events.WorkerProgress); ok {
			progresses = append(progresses, p)
		}
	}
	if len(progresses) == 0 {
		t.Fatal("no WorkerProgress events observed")
	}
	// Rough bound: with a 10 ms send cadence over ~50 ms of ramp+hold
	// plus finish, at most a handful of throttled publishes fit even
	// ignoring the interval; assert far below one publish per send.
	if limit := sender.Calls()/2 + 3; len(progresses) > limit {
		t.Errorf("%d progress events for %d completions, throttle not applied", len(progresses), sender.Calls())
	}
}

func TestPercentileDuration(t *testing.T) {
	t.Parallel()

	t.Helper()

	tests := []struct {
		name     string
		sorted   []time.Duration
		pct      float64
		expected time.Duration
	}{
		{name: "empty slice", sorted: []time.Duration{}, pct: 0.5, expected: 0},
		{name: "pct <= 0.0", sorted: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, pct: -0.1, expected: 10 * time.Millisecond},
		{name: "pct >= 1.0", sorted: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, pct: 1.1, expected: 20 * time.Millisecond},
		{name: "single element", sorted: []time.Duration{15 * time.Millisecond}, pct: 0.5, expected: 15 * time.Millisecond},
		{name: "median exact match (odd count)", sorted: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond}, pct: 0.5, expected: 20 * time.Millisecond},
		{name: "median interpolation (even count)", sorted: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, pct: 0.5, expected: 15 * time.Millisecond},
		{name: "90th percentile interpolation", sorted: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond}, pct: 0.9, expected: 28 * time.Millisecond},
	}

	for _, tc := range tests {
		if actual := percentileDuration(tc.sorted, tc.pct); actual != tc.expected {
			t.Errorf("%s: expected %v, got %v", tc.name, tc.expected, actual)
		}
	}
}
