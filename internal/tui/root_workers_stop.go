// root_workers_stop.go owns the §H stop paths (SCR-508). `k` stops the
// selected worker through the App manager (WorkerStop — the same entry
// the CLI shim drives); a terminal row is a no-op with a status line and
// NO App call. `K` stop-all asks the §N3 confirm while any worker is
// active (default No — the SCR-507 ConfirmDialog pattern); quitting with
// active workers reuses the same confirm (confirmQuit marks it). Stops
// run as tea.Cmds (WorkerStop may block up to the App's stop timeout);
// the table flip is event-driven — nothing writes a terminal status
// before WorkerStopped arrives on the bus.
package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/widgets"
)

// workerStopResultMsg closes one single-worker stop attempt; the row's
// terminal status comes from the bus, this only carries errors.
type workerStopResultMsg struct {
	id  string
	err error
}

// workerStopAllResultMsg closes a stop-all attempt.
type workerStopAllResultMsg struct{ err error }

// handleWorkerStop is the `k` path: terminal rows never reach the App
// (no-op + status line); live rows return the stop Cmd.
func (m *RootModel) handleWorkerStop(id string) (tea.Model, tea.Cmd) {
	r, ok := m.workerRows[id]
	if !ok {
		m.workersStatus = "worker '" + id + "' not found"

		return m, nil
	}
	if r.terminal {
		m.workersStatus = id + " already " + r.status + " — nothing to stop"
		m.debug.logf("worker stop ignored id=%s status=%s", id, r.status)

		return m, nil
	}
	stop := m.workerStopFn
	if stop == nil {
		if m.app == nil {
			m.workersStatus = errNoAppWired

			return m, nil
		}
		stop = m.app.WorkerStop
	}
	m.workersStatus = ""
	m.debug.logf("worker stop requested id=%s", id)

	return m, func() tea.Msg { return workerStopResultMsg{id: id, err: stop(id)} }
}

// handleWorkerStopAll is the `K` path: with active workers it opens the
// §N3 confirm (default No); with none it is a status-line no-op.
func (m *RootModel) handleWorkerStopAll() (tea.Model, tea.Cmd) {
	n := m.activeWorkerCount()
	if n == 0 {
		m.workersStatus = "no active workers — nothing to stop"
		m.debug.logf("worker stop-all ignored (none active)")

		return m, nil
	}
	m.confirmQuit = false
	m.workersConfirm = widgets.NewConfirmDialog(m.themeOrNil(),
		"stop "+strconv.Itoa(n)+" active worker(s)?")
	m.debug.logf("worker stop-all confirm active=%d", n)

	return m, nil
}

// applyWorkersConfirmed is the y path: a pending quit quits, a pending
// stop-all returns the stop-all Cmd (rows flip as their events land).
// requestQuit arms the quit confirmation (default No). The worker count
// only shapes the message; quitting is destructive even with none (UAT).
func (m *RootModel) requestQuit() (tea.Model, tea.Cmd) {
	if m.workersConfirm != nil && m.workersConfirm.Pending() {
		return m, nil // already confirming
	}
	text := "quit jiso?"
	if n := m.activeWorkerCount(); n > 0 {
		text = "quit with " + strconv.Itoa(n) + " active worker(s)?"
	}
	m.confirmQuit = true
	m.workersConfirm = widgets.NewConfirmDialog(m.themeOrNil(), text)

	return m, nil
}

func (m *RootModel) applyWorkersConfirmed() (tea.Model, tea.Cmd) {
	if m.workersConfirm == nil {
		return m, nil // straggler after a pop
	}
	m.workersConfirm = nil

	if m.confirmQuit {
		m.confirmQuit = false
		m.debug.logf("quit confirmed with workers")

		return m, tea.Quit
	}

	stopAll := m.workerStopAllFn
	if stopAll == nil {
		if m.app == nil {
			m.workersStatus = errNoAppWired

			return m, nil
		}
		stopAll = m.app.WorkerStopAll
	}
	m.workersStatus = ""
	m.debug.logf("worker stop-all requested")

	return m, func() tea.Msg { return workerStopAllResultMsg{err: stopAll()} }
}

// applyWorkersCancelled is the n/Esc/Enter (default No) path: nothing
// is stopped and the confirm closes.
func (m *RootModel) applyWorkersCancelled() (tea.Model, tea.Cmd) {
	if m.workersConfirm == nil {
		return m, nil
	}
	m.confirmQuit = false
	m.workersConfirm = nil
	m.debug.logf("worker stop-all canceled")

	return m, nil
}

// applyWorkerStopResult records stop failures on the status line; the
// success path needs no write (the WorkerStopped event owns the flip).
func (m *RootModel) applyWorkerStopResult(msg workerStopResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.workersStatus = "stop " + msg.id + ": " + msg.err.Error()
		m.debug.logf("worker stop failed id=%s err=%v", msg.id, msg.err)
	}

	return m, nil
}

// applyWorkerStopAllResult records the stop-all failure line, if any.
func (m *RootModel) applyWorkerStopAllResult(msg workerStopAllResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.workersStatus = "stop all: " + msg.err.Error()
		m.debug.logf("worker stop-all failed err=%v", msg.err)
	}

	return m, nil
}
