// analyze_keys.go is §J's keyboard: which key does what on each step. The
// page never runs an engine leg itself — it returns the command the root
// should act on.
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

// updateKey is the page-local keymap: overlays own every key, then the run
// step's open filter; Esc walks back one step (aborting on step 1), Enter
// commits the step, PgUp/PgDn/Tab jump steps.
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
	if a.state.Step == StepMatching && a.condEditing {
		return a.updateCondEditKey(msg)
	}
	if a.state.Step == StepMatching && a.groupOpen {
		return a.updateGroupKey(msg)
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
		return a.updateListStep(msg, len(a.filteredSpec()), true)
	case StepHeader:
		return a.updateHeader(msg)
	case StepRun:
		return a.updateRun(msg)
	case StepMatching:
		return a.updateMatching(msg)
	}

	return a, nil
}

// updateEsc: Esc with an active filter clears it first; elsewhere it walks
// back one step (backward jumps are free); on the first step it aborts.
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
	case StepMatching:
		// Advancing is always allowed; running with zero conditions is
		// what root refuses (the note lands there, not here).
		return a, func() tea.Msg { return AnalyzeNextMsg{} }
	case StepRun:
		f := a.draft
		a.filtering = false

		return a, func() tea.Msg { return AnalyzeRunMsg{Filter: f} }
	}

	return a, nil
}

// updateListStep edits a candidate list (capture/spec). NAVIGATE mode: j/k
// move the cursor, [f] opens the shared picker (IsSpec picks .pcap vs
// .json). Typing enters EDIT mode; from there every printable (f included)
// goes into the draft and the step claims the keyboard whole.
func (a *Analyze) updateListStep(msg tea.KeyPressMsg, n int, browse bool) (Page, tea.Cmd) {
	switch {
	case !a.Editing() && key.Matches(msg, a.nav.Up):
		a.sel = max(a.sel-1, 0)
	case !a.Editing() && key.Matches(msg, a.nav.Down):
		a.sel = min(a.sel+1, max(n-1, 0))
	case !a.Editing() && browse && key.Matches(msg, a.nav.Browse):
		return a, func() tea.Msg { return AnalyzeBrowseMsg{IsSpec: a.state.Step == StepSpec} }
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

// updateHeader edits the length-header list: j/k move, space selects the
// highlighted framing.
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

// updateRun edits the run step's inline options: t/r/s pick the goal, m
// toggles masking, w writes, "/" opens the flow filter (and x/u open the
// overlays).
func (a *Analyze) updateRun(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	// Flow selection: j/k move the cursor over every visible flow row
	// (dst and src), space toggles that row's own (port, direction) unit;
	// a runs all/none over every direction.
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
		// Reopen the generated-item picker (it also auto-opens when a run
		// attaches); a reopen re-homes the sub-pane focus and offset like
		// a fresh run, never reviving a stale scroll.
		if len(a.state.Items) > 0 {
			a.itemsOpen = true
			a.previewFocused, a.previewOff = false, 0
			a.resetItemSel()
		}

		return a, nil
	case msg.Text == "u":
		// Open the unparsable reviewer over the failure samples, but only
		// when the enumeration kept some.
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

// runMiscKey maps the run step's remaining shortcut keys (goal, mask,
// output editor, filter, write) to their messages; the third result says
// whether it handled the key.
// canUseItNow is the one state that answers "what now?" right on the spot:
// the scenario run finished and its file was actually written ([l] can
// load it as the session's transactions file, [g] can pre-fill the §G
// start form with it as the routes file). An armed-but-unwritten picker
// is not enough — the keys must never promise a file that is not on disk.
func (a *Analyze) canUseItNow() bool {
	return a.state.Step == StepRun && a.state.Status == AnalyzeStatusDone &&
		a.state.FileWritten && a.state.Goal == AnalyzeGoalScenario && a.state.OutputPath != ""
}

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
	case "l":
		if !a.canUseItNow() {
			return a, nil, false
		}

		return a, func() tea.Msg { return AnalyzeUseTxFileMsg{} }, true
	case "g":
		if !a.canUseItNow() {
			return a, nil, false
		}

		return a, func() tea.Msg { return AnalyzeUseServerMsg{} }, true
	case "o":
		// [o] edits the output file the generated items land in, seeded
		// with the effective path.
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

// updateOutEditKey edits the [o] output-path one-liner: printables extend,
// backspace trims, Enter commits, Esc cancels. The outTyped gate: while
// unedited, [f] closes the editor and hands the location to the shared
// picker; once anything is typed, [f] is a path byte again.
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

// updateFlowFilterKey edits the open "/" flow filter: printables extend,
// backspace trims, Enter starts the run, Esc clears first and backs out
// on the second press.
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

// updateUnparsableKey is the unparsable viewer's read-only keyboard: Esc
// closes, j/k walk the failure samples, PgUp/PgDn page the roster.
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
