// analyze_items_keys.go is the generated-item picker's slice of §J's
// keyboard (UAT round 6): the overlay owns the keyboard wholesale while
// open, so updateKey in analyze_keys.go hands it every key. The file
// pairs with analyze_items_view.go, which renders the same state.
package pages

import (
	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// updateItemsKey is the generated-item picker's keyboard (UAT round 6):
// Esc AND Enter apply the local inclusion set to root (the next w writes
// exactly the selected items — UAT round 7: Esc no longer discards), space
// toggles the row under the cursor and its coupled dataset partner, a
// toggles all/none, and j/k/PgUp/PgDn move the roster cursor. UAT round 8
// finding 8: [tab]/[shift+tab] move the picker focus between the roster
// and the preview sub-pane; while the preview is focused those same
// scroll keys drive the preview window (ScrollPreview) instead of the
// roster cursor — the list scroll (itemOff) stays the roster's own.
func (a *Analyze) updateItemsKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	n := len(a.state.Items)
	rows := a.itemsWindow()

	switch {
	case key.Matches(msg, a.nav.Cancel):
		// UAT round 7: closing with Esc applies the selection just like
		// Enter, so backing out to the run step and pressing w still
		// writes exactly what the operator left picked.
		a.itemsOpen = false
		a.previewFocused, a.previewOff = false, 0

		return a, func() tea.Msg { return AnalyzeItemsApplyMsg{Excluded: a.itemsExcluded()} }
	case key.Matches(msg, a.nav.Enter):
		a.itemsOpen = false
		a.previewFocused, a.previewOff = false, 0

		return a, func() tea.Msg { return AnalyzeItemsApplyMsg{Excluded: a.itemsExcluded()} }
	case key.Matches(msg, a.nav.TabBack):
		a.previewFocused = false // focus back to the roster
	case key.Matches(msg, a.nav.Tab):
		a.previewFocused = true // the preview sub-pane takes the scroll keys
	case a.previewFocused && a.previewScrollKey(msg):
		// finding 8: the scroll keys were consumed over the preview;
		// space/a still reach the roster cases below.
	case key.Matches(msg, a.nav.Up):
		a.itemCursor = max(a.itemCursor-1, 0)
		a.previewOff = 0 // a new item previews from the top
	case key.Matches(msg, a.nav.Down):
		a.itemCursor = min(a.itemCursor+1, max(n-1, 0))
		a.previewOff = 0
	case key.Matches(msg, a.nav.PgUp):
		a.itemCursor = max(a.itemCursor-rows, 0)
		a.previewOff = 0
	case key.Matches(msg, a.nav.PgDn):
		a.itemCursor = min(a.itemCursor+rows, max(n-1, 0))
		a.previewOff = 0
	case key.Matches(msg, a.nav.Space):
		a.toggleItemAt(a.itemCursor)
	case msg.Text == "a":
		all := true
		for _, on := range a.itemSel {
			if !on {
				all = false

				break
			}
		}
		for i := range a.itemSel {
			a.itemSel[i] = !all
		}
	}
	a.scrollItemsIntoView(rows)

	return a, nil
}

// previewScrollKey scrolls the preview sub-pane by one row (j/k and the
// arrows) or one window (PgUp/PgDn) and reports whether the key belonged
// to it; Enter/Esc/space/a are never consumed here — they stay picker
// commands over the item under the cursor.
func (a *Analyze) previewScrollKey(msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, a.nav.Up):
		a.ScrollPreview(-1)
	case key.Matches(msg, a.nav.Down):
		a.ScrollPreview(1)
	case key.Matches(msg, a.nav.PgUp):
		a.ScrollPreview(-a.previewWindow())
	case key.Matches(msg, a.nav.PgDn):
		a.ScrollPreview(a.previewWindow())
	default:
		return false
	}

	return true
}

// resetItemSel re-seeds the local inclusion set from the pushed rows
// (open, or close-without-apply).
func (a *Analyze) resetItemSel() {
	a.itemSel = nil
	for _, it := range a.state.Items {
		a.itemSel = append(a.itemSel, it.Included)
	}
}

// itemsExcluded lists the keys whose inclusion the operator turned off — the
// exclusion set Enter and Esc commit to root, and the write persists exactly
// the complement.
func (a *Analyze) itemsExcluded() []string {
	var excluded []string
	for i, it := range a.state.Items {
		if i < len(a.itemSel) && !a.itemSel[i] {
			excluded = append(excluded, it.Key)
		}
	}

	return excluded
}

// toggleItemAt flips the row at i and, for a transaction/dataset pair, its
// coupled partner (they share a Group), so a dataset is included and written
// only together with its transaction (UAT round 7).
func (a *Analyze) toggleItemAt(i int) {
	if i >= len(a.itemSel) || i >= len(a.state.Items) {
		return
	}
	next := !a.itemSel[i]
	group := a.state.Items[i].Group
	for j := range a.itemSel {
		if j == i || (group != "" && j < len(a.state.Items) && a.state.Items[j].Group == group) {
			a.itemSel[j] = next
		}
	}
}

// scrollItemsIntoView keeps the roster cursor inside the visible rows.
func (a *Analyze) scrollItemsIntoView(rows int) {
	if rows <= 0 {
		return
	}
	if a.itemOff < 0 {
		a.itemOff = 0
	}
	if a.itemCursor < a.itemOff {
		a.itemOff = a.itemCursor
	}
	if a.itemCursor >= a.itemOff+rows {
		a.itemOff = a.itemCursor - rows + 1
	}
}
