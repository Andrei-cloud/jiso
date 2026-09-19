// send_wizard_keys.go is the send wizard's key routing: the embedded
// connect step's Enter/Esc, the list steps' cursor, filter and browse,
// and the Enter that emits each step's choose message.
package pages

import (
	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/theme"
)

// Update routes sizes and keys; every key reaches here (the router does
// not intercept Enter/Esc for this modal).
func (w *SendWizard) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		w.width, w.height = msg.Width, msg.Height

		return w, nil
	case tea.KeyPressMsg:
		return w.updateKey(msg)
	}

	return w, nil
}

// updateKey routes one key: the connect step forwards to the embedded
// dialog (Enter/Esc included), the list steps own their cursor/filter.
func (w *SendWizard) updateKey(msg tea.KeyPressMsg) (Modal, tea.Cmd) {
	if w.currentStep() == WizardStepConnect {
		return w.updateConnectStep(msg)
	}

	switch {
	case key.Matches(msg, wizardKeyEnter):
		return w.advance()
	case key.Matches(msg, wizardKeyEsc):
		return w.back()
	case !w.Editing() && key.Matches(msg, w.nav.Up):
		w.sel = max(w.sel-1, 0)
	case !w.Editing() && key.Matches(msg, w.nav.Down):
		w.sel = min(w.sel+1, max(w.filteredCount()-1, 0))
	case !w.Editing() && key.Matches(msg, wizardKeyBrowse):
		// [f] in NAVIGATE mode opens the root-owned file picker; once
		// typing has entered edit mode every printable — f included —
		// goes into the filter (the two-mode contract).
		return w, emitMsg(WizardBrowseMsg{IsSpec: w.currentStep() == WizardStepSpec})
	default:
		// j/k/arrows navigate only on an empty filter; once the user
		// types, every printable rune (j and k included — the
		// live-filter lesson) goes into the filter.
		w.filterList(msg)
	}

	return w, nil
}

// updateConnectStep forwards everything to the embedded dialog; Enter
// (not while an attempt runs) arms the attempt, Esc closes the wizard
// (cancelling an in-flight attempt first — the root owns the leg).
func (w *SendWizard) updateConnectStep(msg tea.KeyPressMsg) (Modal, tea.Cmd) {
	if w.dlg == nil {
		return w.back()
	}
	if w.dlg.pickerOpen {
		_, _ = w.dlg.Update(msg)

		return w, nil
	}
	st := w.dlg.State()
	if st.InFlight {
		if key.Matches(msg, wizardKeyEsc) {
			return w, emitMsg(WizardCancelMsg{})
		}

		return w, nil // form frozen while attempting (progress line only)
	}

	switch {
	case key.Matches(msg, wizardKeyEnter):
		return w, emitMsg(WizardConnectAttemptMsg{})
	case key.Matches(msg, wizardKeyEsc):
		return w, emitMsg(WizardCancelMsg{})
	}
	_, _ = w.dlg.Update(msg)

	return w, nil
}

// advance applies the current selection (or a path-shaped filter) and
// emits the step's choose message; the send step emits the template name.
func (w *SendWizard) advance() (Modal, tea.Cmd) {
	switch w.currentStep() {
	case WizardStepSpec:
		if path, ok := w.pickedPath(w.state.SpecItems); ok {
			return w, emitMsg(WizardChooseSpecMsg{Path: path})
		}
	case WizardStepFile:
		if path, ok := w.pickedPath(w.state.FileItems); ok {
			return w, emitMsg(WizardChooseFileMsg{Path: path})
		}
	case WizardStepSend:
		idx := w.filteredTemplates()
		if len(idx) > 0 && w.sel < len(idx) {
			return w, emitMsg(WizardSendMsg{Name: w.state.Templates[idx[w.sel]].Name})
		}
	}

	return w, nil
}

// back steps one wizard step back (clearing the filter first when one is
// active); on the first step it cancels the wizard.
func (w *SendWizard) back() (Modal, tea.Cmd) {
	if w.filter != "" {
		w.filter = ""
		w.clampSel()

		return w, nil
	}
	if w.step > 0 {
		w.step--
		w.resetStepInput()

		return w, nil
	}

	return w, emitMsg(WizardCancelMsg{})
}

// filterList types into the step filter; Esc/backspace are handled in
// updateKey. Non-printable keys are ignored.
func (w *SendWizard) filterList(msg tea.KeyPressMsg) {
	if r, ok := printableRune(msg.Text); ok {
		w.filter += string(r)
		w.clampSel()

		return
	}
	if key.Matches(msg, wizardKeyBackspace) && w.filter != "" {
		r := []rune(w.filter)
		w.filter = string(r[:len(r)-1])
		w.clampSel()
	}
}

var (
	wizardKeyEnter     = key.NewBinding(key.WithKeys(theme.KeyEnter))
	wizardKeyEsc       = key.NewBinding(key.WithKeys(theme.KeyEsc))
	wizardKeyBrowse    = key.NewBinding(key.WithKeys("f"))
	wizardKeyBackspace = key.NewBinding(key.WithKeys("backspace"))
)

// emitMsg adapts a value to the tea.Cmd return (the dashboard idiom).
func emitMsg(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}
