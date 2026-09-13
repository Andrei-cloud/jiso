package bridge

import (
	"context"
	"runtime"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app/events"
)

// collect returns a sender appending to a slice guarded for the test's
// single reader, plus a poll helper.
type collector struct {
	msgs chan tea.Msg
}

func newCollector(n int) *collector {
	return &collector{msgs: make(chan tea.Msg, n)}
}

func (c *collector) send(msg tea.Msg) { c.msgs <- msg }

func (c *collector) next(t *testing.T) tea.Msg {
	t.Helper()

	select {
	case msg := <-c.msgs:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for bridge message")

		return nil
	}
}

// startPump runs the bridge Cmd the way the program does: in its own
// goroutine, blocking there until a stop fires. The returned func joins
// that goroutine (fails if it does not exit within 2s).
func startPump(t *testing.T, b *Bridge) func() {
	t.Helper()

	cmd := b.Cmd()
	if cmd == nil {
		t.Fatal("Cmd() = nil, want startable command")
	}
	exited := make(chan struct{})

	go func() { cmd(); close(exited) }()

	return func() {
		t.Helper()
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
			t.Error("pump goroutine did not exit after stop")
		}
	}
}

// assertNoLeak waits for the goroutine count to return to (or below) the
// baseline captured before the bridge ran.
func assertNoLeak(t *testing.T, baseline int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > baseline {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			buf = buf[:runtime.Stack(buf, true)]

			t.Fatalf("goroutine leak: baseline %d, now %d\n%s", baseline, runtime.NumGoroutine(), buf)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBridgePumpsEventsToInjectedSender(t *testing.T) {
	t.Parallel()

	src := make(chan events.Event, 4)
	col := newCollector(4)
	b := New(context.Background(), src, col.send)

	startPump(t, b)
	defer b.Stop()

	want := WorkerProbe
	src <- want

	msg := col.next(t)
	bm, ok := msg.(Msg)
	if !ok {
		t.Fatalf("sender got %T, want Msg", msg)
	}
	if bm.Event != want {
		t.Errorf("event = %+v, want %+v", bm.Event, want)
	}
}

// WorkerProbe is a package-internal fixture event.
var WorkerProbe = events.WorkerProgress{ID: "w1", Done: 2, Total: 5}

func TestBridgeMsgTypeIntegrity(t *testing.T) {
	t.Parallel()

	src := make(chan events.Event, 4)
	col := newCollector(4)
	b := New(context.Background(), src, col.send)
	startPump(t, b)
	defer b.Stop()

	want := []events.Event{
		events.ConnectionEvent{State: events.StateConnected, Detail: "127.0.0.1:8583"},
		events.WorkerStarted{ID: "w1", Kind: "stress"},
		events.Logf{Level: "warn", Msg: "slow"},
	}
	for _, ev := range want {
		src <- ev
	}
	for i, w := range want {
		bm, ok := col.next(t).(Msg)
		if !ok {
			t.Fatalf("msg %d not Msg", i)
		}
		switch ev := bm.Event.(type) {
		case events.ConnectionEvent:
			wantEv, ok := w.(events.ConnectionEvent)
			if !ok {
				t.Fatalf("want[%d] = %T, want events.ConnectionEvent", i, w)
			}
			if ev != wantEv {
				t.Errorf("event %d = %+v", i, ev)
			}
		case events.WorkerStarted:
			wantEv, ok := w.(events.WorkerStarted)
			if !ok {
				t.Fatalf("want[%d] = %T, want events.WorkerStarted", i, w)
			}
			if ev != wantEv {
				t.Errorf("event %d = %+v", i, ev)
			}
		case events.Logf:
			wantEv, ok := w.(events.Logf)
			if !ok {
				t.Fatalf("want[%d] = %T, want events.Logf", i, w)
			}
			if ev != wantEv {
				t.Errorf("event %d = %+v", i, ev)
			}
		default:
			t.Errorf("event %d lost concrete type: %T", i, bm.Event)
		}
	}
}

func TestBridgeStopNoGoroutineLeak(t *testing.T) {
	t.Parallel()

	base := runtime.NumGoroutine()
	src := make(chan events.Event)
	col := newCollector(1)
	b := New(context.Background(), src, col.send)
	join := startPump(t, b)

	src <- WorkerProbe // pump definitely live
	col.next(t)

	b.Stop()
	join()
	assertNoLeak(t, base)
}

func TestBridgeContextCancelStopsPump(t *testing.T) {
	t.Parallel()

	base := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	src := make(chan events.Event)
	col := newCollector(1)
	b := New(ctx, src, col.send)
	join := startPump(t, b)

	src <- WorkerProbe
	col.next(t)

	cancel()
	join()
	assertNoLeak(t, base)
}

func TestBridgeSourceCloseStopsPump(t *testing.T) {
	t.Parallel()

	base := runtime.NumGoroutine()
	src := make(chan events.Event)
	col := newCollector(1)
	b := New(context.Background(), src, col.send)
	join := startPump(t, b)

	close(src)
	join() // source close must end the pump without Stop/ctx cancel
	assertNoLeak(t, base)
}

func TestBridgeCmdStartsPumpOnce(t *testing.T) {
	t.Parallel()

	src := make(chan events.Event, 1)
	col := newCollector(4)
	b := New(context.Background(), src, col.send)
	startPump(t, b) // a model arms once; double-pump would deliver twice
	defer b.Stop()

	src <- WorkerProbe
	col.next(t)

	select {
	case extra := <-col.msgs:
		t.Errorf("double pump delivered extra %v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBridgeNilSenderIsSink(t *testing.T) {
	t.Parallel()

	src := make(chan events.Event, 1)
	b := New(context.Background(), src, nil)
	startPump(t, b)
	defer b.Stop()

	src <- WorkerProbe
	time.Sleep(20 * time.Millisecond) // sending into the sink must not panic
}
