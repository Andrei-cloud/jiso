// worker_wizard_view.go renders the §H worker wizard as one centered
// modal box in the analyze wizard's visual vocabulary: a
// "STRESS  1 tx ▸ 2 rate ▸ 3 run" rail (current step accented, the
// rest dim), the step body, the inline error line, and a right-aligned
// key footer. Step 1's transaction list is a subwindow with EXACTLY 5
// visible rows (UAT round 4) whose scrollability is unmistakable: a
// "▴ n above" first line and a "v n below" last line name the hidden
// rows ("^"/"v" under theme.ASCII), ▸ marks the cursor, [x]/[ ] boxes
// tick the stress multi-select (dim "N selected" line under the
// window), and the bgsend single-select shows ●/○ radios. EVERY line is
// clipped to the box's inner width (clipCells + clipTail): no fragment
// may leak past the frame at any width ≥ the frame minimum.
package pages

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
)

const (
	workerWizardBoxWidth = 60
	workerWizardBoxMin   = 44

	// workerParamLabelCol is the label column of the step-2 rows, the
	// run-step summary rows, and the filter line (analyze's
	// analyzeListLabelWidth idiom).
	workerParamLabelCol = 9
)

// View renders the overlay body the root composes over the content area.
func (w *WorkerWizard) View() string {
	width := w.width
	if width <= 0 {
		width = frame.FallbackWidth
	}

	bw := max(min(workerWizardBoxWidth, width-2), workerWizardBoxMin)
	inner := bw - 2

	body := w.rail(inner) + "\n" + w.stepBody(inner)
	if line := w.errorLine(); line != "" {
		body += "\n" + clipCells(w.th.Status(theme.KindError, line), inner, clipTail(w.th))
	}
	body += "\n" + w.footer(inner)

	return w.boxStyle().Width(bw).Render(body)
}

// errorLine folds the three mutually exclusive inline lines into one
// slot: the tx step's Enter error, the param step's validation error,
// and the root-stamped failure (tx-file load or start leg).
func (w *WorkerWizard) errorLine() string {
	if w.errLine != "" {
		return w.errLine
	}
	if w.paramErr != "" {
		return w.paramErr
	}

	return w.state.Error
}

// rail renders "STRESS  1 tx ▸ 2 rate ▸ 3 run": the accent title, then
// the numbered step labels with the current step accented and the rest
// dim (the analyze railLine vocabulary).
func (w *WorkerWizard) rail(inner int) string {
	// three sections at most: the title, the step body and the key line
	parts := make([]string, 0, 3)
	parts = append(parts, titleLine(w.th, w.title()))
	names := workerStepNames[w.mode]
	labels := make([]string, 0, len(names))
	for i, name := range names {
		label := strconv.Itoa(i+1) + " " + name
		if i == w.step {
			labels = append(labels, w.th.Accent.Render(label))

			continue
		}
		labels = append(labels, w.th.Deemphasized.Render(label))
	}
	parts = append(parts, strings.Join(labels, w.pick(" \u25b8 ", " > ")))

	return clipCells(strings.Join(parts, "  "), inner, clipTail(w.th))
}

// title is the wizard's box title (the retired forms' dialog titles).
func (w *WorkerWizard) title() string {
	if w.mode == WorkerModeStress {
		return "STRESS"
	}

	return "BGSEND"
}

// stepBody renders the current step's content.
func (w *WorkerWizard) stepBody(inner int) string {
	switch w.step {
	case WorkerStepTx:
		return w.txBody(inner)
	case WorkerStepParams:
		return w.paramsBody(inner)
	case WorkerStepRun:
		return w.runBody(inner)
	}

	return ""
}

// txBody renders step 1: the filter line, the 5-row scrollable list
// subwindow with its above/below marker lines, and the stress
// "N selected" count line.
func (w *WorkerWizard) txBody(inner int) string {
	lines := []string{w.filterLine(inner)}
	shown := w.filteredTx()
	switch {
	case len(w.state.TxItems) == 0:
		lines = append(lines, clipCells(
			w.th.Deemphasized.Render(pickGlyph(w.th,
				"no transactions loaded \u2014 [", "no transactions loaded -- ["))+
				w.th.HotKey.Render("f")+w.th.Deemphasized.Render("] browse"),
			inner, clipTail(w.th)))
	case len(shown) == 0:
		lines = append(lines, w.th.Deemphasized.Render(clipCells(
			"no transactions match the filter", inner, clipTail(w.th))))
	default:
		lines = append(lines, w.listBox(shown, inner))
		if w.mode == WorkerModeStress {
			lines = append(lines, w.selectedLine(inner))
		}
	}

	return strings.Join(lines, "\n")
}

