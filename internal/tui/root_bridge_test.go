package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app/events"
	"jiso/internal/tui/bridge"
	"jiso/internal/tui/pages"
)

// runCmdProgramEquivalently executes cmd the way v2's command runner does:
// a BatchMsg is unpacked and every sub-cmd runs in its own goroutine (the
// pump blocks there until a stop fires). Since the arming Update
// returns a BatchMsg (bridge cmd + resize flush cmd), so tests unpack like
// the program instead of assuming the returned cmd IS the pump.
func runCmdProgramEquivalently(cmd tea.Cmd) {
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			if sub != nil {
				go runCmdProgramEquivalently(sub)
			}
		}
	}
}

// armCollector returns a root with an event source + a sender that queues
// bridge messages. It proves the source→Cmd transition: the arming Update's
// bridge cmd is run here in a goroutine, so the pump is live and the caller
// only drives src and col.
func armCollector(t *testing.T) (*RootModel, chan events.Event, chan tea.Msg) {
	t.Helper()

	m := NewRootModel(nil)
	src := make(chan events.Event, 4)
	col := make(chan tea.Msg, 8)

	m.SetEventSender(func(msg tea.Msg) { col <- msg })
	m.SetEventSource(src)

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd == nil {
		t.Fatal("Update after SetEventSource returned no bridge cmd")
	}

	go runCmdProgramEquivalently(cmd)

	return m, src, col
}

func nextBridgeMsg(t *testing.T, col chan tea.Msg) bridge.Msg {
	t.Helper()

	select {
	case msg := <-col:
		bm, ok := msg.(bridge.Msg)
		if !ok {
			t.Fatalf("sender got %T, want bridge.Msg", msg)
		}

		return bm
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not deliver the published event")

		return bridge.Msg{}
	}
}

func TestSetEventSourceArmsBridgeOnce(t *testing.T) {
	t.Parallel()

	m, _, _ := armCollector(t)

	// Arming is one-shot: the next Update returns no further bridge cmd.
	_, cmd := m.Update(special('j'))
	if cmd != nil {
		t.Errorf("second Update re-armed the bridge (cmd %T)", cmd)
	}
	m.stopBridge()
}

func TestConnectionEventFlipsStatusSlot(t *testing.T) {
	t.Parallel()

	m, src, col := armCollector(t)
	defer m.stopBridge()

	src <- events.ConnectionEvent{State: events.StateConnected, Detail: "127.0.0.1:8583"}
	m.Update(nextBridgeMsg(t, col))

	if got := m.frameProps("body").Conn; got.Text != "connected" {
		t.Fatalf("conn slot after connect = %q, want %q", got.Text, "connected")
	}
	if v := m.View().Content; !strings.Contains(v, "connected") {
		t.Errorf("rendered frame lost the connected slot: %q", firstLines(v, 3))
	}

	src <- events.ConnectionEvent{State: events.StateDisconnected}
	m.Update(nextBridgeMsg(t, col))
	if got := m.frameProps("body").Conn; got.Text != "offline" {
		t.Errorf("conn slot after disconnect = %q, want %q", got.Text, "offline")
	}

	src <- events.ConnectionEvent{State: events.StateFailed, Detail: "connection refused"}
	m.Update(nextBridgeMsg(t, col))
	got := m.frameProps("body").Conn
	if !strings.Contains(got.Text, "failed") || !strings.Contains(got.Text, "connection refused") {
		t.Errorf("conn slot after failure = %q, want failed+detail", got.Text)
	}
}

func TestBridgeEventsForwardToTopPage(t *testing.T) {
	m, src, col := armCollector(t)
	defer m.stopBridge()

	m.Replace(recordingPage{id: "rec"})
	src <- events.Logf{Level: "info", Msg: "hi"}
	m.Update(nextBridgeMsg(t, col))

	// Pages receive pages.EventMsg (event + root-stamped time),
	// never the bridge wrapper — pages may not import the bridge package.
	for _, msg := range seenOf(m) {
		if em, ok := msg.(pages.EventMsg); ok {
			if ev, ok := em.Event.(events.Logf); !ok || ev.Msg != "hi" {
				t.Errorf("forwarded event = %+v", em.Event)
			}
			if em.Time.IsZero() {
				t.Errorf("EventMsg time not stamped by root: %v", em.Time)
			}

			return
		}
	}
	t.Error("bridge event never reached the top page as pages.EventMsg")
}

func TestConnSegmentMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		ev    events.ConnectionEvent
		want  string
		plain bool
	}{
		{events.ConnectionEvent{State: events.StateConnected}, "connected", false},
		{events.ConnectionEvent{State: events.StateDisconnected}, "offline", false},
		{events.ConnectionEvent{State: events.StateFailed, Detail: "boom"}, "failed: boom", false},
		{events.ConnectionEvent{State: "weird"}, "offline", false},
	}
	for _, tc := range cases {
		seg := connSegment(tc.ev)
		if seg.Text != tc.want {
			t.Errorf("connSegment(%+v).Text = %q, want %q", tc.ev, seg.Text, tc.want)
		}
		if seg.Plain != tc.plain {
			t.Errorf("connSegment(%+v).Plain = %v, want %v", tc.ev, seg.Plain, tc.plain)
		}
	}
}

func TestBridgeNilSourceIsNoop(t *testing.T) {
	m := NewRootModel(nil)
	m.SetEventSource(nil)
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.bridge != nil {
		t.Error("nil source armed a bridge")
	}

	// The only cmd a nil-source resize Update may return is the resize
	// flush (coalescing) — never a bridge pump.
	if cmd != nil {
		if _, ok := cmd().(resizeFlushMsg); !ok {
			t.Errorf("nil source armed something: %T", cmd)
		}
	}
}

// firstLines returns the first n rendered lines for failure messages.
func firstLines(s string, n int) string {
	return strings.Join(strings.SplitN(s, "\n", n+1)[:min(n, strings.Count(s, "\n")+1)], "\n")
}

// TestStatusStripCurrentTruth: every connection state change stamps a
// fresh timestamped line, so a stale "Connection closed" can never
// outlive the reconnect(the strip lied across sessions).
func TestStatusStripCurrentTruth(t *testing.T) {
	t.Parallel()

	m, src, col := armCollector(t)
	defer m.stopBridge()
	m.now = func() time.Time {
		return time.Date(2026, 9, 11, 9, 22, 3, 0, time.UTC)
	}

	src <- events.ConnectionEvent{State: events.StateDisconnected, Detail: "127.0.0.1:9999"}
	m.Update(nextBridgeMsg(t, col))
	line, _ := m.consoleLine()
	if !strings.Contains(line, "disconnected") || !strings.HasPrefix(line, "09:22:03 ") {
		t.Fatalf("strip after disconnect = %q, want the timestamped truth", line)
	}

	m.now = func() time.Time {
		return time.Date(2026, 9, 11, 9, 25, 41, 0, time.UTC)
	}
	src <- events.ConnectionEvent{State: events.StateConnected, Detail: "127.0.0.1:9999"}
	m.Update(nextBridgeMsg(t, col))
	line, _ = m.consoleLine()
	if !strings.HasPrefix(line, "09:25:41 ") || !strings.Contains(line, "connected") {
		t.Fatalf("strip after reconnect = %q, want the fresh connected line", line)
	}
}
