// analyze_match_keys.go is the matching step's keymap: the conds screen
// (move/add/delete/cycle/side), the inline value editor (the [o]
// output-editor idiom), and the group-by pane (multiselect toggles).
// Enter advances and Esc walks back through the shared step cases in
// analyze_keys.go; page keys are step-local, so what "s" means here is
// nobody else's business.
package pages

import (
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// updateMatching drives the conds screen (the editor and the group pane
// are checked by updateKey before the shared cancel/enter cases, so they
// own the keyboard wholesale while open).
func (a *Analyze) updateMatching(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	row, ok := a.matchRow()
	switch {
	case key.Matches(msg, a.nav.Up):
		if a.sel > 0 {
			a.sel--
		}

		return a, nil

	case key.Matches(msg, a.nav.Down):
		if ok && a.sel < len(a.state.Conds)-1 {
			a.sel++
		}

		return a, nil

	case msg.Text == "a":
		return a, func() tea.Msg { return AnalyzeCondAddMsg{} }

	case msg.Text == "d":
		if !ok {
			return a, nil
		}

		return a, func() tea.Msg { return AnalyzeCondDeleteMsg{Index: row} }

	case key.Matches(msg, a.nav.Space):
		if !ok {
			return a, nil
		}

		return a, func() tea.Msg { return AnalyzeCondWhenMsg{Index: row} }

	case msg.Text == "s":
		if !ok {
			return a, nil
		}

		return a, func() tea.Msg { return AnalyzeCondSideMsg{Index: row} }

	case msg.Text == "e":
		if !ok {
			return a, nil
		}
		a.condEditing = true
		a.condEditField = a.state.Conds[row].Field == ""
		a.condRow = row
		a.condDraft = ""

		return a, nil

	case msg.Text == "g":
		if len(a.state.Variances) == 0 {
			return a, nil // nothing varied (or nothing scanned) - nothing to choose
		}
		a.groupOpen, a.groupSel = true, 0

		return a, nil
	}

	return a, nil
}

// updateCondEditKey is the inline FIELD/VALUE mini-prompt: esc cancels
// without a message, enter commits what was typed (an empty draft is a
// cancel, never an accidental wipe), every other printable extends the
// draft. Dot-paths compose as typed ("55.1").
func (a *Analyze) updateCondEditKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	switch {
	case key.Matches(msg, a.nav.Cancel):
		a.condEditing, a.condDraft = false, ""

		return a, nil

	case key.Matches(msg, a.nav.Enter):
		v := strings.TrimSpace(a.condDraft)
		idx := a.condRow
		isField := a.condEditField
		a.condEditing, a.condDraft = false, ""
		if v == "" {
			return a, nil // empty commit is a cancel, not a wipe
		}
		if isField {
			return a, func() tea.Msg { return AnalyzeCondSetFieldMsg{Index: idx, Value: v} }
		}

		return a, func() tea.Msg { return AnalyzeCondSetValueMsg{Index: idx, Value: v} }

	case key.Matches(msg, a.nav.Backspace):
		a.condDraft = dropLastRune(a.condDraft)

		return a, nil
	}
	if r, ok := printableRune(msg.Text); ok {
		a.condDraft += string(r)
	}

	return a, nil
}

// updateGroupKey drives the group-by pane: j/k move, space toggles the
// field into/out of the grouping (the root folds the reroute), enter and
// esc return to the conds screen without a message.
func (a *Analyze) updateGroupKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	sel, ok := -1, false
	if len(a.state.Variances) > 0 {
		sel, ok = min(a.groupSel, len(a.state.Variances)-1), true
	}
	switch {
	case key.Matches(msg, a.nav.Up):
		if a.groupSel > 0 {
			a.groupSel--
		}

		return a, nil

	case key.Matches(msg, a.nav.Down):
		if ok && a.groupSel < len(a.state.Variances)-1 {
			a.groupSel++
		}

		return a, nil

	case key.Matches(msg, a.nav.Space):
		if !ok {
			return a, nil
		}
		v := a.state.Variances[sel]

		return a, func() tea.Msg { return AnalyzeGroupToggleMsg{Field: v.Field, Side: v.Side} }

	case key.Matches(msg, a.nav.Cancel), key.Matches(msg, a.nav.Enter):
		a.groupOpen = false

		return a, nil
	}

	return a, nil
}

// matchRow is the clamped conds cursor (ok=false on an empty table).
func (a *Analyze) matchRow() (int, bool) {
	if len(a.state.Conds) == 0 {
		return 0, false
	}

	return min(a.sel, len(a.state.Conds)-1), true
}