// listBox renders the subwindow: up to WorkerTxVisibleRows rows from
// the scroll window, bracketed by the "▴ n above" / "v n below"
// markers whenever rows hide beyond the window's edges (the obvious
// scroll affordance the UAT asked for).
func (w *WorkerWizard) listBox(shown []int, inner int) string {
	lo, hi := w.top, min(w.top+WorkerTxVisibleRows, len(shown))
	rows := make([]string, 0, WorkerTxVisibleRows+2)
	if lo > 0 {
		rows = append(rows, w.dimMarker(
			w.pick("\u25b4 ", "^ ")+strconv.Itoa(lo)+" above", inner))
	}
	for i := lo; i < hi; i++ {
		rows = append(rows, w.txRow(shown[i], i, inner-2))
	}
	if below := len(shown) - hi; below > 0 {
		rows = append(rows, w.dimMarker("v "+strconv.Itoa(below)+" below", inner))
	}

	return w.boxStyle().Width(inner).Render(strings.Join(rows, "\n"))
}

// dimMarker renders one scroll-affordance line (dim, clipped).
func (w *WorkerWizard) dimMarker(text string, inner int) string {
	return clipCells(w.th.Dim.Render("  "+text), inner-2, clipTail(w.th))
}

// txRow renders one candidate: the ▸ cursor, the stress checkbox or
// the bgsend radio, and the label.
func (w *WorkerWizard) txRow(itemIdx, row, inner int) string {
	it := w.state.TxItems[itemIdx]
	marker := "  "
	if row == w.sel {
		marker = w.pick("\u25b8", ">") + " "
	}
	box := w.checkbox(it.Label)
	label := clipCells(it.Label, max(inner-lipgloss.Width(marker+box)-1, 8), clipTail(w.th))
	line := marker + box + label
	if row == w.sel {
		return clipCells(line, inner, clipTail(w.th))
	}

	return clipCells(w.th.TextMuted.Render(line), inner, clipTail(w.th))
}

// checkbox renders the stress theme.BoxChecked/theme.BoxUnchecked tick box or the bgsend
// ●/○ radio (ASCII "(*)"/"( )" — the connect dialog's selection
// glyphs) for a candidate name.
func (w *WorkerWizard) checkbox(label string) string {
	if w.mode != WorkerModeStress {
		if w.picked == label {
			return w.pick("\u25cf ", "(*) ")
		}

		return w.pick("\u25cb ", "( ) ")
	}
	for i, it := range w.state.TxItems {
		if it.Label == label {
			if i < len(w.checked) && w.checked[i] {
				return theme.BoxChecked
			}

			return theme.BoxUnchecked
		}
	}

	return theme.BoxUnchecked
}

// selectedLine is the stress count line under the list: the dim
// "N selected (space toggles)" badge (the §N2 checklist's idiom), the
// toggle hint alone while nothing is ticked.
func (w *WorkerWizard) selectedLine(inner int) string {
	n := len(w.SelectedNames())
	base := w.th.Deemphasized
	hint := base.Render("(") + keyGlyph(w.th, "space") + base.Render(" toggles)")
	text := hint
	if n > 0 {
		text = base.Render(strconv.Itoa(n)+" selected ") + hint
	}

	return clipCells("  "+text, inner, clipTail(w.th))
}

// filterLine shows the live "/" filter with the accent caret (the
// analyze filterLine idiom).
func (w *WorkerWizard) filterLine(inner int) string {
	caret := ""
	if w.filtering {
		caret = cursorGlyph(w.th)
	}
	line := w.th.Deemphasized.Render(padRight("filter", workerParamLabelCol)) +
		w.th.Accent.Render(w.draft+caret)

	return clipCells(line, inner, clipTail(w.th))
}

// paramsBody renders step 2: one labeled row per parameter, the
// focused one with the accent caret, each carrying its dim bounds
// hint, and the inline validation error.
func (w *WorkerWizard) paramsBody(inner int) string {
	keys := w.paramKeys()
	rows := make([]string, 0, len(keys)+1)
	for i, key := range keys {
		label := w.th.Deemphasized.Render(padRight(key, workerParamLabelCol))
		value := w.params[key]
		if i == w.focus {
			value = w.th.Accent.Render(value + cursorGlyph(w.th))
		} else {
			value = w.th.TextPrimary.Render(value)
		}
		line := label + value + w.th.Dim.Render("  "+paramHints[w.mode][key])
		rows = append(rows, clipCells(line, inner, clipTail(w.th)))
	}

	return strings.Join(rows, "\n")
}

