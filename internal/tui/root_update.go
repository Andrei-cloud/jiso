// root_update.go is the message router: which message family handles what. Each
// case delegates to the file that owns that flow (connect, send, scenarios, server,
// workers, sessions, analyze, ctf, settings); the ones that mean "the page wants
// the root to change the stack" are handled here because nothing else can.
//
// It is a type switch over message families with a default of forward, so a case
// that does not return falls through to forwarding the message to the page stack:
// adding a case here is also a decision about whether the root keeps that message.
package tui

import (
	tea "charm.land/bubbletea/v2"
)

// errNoAppWired is what every action that needs the application layer says when
// the root was built without one (the zero-value model the page tests construct).
// Spelled at six sites, an operator would have seen five wordings of the same
// "this build has no app" once anyone reworded one of them.
const errNoAppWired = "no application wired"

// the root was built without one (the zero-value model the page tests construct).
// Spelled at six sites, an operator would have seen five different wordings of the
// same "this build has no app" once anyone reworded one of them.
// update routes one message through the global layer.
func (m *RootModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Send-wizard msgs (proposal 04 §B) are interpreted before anything
	// else — they are emitted by the wizard's own Update and name the
	// leg the root must run (attempt, choose, send, cancel).
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
