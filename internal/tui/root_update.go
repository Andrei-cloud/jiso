// root_update.go is the message router: which message family handles what.
// Each case delegates to the file that owns that flow; the ones that mean
// "the page wants the root to change the stack" are handled here. Cases
// that do not return fall through to forwarding to the page stack.
package tui

import (
	tea "charm.land/bubbletea/v2"
)

// errNoAppWired is what every action that needs the application layer says
// when the root was built without one (the zero-value model tests use).
const errNoAppWired = "no application wired"

// update routes one message through the global layer.
func (m *RootModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Send-wizard msgs are interpreted before anything else — they name
	// the leg the root must run (attempt, choose, send, cancel).
	if next, cmd, ok := m.handleWizardMsg(msg); ok {
		return next, cmd
	}

	// Family routers, in source order. Each returns (nil, nil) for a message
	// it does not own; message types do not overlap, so the first non-nil
	// result is the handler.
	if next, cmd := m.routeCoreMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeExchangeMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeSendConsoleMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeConnectResultMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeScenarioMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeServerMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeWorkerMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeSessionsMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeAnalyzeMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeAnalyzeApplyMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeSessionsLoadedMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeCtfMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeFilePickerMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeSettingsMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeStressResultMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeConfirmedMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeCancelledMsg(msg); next != nil {
		return next, cmd
	}
	if next, cmd := m.routeMouseMsg(msg); next != nil {
		return next, cmd
	}
	// Arrows, hjkl aliases, and unknown keys reach the page unharmed.
	return m.forward(msg)
}
