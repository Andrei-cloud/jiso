// worker_wizard_keys.go is §H's worker-form keyboard: field cursor, stepping, the
// value editor, and which Enter means "start the worker" rather than "next field".
// The form's fields and their validation are in worker_wizard.go; this file is only
// the key routing between them.
package pages

import (
	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Update routes sizes and keys; every key reaches here (the root's
// modal branch forwards the keyboard wholesale while the wizard is
// open, like the send wizard's).
func (w *WorkerWizard) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		w.width, w.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return w.updateKey(msg)
	}

	return w, nil
}

// updateKey routes one key: an in-flight start leg freezes the wizard
// (Esc alone still closes it, as the retired forms did); the tx and
// param steps own their editors; Enter advances through the step's
// validation and Esc backs one step (closing on step 1).
func (w *WorkerWizard) updateKey(msg tea.KeyPressMsg) (Modal, tea.Cmd) {
	if w.state.InFlight {
		if key.Matches(msg, w.nav.Cancel) {
			return w, emitMsg(WorkerWizardCloseMsg{})
		}

		return w, nil // leg in flight: the progress line is the only output
	}

	switch w.step {
	case WorkerStepTx:
		return w.updateTxStep(msg)
	case WorkerStepParams:
		return w.updateParams(msg)
	case WorkerStepRun:
		return w.updateRun(msg)
	}

	return w, nil
}

// updateTxStep edits step 1: an open "/" filter owns the printables
// (the SCR-502 lesson — j and k included go into the filter); with the
// filter closed the cursor moves (auto-scrolling the 5-row window),
// space toggles a stress row, "/" opens the filter, [f] browses, and
// Enter commits the selection into step 2.
func (w *WorkerWizard) updateTxStep(msg tea.KeyPressMsg) (Modal, tea.Cmd) {
	if w.filtering {
		switch {
		case key.Matches(msg, w.nav.Cancel):
			// Esc closes the filter first (back's contract); the
			// second press walks the wizard.
			return w.back()
		case key.Matches(msg, w.nav.Enter):
			return w.commitTx()
		case key.Matches(msg, w.nav.Backspace):
			w.draft = dropLastRune(w.draft)
			w.clampSel()
			w.clampTop()

			return w, nil
		}
		if r, ok := printableRune(msg.Text); ok {
			w.draft += string(r)
			w.clampSel()
			w.clampTop()

			return w, nil
		}

		return w, nil
	}

	switch {
	case key.Matches(msg, w.nav.Cancel):
		return w.back()
	case key.Matches(msg, w.nav.Enter):
		return w.commitTx()
	case key.Matches(msg, w.nav.Up):
		w.moveSel(-1)
	case key.Matches(msg, w.nav.Down):
		w.moveSel(1)
	case key.Matches(msg, w.nav.Space):
		if w.mode == WorkerModeStress {
			w.toggleSel()
		}
	case key.Matches(msg, w.nav.Filter):
		w.filtering = true
	case key.Matches(msg, w.nav.Browse):
		return w, emitMsg(WorkerWizardBrowseMsg{})
	}

	return w, nil
}

// updateParams edits step 2: up/down move the row focus (code-only, so
// j/k stay typeable into duration values), printables extend the
// focused value, backspace trims it, and Enter advances only when the
// whole field set resolves.
func (w *WorkerWizard) updateParams(msg tea.KeyPressMsg) (Modal, tea.Cmd) {
	keys := w.paramKeys()
	switch {
	case key.Matches(msg, w.nav.Cancel):
		return w.back()
	case key.Matches(msg, w.nav.Enter):
		if _, msg := w.resolveRun(); msg != "" {
			w.paramErr = msg

			return w, nil
		}
		w.paramErr = ""
		w.setStep(WorkerStepRun)

		return w, nil
	case key.Matches(msg, w.nav.FieldUp):
		w.focus = max(w.focus-1, 0)
	case key.Matches(msg, w.nav.FieldDown):
		w.focus = min(w.focus+1, len(keys)-1)
	case key.Matches(msg, w.nav.Backspace):
		key := keys[min(w.focus, len(keys)-1)]
		w.params[key] = dropLastRune(w.params[key])
		w.paramErr = ""
	default:
		// Space never belongs in a number or duration; every other
		// printable edits the focused row.
		if r, ok := printableRune(msg.Text); ok && r != ' ' {
			key := keys[min(w.focus, len(keys)-1)]
			w.params[key] += string(r)
			w.paramErr = ""
		}
	}

	return w, nil
}

// updateRun edits step 3: Enter emits the resolved start message
// (root re-validates bounds and runs the leg); Esc backs to step 2.
func (w *WorkerWizard) updateRun(msg tea.KeyPressMsg) (Modal, tea.Cmd) {
	switch {
	case key.Matches(msg, w.nav.Enter):
		run, errText := w.resolveRun()
		if errText != "" {
			w.state.Error = errText

			return w, nil
		}

		return w, emitMsg(WorkerWizardStartMsg{Run: run})
	case key.Matches(msg, w.nav.Cancel):
		return w.back()
	}

	return w, nil
}

// commitTx validates the step-1 selection (stress: at least one
// toggle; bgsend: a row under the cursor or the picked name) and
// advances; a failure stays with the inline error line.
func (w *WorkerWizard) commitTx() (Modal, tea.Cmd) {
	if len(w.state.TxItems) == 0 {
		w.errLine = workerErrSelectTx

		return w, nil
	}
	if w.mode == WorkerModeBg {
		idx := w.filteredTx()
		if w.sel < len(idx) {
			w.picked = w.state.TxItems[idx[w.sel]].Label
		} else if w.picked == "" {
			w.errLine = workerErrSelectTx

			return w, nil
		}
	}
	if w.mode == WorkerModeStress && len(w.SelectedNames()) == 0 {
		w.errLine = workerErrSelectTx

		return w, nil
	}
	w.errLine = ""
	w.filtering = false
	w.draft = ""
	w.setStep(WorkerStepParams)

	return w, nil
}

// back steps one wizard step back (clearing the filter first when one
// is open, like the send wizard's); on the first step it closes.
func (w *WorkerWizard) back() (Modal, tea.Cmd) {
	if w.filtering || w.draft != "" {
		w.filtering = false
		w.draft = ""
		w.clampSel()
		w.clampTop()

		return w, nil
	}
	if w.step > 0 {
		w.setStep(w.step - 1)

		return w, nil
	}

	return w, emitMsg(WorkerWizardCloseMsg{})
}

// setStep lands on step n and clears the step-local lines (the root's
// error stamp included — arriving at a step shows it ready, not stale).
func (w *WorkerWizard) setStep(n int) {
	w.step = n
	w.errLine = ""
	w.paramErr = ""
	w.state.Error = ""
	if n == WorkerStepTx {
		w.filtering = false
		w.draft = ""
		w.clampSel()
		w.clampTop()
	}
}
