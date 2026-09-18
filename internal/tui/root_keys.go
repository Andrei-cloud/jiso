// root_keys.go is the keyboard: which key does what, in priority order.
// The long switch is deliberate — the order keys are tested in is part of
// the behaviour. Flows that outlive the keypress live in their own file
// (connect, send, workers, settings).
package tui

import (
	"jiso/internal/tui/pages"

	tea "charm.land/bubbletea/v2"
)

// updateKey applies the global layer, then routes.
func (m *RootModel) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	km := &m.keys

	// Ctrl+C arrives as a key press in raw mode; the design contract maps it
	// to a graceful tea.Quit (runtime restores the terminal). Ctrl+Z and
	// Ctrl+\ stay unbound below and are forwarded untouched.
	if keyMatches(msg, km.GracefulExit) {
		return m, tea.Quit
	}

	// The error screen is the topmost overlay — View draws it last and
	// this leg is the first check, so it owns the keyboard over every
	// other overlay (only toasts compose above it, and they own no
	// keys): enter/esc close the screen only (the overlay below keeps
	// its state and receives keys again), j/k and pgup/pgdown scroll
	// the body, and every other key is swallowed so the page
	// (page jumps included) stays frozen.
	if m.errModal != nil {
		if m.errModal.UpdateKey(msg) {
			m.errModal = nil
			m.debug.logf("error modal close")
		}

		return m, nil
	}

	if m.pal != nil {
		return m.updatePalette(msg)
	}

	// Root-owned modal overlays take the keyboard in the order below:
	// while one is open its keys edit the form, never quit or jump
	// pages. The file picker, opened from a form's [f] browse, sits on
	// top of its form and owns the keys first.
	if m.dlg != nil {
		return m.updateConnectDialog(msg)
	}

	// The send wizard.
	if m.wizard != nil && m.filePick == nil {
		return m.updateWizardKey(msg)
	}

	// The §G server start form.
	if m.serverDlg != nil && m.filePick == nil {
		return m.updateServerFormKey(msg)
	}

	// The §H worker start wizard.
	if m.workerWiz != nil && m.filePick == nil {
		return m.updateWorkerWizKey(msg)
	}

	if next, cmd, ok := m.handleConfirmKey(msg); ok {
		return next, cmd
	}

	if next, cmd, ok := m.handlePickOrHelpKey(msg); ok {
		return next, cmd
	}

	// A page in a page-local text-input mode claims EVERY key while a
	// field is being typed into — no global may fire mid-typing (esc
	// leaves the field first). Ctrl+C was claimed above and stays global.
	if kc, ok := m.Current().(pages.KeyboardClaimer); ok && kc.ClaimsKeyboard() {
		return m.forward(msg)
	}

	return m.handleGlobalKey(msg)
}

