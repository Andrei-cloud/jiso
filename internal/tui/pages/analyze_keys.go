// analyze_keys.go is §J's keyboard: which key does what on each step. The page
// never runs an engine leg itself -- it returns the command the root should act on
// -- so what is written here is the wizard's contract with the operator, and the
// order the keys are tried in is part of that contract.
package pages

import (
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Update routes sizes, the router's Tab revisit, and keys; bus events
// are root-side truth and are ignored with a nil command.
func (a *Analyze) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
	case PaneFocusMsg:
		return a, stepDeltaCmd(boolToStep(msg.Reverse))
	case tea.KeyPressMsg:
		return a.updateKey(msg)
	}

	return a, nil
}

// stepDeltaCmd builds the root-side step-jump message.
func stepDeltaCmd(delta int) tea.Cmd {
	return func() tea.Msg { return AnalyzeStepDeltaMsg{Delta: delta} }
}

// updateKey is the page-local keymap. The run step's open flow filter
// owns the keys first; Esc walks back one step (aborting on step 1);
// Enter commits the step; PgUp/PgDn/Tab jump steps; the rest are
// step-local.
func (a *Analyze) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	if a.itemsOpen {
		return a.updateItemsKey(msg) // the picker overlay owns the keyboard wholesale
	}
	if a.unparsableOpen {
		return a.updateUnparsableKey(msg) // the hexdump viewer owns it too
	}
	if a.state.Step == StepRun && a.outEditing {
		return a.updateOutEditKey(msg)
	}
	if a.state.Step == StepRun && a.filtering {
		return a.updateFlowFilterKey(msg)
	}
	switch {
	case key.Matches(msg, a.nav.Cancel):
		return a.updateEsc()
	case key.Matches(msg, a.nav.Enter):
		return a.updateEnter()
	case key.Matches(msg, a.nav.PgUp):
		a.draft = ""
		a.filtering = false

		return a, stepDeltaCmd(-1)
	case key.Matches(msg, a.nav.PgDn):
		a.draft = ""
		a.filtering = false

		return a, stepDeltaCmd(1)
	case key.Matches(msg, a.nav.TabBack):
		a.draft = ""
		a.filtering = false

		return a, stepDeltaCmd(-1)
	case key.Matches(msg, a.nav.Tab):
		a.draft = ""
		a.filtering = false

		return a, stepDeltaCmd(1)
	}

	switch a.state.Step {
	case StepCapture:
		return a.updateListStep(msg, len(a.filteredCapture()), true)
	case StepSpec:
		return a.updateListStep(msg, len(a.filteredSpec()), false)
	case StepHeader:
		return a.updateHeader(msg)
	case StepRun:
		return a.updateRun(msg)
	}

	return a, nil
}

// updateEsc: Esc with an active filter clears it first (the send
// wizard's back()); elsewhere it steps back through root's free
// backward jump; on the first step it aborts (root asks §N3 confirm
// when a leg is in flight).
func (a *Analyze) updateEsc() (Page, tea.Cmd) {
	if a.draft != "" {
		a.draft = ""
		a.filtering = false
		a.clampSel()

		return a, nil
	}
	if a.state.Step > StepCapture {
		return a, stepDeltaCmd(-1)
	}

	return a, func() tea.Msg { return AnalyzeAbortMsg{} }
}

// updateEnter commits the current step (capture/spec pick or typed
// path; the header step advances; the run step starts the analysis
// with the folded inline options).
func (a *Analyze) updateEnter() (Page, tea.Cmd) {
	switch a.state.Step {
	case StepCapture:
		v, ok := a.pickedCapture()
		if !ok {
			return a, nil
		}
		a.draft = ""

		return a, func() tea.Msg { return AnalyzeCommitCaptureMsg{Value: v} }
	case StepSpec:
		v, ok := a.pickedSpec()
		if !ok {
			return a, nil
		}
		a.draft = ""

		return a, func() tea.Msg { return AnalyzeCommitSpecMsg{Value: v} }
	case StepHeader:
		return a, func() tea.Msg { return AnalyzeNextMsg{} }
	case StepRun:
		f := a.draft
		a.filtering = false

		return a, func() tea.Msg { return AnalyzeRunMsg{Filter: f} }
	}

	return a, nil
}

// updateListStep edits a candidate list (capture/spec): in NAVIGATE mode
// j/k/arrows move the cursor and [f] opens the shared picker (capture
// only); typing enters EDIT mode and from there every printable — f
// included — goes into the filter/typed path (the SCR-502 lesson plus the
// two-mode browse gate, Task 5.2). Once typing is in progress the step
// claims the keyboard whole (UAT round 8 / D2): "?" types too; on the
// fresh step the global layer still works and "?" opens §M).
func (a *Analyze) updateListStep(msg tea.KeyPressMsg, n int, browse bool) (Page, tea.Cmd) {
	switch {
	case !a.Editing() && key.Matches(msg, a.nav.Up):
		a.sel = max(a.sel-1, 0)
	case !a.Editing() && key.Matches(msg, a.nav.Down):
		a.sel = min(a.sel+1, max(n-1, 0))
	case !a.Editing() && browse && key.Matches(msg, a.nav.Browse):
		return a, func() tea.Msg { return AnalyzeBrowseMsg{} }
	default:
		if r, ok := printableRune(msg.Text); ok {
			a.draft += string(r)
			a.clampSel()

			return a, nil
		}
		if key.Matches(msg, a.nav.Backspace) && a.draft != "" {
			a.draft = dropLastRune(a.draft)
			a.clampSel()
		}
	}

	return a, nil
}

