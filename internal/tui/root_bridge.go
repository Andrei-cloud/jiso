package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app/events"
	"jiso/internal/tui/bridge"
	"jiso/internal/tui/frame"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
)

// SetEventSource installs the bus channel (App.Events.Subscribe) as the
// event source. The bridge is not started here — the next Update arms it as
// a tea.Cmd, keeping this call I/O-free and Update-pure-testable. Passing
// nil disarms a pending source.
func (m *RootModel) SetEventSource(ch <-chan events.Event) {
	m.eventSrc, m.bridgePending = ch, ch != nil
}

// SetEventSender overrides how bridge messages reach the program. Run wires
// (*tea.Program).Send; tests inject a collector and re-enter Update by
// hand, proving the pump→msg→status plumbing without a real program.
func (m *RootModel) SetEventSender(send bridge.Sender) { m.eventSender = send }

// setEventWiring binds the bridge's stop context — the caller ctx passed
// to Run, so caller-side cancellation stops the pump; program-initiated exit
// (q, error, recovered panic) is covered by run's stopBridge, because v2
// does not cancel the caller ctx. Used by run only.
func (m *RootModel) setEventWiring(ctx context.Context, send bridge.Sender) {
	m.eventCtx, m.eventSender = ctx, send
}

// armBridgeCmd builds the bridge and its start Cmd if a source is pending.
func (m *RootModel) armBridgeCmd() tea.Cmd {
	if !m.bridgePending {
		return nil
	}
	m.bridgePending = false
	m.bridge = bridge.New(m.eventCtx, m.eventSrc, m.eventSender)
	m.debug.logf("bridge start")

	return m.bridge.Cmd()
}

// stopBridge stops the pump goroutine; Run calls it after the program exits
// so no bridge goroutine outlives the session (ctx cancellation is the
// primary stop, this is the belt).
func (m *RootModel) stopBridge() {
	if m.bridge != nil {
		m.bridge.Stop()
		m.debug.logf("bridge stop")
	}
}

// updateBridgeMsg applies one bus event: ConnectionEvent flips the frame's
// connection slot (the slot's authoritative source while the bridge is
// live) and (dis)arms the uptime clock, and every event is forwarded to the
// top page as a pages.EventMsg stamped with the root's injectable clock —
// screens never import internal/tui/bridge and never read the clock
// themselves (data-flow contract).
func (m *RootModel) updateBridgeMsg(msg bridge.Msg) (tea.Model, tea.Cmd) {
	now := m.now()
	// The snapshot must reflect this event even when the event itself is
	// the last message before a View (no Update wraps it).
	defer m.syncDashboard()
	defer m.syncWorkers()
	if ev, ok := msg.Event.(events.ConnectionEvent); ok {
		c := ev
		m.conn = &c
		if ev.State == events.StateConnected {
			m.connSince = &now
			// A user-issued attempt that reached its Connected truth is over;
			// every later bus failure is a background flap — chip, no modal.
			m.connectInitiated = false
		} else {
			m.connSince = nil
		}
		// The strip must always show the CURRENT truth;
		// every state change stamps a fresh line over any stale one.
		m.stampConnStatus(ev)
	}
	// The worker events' designed consumer is root, not the
	// page — the row cache folds them in here (before the top page ever
	// sees the forwarded EventMsg), so the §H table updates on the bus
	// alone, with no tick and no polling, whichever page is current.
	switch ev := msg.Event.(type) {
	case events.WorkerStarted:
		m.onWorkerStarted(ev, now)
	case events.WorkerProgress:
		m.onWorkerProgress(ev, now)
	case events.WorkerStopped:
		m.onWorkerStopped(ev, now)
		// Every bgsend completion lands in the session DB —
		// mark the §I cache dirty (the query itself only ever runs
		// while the page is current, off the UI thread).
		// The §A SESSION card's async read is dirtied the same way.
		m.sessionsDirty = true
		m.sessionStatsDirty = true
	}

	return m.forward(pages.EventMsg{Event: msg.Event, Time: now})
}

// connSegment maps a ConnectionEvent to the frame's connection slot:
// symbol+text pairs from theme kinds, failure detail never dropped.
func connSegment(ev events.ConnectionEvent) frame.Segment {
	switch ev.State {
	case events.StateConnected:
		return frame.Segment{Kind: theme.KindOK, Text: "connected"}
	case events.StateFailed:
		text := "failed"
		if ev.Detail != "" {
			text += ": " + ev.Detail
		}

		return frame.Segment{Kind: theme.KindError, Text: text}
	default:
		return frame.Segment{Kind: theme.KindError, Text: "offline"}
	}
}