// handleGlobalKey applies the global key bindings: the page-jump digits, then the
// help/palette/connect/send/pane-focus/quit switch, defaulting to forwarding the
// key to the current page.
func (m *RootModel) handleGlobalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	km := &m.keys

	for i := range km.PageJumps {
		if keyMatches(msg, km.PageJumps[i]) {
			m.jumpTo(i)

			return m, nil
		}
	}

	switch {
	case keyMatches(msg, km.MouseToggle):
		// Release/re-arm terminal mouse reporting so text can be
		// click-drag selected (gates: hitmap_mouse.go / hitmap.go). F9
		// is global here, but edit-mode fields claim it first above.
		m.mouseEnabled = !m.mouseEnabled
		m.debug.logf("mouse enabled=%v", m.mouseEnabled)

		return m, nil

	case keyMatches(msg, km.Help):
		// `?` opens the §M overlay with the current page's context.
		m.openHelp()

		return m, nil

	case keyMatches(msg, km.Palette):
		m.pal = m.newPalette()
		m.debug.logf("palette open")

		return m, nil

	case keyMatches(msg, km.Connect):
		// "c" opens the modal on the current page.
		// Exception: on the §G page it opens the server start form.
		if m.Current().ID() == pages.ServerPageID {
			return m.openServerForm()
		}
		// While a connection is live "c" drops it instead
		// (reconnect = disconnect, then c again).
		if m.connectionLive() {
			return m.handleDisconnect()
		}

		return m.openConnect()

	case keyMatches(msg, km.Send) && m.Current().ID() == pages.DashboardPageID:
		// "s" on the dashboard sends directly when the session config is
		// complete, otherwise opens the send wizard; everywhere else "s"
		// stays page-local (§B send, §G stop, §K step pick).
		return m.directSend()

	case keyMatches(msg, km.PaneFocus):
		return m.forward(PaneFocusMsg{})

	case keyMatches(msg, km.PaneFocusBack):
		return m.forward(PaneFocusMsg{Reverse: true})

	case keyMatches(msg, km.Quit):
		if m.StackDepth() > 1 {
			m.Pop()

			return m, nil
		}
		// Quitting always confirms first (default No);
		// Ctrl+C stays the immediate graceful exit.
		return m.requestQuit()

	default:
		// Arrows, hjkl aliases, and unknown keys reach the page unharmed.
		return m.forward(msg)
	}
}

// handleConfirmKey routes a key press to whichever root-owned confirm dialog is
// pending, reporting whether one consumed it. While a confirm is pending only
// y/n/Esc/Enter have meaning; everything else (including page jumps) is swallowed.
func (m *RootModel) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if m.serverConfirm != nil && m.serverConfirm.Pending() {
		next, cmd := m.updateServerConfirmKey(msg)

		return next, cmd, true
	}
	if m.workersConfirm != nil && m.workersConfirm.Pending() {
		_, cmd := m.workersConfirm.Update(msg)

		return m, cmd, true
	}
	if m.analyzeConfirm != nil && m.analyzeConfirm.Pending() {
		// The §J abort-over-in-flight confirm.
		_, cmd := m.analyzeConfirm.Update(msg)

		return m, cmd, true
	}
	if m.ctfConfirm != nil && m.ctfConfirm.Pending() {
		// The §K overwrite confirm (default No — nothing is written).
		_, cmd := m.ctfConfirm.Update(msg)

		return m, cmd, true
	}
	if m.analyzeOverwriteConfirm != nil && m.analyzeOverwriteConfirm.Pending() {
		// The analyze overwrite confirm (default No — the existing
		// item set stays intact).
		_, cmd := m.analyzeOverwriteConfirm.Update(msg)

		return m, cmd, true
	}
	if m.scenarioConfirm != nil && m.scenarioConfirm.Pending() {
		// The scenario-export overwrite confirm (default No — the
		// previous report stays).
		_, cmd := m.scenarioConfirm.Update(msg)

		return m, cmd, true
	}
	if m.disconnectConfirm != nil && m.disconnectConfirm.Pending() {
		// The disconnect confirm (default No — the connection stays up).
		_, cmd := m.disconnectConfirm.Update(msg)

		return m, cmd, true
	}

	return m, nil, false
}

// handlePickOrHelpKey routes a key press to the file picker or the help
// overlay when either is open, reporting whether one consumed it. The
// picker owns every key while open; the page behind it stays frozen.
func (m *RootModel) handlePickOrHelpKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	// Esc reaches the widget itself, which emits its own cancel msg.
	if m.filePick != nil {
		_, cmd := m.filePick.Update(msg)

		return m, cmd, true
	}

	// The §M overlay: `?` toggles it, Esc closes first;
	// every other key is swallowed.
	if m.help != nil {
		if keyMatches(msg, m.keys.Help) || msg.Code == tea.KeyEscape {
			m.help = nil
			m.debug.logf("help close")
		} else {
			m.debug.logf("help swallow key")
		}

		return m, nil, true
	}

	return m, nil, false
}