// paramHints are the step-2 rows' dim bounds hints — the exact note
// strings the retired §N2 forms carried.
var paramHints = map[string]map[string]string{
	WorkerModeStress: {
		WorkerParamTps:      "(1-100000)",
		WorkerParamRamp:     "(e.g. 30s, 1m)",
		WorkerParamDuration: "(after ramp)",
		WorkerParamWorkers:  "(1-50)",
	},
	WorkerModeBg: {
		WorkerParamCount:    "(sends per tick)",
		WorkerParamInterval: "(e.g. 500ms, 1.5s, 1m)",
	},
}

// runBody renders step 3: the summary rows (selection + every
// parameter) and the status line (ready / root-stamped in-flight
// progress; failures render through the shared error line).
func (w *WorkerWizard) runBody(inner int) string {
	rows := []string{w.summaryRow("tx", w.txSummary(), inner)}
	for _, key := range w.paramKeys() {
		rows = append(rows, w.summaryRow(key, w.params[key], inner))
	}
	switch {
	case w.state.InFlight:
		rows = append(rows, w.th.Deemphasized.Render(
			clipCells(w.state.Progress, inner, clipTail(w.th))))
	default:
		what := "the background send"
		if w.mode == WorkerModeStress {
			what = "the stress test"
		}
		ready := w.th.TextMuted.Render("ready - ") + keyGlyph(w.th, "Enter") +
			w.th.TextMuted.Render(" starts "+what)
		rows = append(rows, clipCells(ready, inner, clipTail(w.th)))
	}

	return strings.Join(rows, "\n")
}

// txSummary is the run step's tx row: the selected names joined, or
// the count once the join would dominate the line.
func (w *WorkerWizard) txSummary() string {
	names := w.SelectedNames()
	if len(names) > 3 {
		return strconv.Itoa(len(names)) + " transactions selected"
	}

	return strings.Join(names, ", ")
}

// summaryRow renders one run-step "label  value" row (the analyze
// summaryRow idiom).
func (w *WorkerWizard) summaryRow(label, value string, inner int) string {
	line := w.th.Deemphasized.Render(padRight(label, workerParamLabelCol)) +
		w.th.TextPrimary.Render(value)

	return clipCells(line, inner, clipTail(w.th))
}

// footer right-aligns the step's key line (the send wizard's footer
// vocabulary).
func (w *WorkerWizard) footer(inner int) string {
	base := w.th.Deemphasized
	var keys string
	switch w.step {
	case WorkerStepTx:
		// Step 1 is the wizard's first step: Esc closes it (the UAT
		// wizard contract), so the footer always reads cancel here.
		keys = keySpan(w.th, base, "Enter", "next") + base.Render("   ") +
			keySpan(w.th, base, "/", "filter") + base.Render("   ") +
			keySpan(w.th, base, "f", "browse") + base.Render("   ") +
			keySpan(w.th, base, "Esc", "cancel")
	case WorkerStepParams:
		keys = keySpan(w.th, base, "Enter", "next") + base.Render("   ") +
			keySpan(w.th, base, "up/down", "field") + base.Render("   ") +
			keySpan(w.th, base, "Esc", "back")
	case WorkerStepRun:
		if w.state.InFlight {
			keys = keySpan(w.th, base, "Esc", "close")
		} else {
			keys = keySpan(w.th, base, "Enter", "start") + base.Render("   ") +
				keySpan(w.th, base, "Esc", "back")
		}
	}
	// Clipped before padding: at the narrow box clamp the key line may
	// never overrun the frame (clip, never wrap).
	keys = clipCells(keys, inner, clipTail(w.th))
	return keyLine(keys, inner)
}

// boxStyle is the wizard border: rounded normally, ASCII under
// theme.ASCII (the send wizard's boxStyle).
func (w *WorkerWizard) boxStyle() lipgloss.Style {
	b := lipgloss.RoundedBorder()
	if w.th.ASCII {
		b = lipgloss.ASCIIBorder()
	}

	return lipgloss.NewStyle().
		Border(b).
		BorderForeground(w.th.Border.GetBorderTopForeground())
}