// updateHeader edits the length-header list: j/k move, space selects
// the highlighted framing, Enter advances (handled in updateKey).
func (a *Analyze) updateHeader(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	n := len(a.state.Headers)
	switch {
	case key.Matches(msg, a.nav.Up):
		a.sel = max(a.sel-1, 0)
	case key.Matches(msg, a.nav.Down):
		a.sel = min(a.sel+1, max(n-1, 0))
	case key.Matches(msg, a.nav.Space):
		if n > 0 {
			hdr := a.state.Headers[min(a.sel, n-1)].Header

			return a, func() tea.Msg { return AnalyzeChooseHeaderMsg{Header: hdr} }
		}
	}

	return a, nil
}

// updateRun edits the run step's inline options: t/r/s pick the goal,
// m toggles the security (mask) option, w writes the report, "/" opens
// the flow filter. Once the filter is open every key goes through
// updateFlowFilterKey.
func (a *Analyze) updateRun(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	// Flow selection (UAT round 4: the arrows did nothing and the ●/○
	// rows looked unselectable; UAT round 7: each direction is its own
	// unit): j/k move the cursor over every visible flow row, and space
	// toggles the row's own (port, direction) — a src row selects the
	// responses independently of its dst half. a runs all/none over every
	// direction. (Scenario folds a port's two directions into one unit;
	// root owns that distinction.)
	switch {
	case key.Matches(msg, a.nav.Up):
		a.sel = max(a.sel-1, 0)

		return a, nil
	case key.Matches(msg, a.nav.Down):
		a.sel = min(a.sel+1, max(len(a.visibleFlowRows())-1, 0))

		return a, nil
	case key.Matches(msg, a.nav.Space):
		if rows := a.visibleFlowRows(); len(rows) > 0 {
			if r := rows[min(a.sel, len(rows)-1)]; r.Port > 0 {
				return a, func() tea.Msg { return AnalyzeFlowToggleMsg{Port: r.Port, Dir: r.Direction} }
			}
		}

		return a, nil
	case msg.Text == "a":
		return a, func() tea.Msg { return AnalyzeFlowToggleAllMsg{} }
	case msg.Text == "x":
		// UAT round 6: reopen the generated-item picker (it also opens
		// automatically when a run attaches).
		if len(a.state.Items) > 0 {
			a.itemsOpen = true
			a.resetItemSel()
		}

		return a, nil
	case msg.Text == "u":
		// UAT round 6: open the unparsable-message reviewer over the
		// failure samples (only when the enumeration kept any).
		if len(a.state.UnparsableRows) > 0 {
			a.unparsableOpen = true
			a.unparsableCursor = 0
			a.unparsableOff = 0
		}

		return a, nil
	}

	if p, cmd, ok := a.runMiscKey(msg); ok {
		return p, cmd
	}

	return a, nil
}

// runMiscKey maps the run step's remaining shortcut keys — the goal and
// security radios, the [o] output editor, the "/" filter and the write
// — to their messages; the third result reports whether it handled the
// key (so updateRun stays within its statement budget).
func (a *Analyze) runMiscKey(msg tea.KeyPressMsg) (Page, tea.Cmd, bool) {
	switch msg.Text {
	case "t":
		return a, func() tea.Msg { return AnalyzeChooseGoalMsg{Goal: AnalyzeGoalTransactions} }, true
	case "r":
		return a, func() tea.Msg { return AnalyzeChooseGoalMsg{Goal: AnalyzeGoalMockRoutes} }, true
	case "s":
		return a, func() tea.Msg { return AnalyzeChooseGoalMsg{Goal: AnalyzeGoalScenario} }, true
	case "m":
		return a, func() tea.Msg { return AnalyzeChooseMaskMsg{Raw: !a.state.MaskRaw} }, true
	case "o":
		// UAT round 5: [o] edits the output file the generated items
		// land in (seeded with the effective path; the browse gate of
		// UAT round 8 finding 6 opens fresh).
		a.outEditing = true
		a.outDraft = a.state.OutputPath
		a.outTyped = false

		return a, nil, true
	case "/":
		a.filtering = true

		return a, nil, true
	}
	if key.Matches(msg, a.nav.Write) {
		return a, func() tea.Msg { return AnalyzeWriteMsg{} }, true
	}

	return a, nil, false
}

