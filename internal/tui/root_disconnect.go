// root_disconnect.go owns the disconnect operation (closes
// REGRESSION-1: the TUI could not drop a live connection without quitting
// or reconnecting — REPL `disconnect` parity). The palette action and the
// §A "D" quick key both emit palette.DisconnectMsg and land on
// handleDisconnect — the router owns every decision (the page never
// touches the app). The leg runs like the §G stop Cmd: App.Disconnect is
// called off the UI thread through a tea.Cmd and reports back as a
// seq-tokened disconnectResultMsg (the serverTickSeq lifecycle): a page
// leave (Push/Replace → leaveDisconnect) bumps the seq, so an orphaned
// result is dropped WITHOUT re-arming the wait flag — the wedge class
// stays closed because applyDisconnectResult clears the flag before the
// stale check. Success folds as a toast only: App.Disconnect publishes
// the Disconnected ConnectionEvent, and updateBridgeMsg already owns the
// card flip (no double-render here). §N3: with workers active or the
// serve engine running the widgets.ConfirmDialog opens first (default
// No — the same liveness checks root_workers.go/root_server.go use).
package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app/events"
	"jiso/internal/tui/widgets"
)

// disconnectResultMsg closes one disconnect leg; seq marks the arming
// generation (the serverTickMsg pattern: a stale seq marks the leg
// orphaned by a page leave and the message is ignored).
type disconnectResultMsg struct {
	seq uint64
	err error
}

// connectionLive is the root-side connection truth for the disconnect
// gate — the same precedence the dashboard card uses: the latest bus
// ConnectionEvent wins, the app snapshot is the fallback.
func (m *RootModel) connectionLive() bool {
	if m.conn != nil {
		return m.conn.State == events.StateConnected
	}
	if m.app != nil {
		return m.app.IsConnected()
	}

	return false
}

// handleDisconnect is the shared entry point (palette action + §A "D"):
// single-flight while a leg runs; without a connection a sane no-op with
// an info toast (the REPL's "no active connection" honesty); with
// workers active or the serve engine running the §N3 confirm opens first
// (default No); otherwise the leg runs directly.
func (m *RootModel) handleDisconnect() (tea.Model, tea.Cmd) {
	if m.disconnectWait {
		return m, nil // one leg at a time (the sendGen single-flight rule)
	}
	if !m.connectionLive() {
		m.pushToast("no active connection — nothing to disconnect", widgets.ToastInfo)
		m.debug.logf("disconnect ignored (no connection)")

		return m, nil
	}

	workers := m.activeWorkerCount()
	if workers > 0 || m.serverRunning() {
		conds := make([]string, 0, 2)
		if workers > 0 {
			conds = append(conds, strconv.Itoa(workers)+" active worker(s)")
		}
		if m.serverRunning() {
			conds = append(conds, "mock server :"+m.serverPort+" running")
		}
		m.disconnectConfirm = widgets.NewConfirmDialog(m.themeOrNil(),
			"disconnect with "+strings.Join(conds, " and ")+"?")
		m.debug.logf("disconnect confirm %s", strings.Join(conds, " and "))

		return m, nil
	}

	return m.performDisconnect()
}

// performDisconnect arms the leg: App.Disconnect (or the injected
// disconnectFn seam — the same WorkerStop-style façade override the root
// tests use) runs off the UI thread and reports back as the seq-tokened
// result msg. Without an app and without a seam the honest failure line
// renders instead of a nil dereference.
func (m *RootModel) performDisconnect() (tea.Model, tea.Cmd) {
	disconnect := m.disconnectFn
	if disconnect == nil {
		if m.app == nil {
			m.pushToast("disconnect: no application wired", widgets.ToastError)

			return m, nil
		}
		disconnect = m.app.Disconnect
	}

	seq := m.disconnectSeq
	m.disconnectWait = true
	m.debug.logf("disconnect requested seq=%d", seq)

	return m, func() tea.Msg { return disconnectResultMsg{seq: seq, err: disconnect()} }
}

// applyDisconnectResult folds one leg. THE WEDGE CONTRACT: the wait flag
// is cleared BEFORE the stale check, so a leg orphaned by a page leave
// can never leave the action permanently "in flight" (the class of bug
// the leave-side seq bumps exist to close). Success is toast-only — the
// card flip rides the Disconnected event App.Disconnect publishes through
// updateBridgeMsg (the single connection-card path; no double-render).
func (m *RootModel) applyDisconnectResult(msg disconnectResultMsg) (tea.Model, tea.Cmd) {
	m.disconnectWait = false
	if msg.seq != m.disconnectSeq {
		m.debug.logf("disconnect result dropped seq=%d current=%d", msg.seq, m.disconnectSeq)

		return m, nil
	}
	if msg.err != nil {
		m.pushToast("disconnect: "+msg.err.Error(), widgets.ToastError)
		m.debug.logf("disconnect failed: %v", msg.err)

		return m, nil
	}

	m.pushToast("disconnected", widgets.ToastSuccess)
	m.debug.logf("disconnect ok")

	return m, nil
}

// applyDisconnectConfirmed is the y path of the §N3 confirm: the leg the
// confirm deferred is armed now.
func (m *RootModel) applyDisconnectConfirmed() (tea.Model, tea.Cmd) {
	if m.disconnectConfirm == nil {
		return m, nil // straggler after a pop
	}
	m.disconnectConfirm = nil

	return m.performDisconnect()
}

// applyDisconnectCancelled is the n/Esc/Enter (default No) path: the
// connection stays up and no leg is ever armed.
func (m *RootModel) applyDisconnectCancelled() (tea.Model, tea.Cmd) {
	if m.disconnectConfirm == nil {
		return m, nil
	}
	m.disconnectConfirm = nil
	m.debug.logf("disconnect canceled")

	return m, nil
}

// leaveDisconnect retires the disconnect leg on navigation (the
// leave-side cancel pattern, unconditional because
// the leg is connection-level, not §A-owned): the seq bump turns an
// in-flight leg stale, and the wait flag plus any pending confirm clear
// with it — a stale result arriving after the leave is dropped by
// applyDisconnectResult with the flag already false.
func (m *RootModel) leaveDisconnect() {
	m.disconnectSeq++
	m.disconnectWait = false
	m.disconnectConfirm = nil
}