// updateOutEditKey edits the [o] output-path one-liner (UAT round 5):
// printables extend it, backspace trims, Enter commits, Esc cancels.
// UAT round 8 finding 6 adds the capture step's two-mode browse gate:
// while nothing has been edited yet, [f] closes the editor and hands
// the output location to the shared root-side picker; once the operator
// has typed, [f] is a path byte again (the SCR-502/D3 lesson).
func (a *Analyze) updateOutEditKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	switch {
	case key.Matches(msg, a.nav.Cancel):
		a.outEditing, a.outDraft, a.outTyped = false, "", false

		return a, nil
	case key.Matches(msg, a.nav.Enter):
		v := strings.TrimSpace(a.outDraft)
		a.outEditing, a.outDraft, a.outTyped = false, "", false
		if v == "" {
			return a, nil
		}

		return a, func() tea.Msg { return AnalyzeOutCommitMsg{Path: v} }
	case !a.outTyped && key.Matches(msg, a.nav.Browse):
		draft := a.outDraft
		a.outEditing, a.outDraft, a.outTyped = false, "", false

		return a, func() tea.Msg { return AnalyzeOutBrowseMsg{Draft: draft} }
	case key.Matches(msg, a.nav.Backspace):
		a.outDraft = dropLastRune(a.outDraft)
		a.outTyped = true

		return a, nil
	}
	if r, ok := printableRune(msg.Text); ok {
		a.outDraft += string(r)
		a.outTyped = true
	}

	return a, nil
}

// updateFlowFilterKey edits the run step's open "/" flow filter: every
// printable extends it, backspace trims, Enter starts the run, Esc
// clears it first and backs out on the second press.
func (a *Analyze) updateFlowFilterKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	switch {
	case key.Matches(msg, a.nav.Enter):
		return a.updateEnter()
	case key.Matches(msg, a.nav.Cancel):
		if a.draft != "" {
			a.draft = ""

			return a, nil
		}
		a.filtering = false

		return a, stepDeltaCmd(-1)
	case key.Matches(msg, a.nav.Backspace):
		a.draft = dropLastRune(a.draft)

		return a, nil
	}
	if r, ok := printableRune(msg.Text); ok {
		a.draft += string(r)
	}

	return a, nil
}

// updateItemsKey is the generated-item picker's keyboard (UAT round 6):
// Esc AND Enter apply the local inclusion set to root (the next w writes
// exactly the selected items — UAT round 7: Esc no longer discards), space
// toggles the row under the cursor and its coupled dataset partner, a
// toggles all/none, and j/k/PgUp/PgDn move the roster cursor.
func (a *Analyze) updateItemsKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	n := len(a.state.Items)
	_, rows := a.itemsWindow()

	switch {
	case key.Matches(msg, a.nav.Cancel):
		// UAT round 7: closing with Esc applies the selection just like
		// Enter, so backing out to the run step and pressing w still
		// writes exactly what the operator left picked.
		a.itemsOpen = false

		return a, func() tea.Msg { return AnalyzeItemsApplyMsg{Excluded: a.itemsExcluded()} }
	case key.Matches(msg, a.nav.Enter):
		a.itemsOpen = false

		return a, func() tea.Msg { return AnalyzeItemsApplyMsg{Excluded: a.itemsExcluded()} }
	case key.Matches(msg, a.nav.Up):
		a.itemCursor = max(a.itemCursor-1, 0)
	case key.Matches(msg, a.nav.Down):
		a.itemCursor = min(a.itemCursor+1, max(n-1, 0))
	case key.Matches(msg, a.nav.PgUp):
		a.itemCursor = max(a.itemCursor-rows, 0)
	case key.Matches(msg, a.nav.PgDn):
		a.itemCursor = min(a.itemCursor+rows, max(n-1, 0))
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

// updateUnparsableKey is the unparsable-message viewer's keyboard (UAT
// round 6): a read-only hexdump browser. Esc closes; j/k (and the
// arrows) walk the failure samples; PgUp/PgDn page the roster. Nothing
// is emitted — the viewer only informs the write decision.
func (a *Analyze) updateUnparsableKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	n := len(a.state.UnparsableRows)
	_, rows := a.unparsableWindow()

	switch {
	case key.Matches(msg, a.nav.Cancel):
		a.unparsableOpen = false
	case key.Matches(msg, a.nav.Up):
		a.unparsableCursor = max(a.unparsableCursor-1, 0)
	case key.Matches(msg, a.nav.Down):
		a.unparsableCursor = min(a.unparsableCursor+1, max(n-1, 0))
	case key.Matches(msg, a.nav.PgUp):
		a.unparsableCursor = max(a.unparsableCursor-rows, 0)
	case key.Matches(msg, a.nav.PgDn):
		a.unparsableCursor = min(a.unparsableCursor+rows, max(n-1, 0))
	}
	a.scrollUnparsableIntoView(rows)

	return a, nil
}

// scrollUnparsableIntoView keeps the viewer cursor inside the visible
// roster rows.
func (a *Analyze) scrollUnparsableIntoView(rows int) {
	if rows <= 0 {
		return
	}
	if a.unparsableOff < 0 {
		a.unparsableOff = 0
	}
	if a.unparsableCursor < a.unparsableOff {
		a.unparsableOff = a.unparsableCursor
	}
	if a.unparsableCursor >= a.unparsableOff+rows {
		a.unparsableOff = a.unparsableCursor - rows + 1
	}
}
