// analyze_flows_view.go draws the enumerated flow list of §J: the filter row, the
// matched count, and one row per flow. A flow row is the densest line in the TUI
// (name, ports, header, mask, verdict), which is why it is on its own rather than
// inline in the step body that hosts it.
package pages

import (
	"strconv"
	"strings"

	"jiso/internal/analyzer"
)

// flowsBlock renders the enumerated flows (the run step's compact
// table): a title with the parse counts, then the rows the filter
// keeps. Rows are clipped to the content width (the old page leaked
// fragments past the frame here).
func (a *Analyze) flowsBlock(w int) string {
	if len(a.state.Flows) == 0 {
		return ""
	}
	counts := strconv.Itoa(a.state.Parsed) + " msgs parsed, " + strconv.Itoa(a.state.Unparsable) + " unparsable"
	if a.state.Unparsable > 0 && len(a.state.UnparsableRows) > 0 {
		counts += "  ·  [u] review"
	}
	title := a.th.Deemphasized.Render(padRight("flows", analyzeListLabelWidth)) + a.th.Dim.Render(counts)

	rows := make([]string, 0, min(len(a.state.Flows), analyzeListMaxRows))
	hidden := 0
	cursorIdx, hasCursor := -1, false
	if vis := a.visibleFlowRows(); len(vis) > 0 {
		cursorIdx, hasCursor = min(a.sel, len(vis)-1), true
	}
	shown := 0
	for _, f := range a.state.Flows {
		if !FlowMatchesFilter(f, a.draft) {
			continue
		}
		if len(rows) >= analyzeListMaxRows {
			hidden++

			continue
		}
		atCursor := hasCursor && shown == cursorIdx
		rows = append(rows, a.flowRow(f, w, atCursor))
		shown++
	}
	if len(rows) == 0 {
		rows = append(rows, a.th.Deemphasized.Render("  no flows match the filter"))
	}
	if hidden > 0 {
		rows = append(rows, a.th.Dim.Render("  +"+strconv.Itoa(hidden)+" more"))
	}

	return clipCells(title, w, clipTail(a.th)) + "\n" + strings.Join(rows, "\n")
}

// flowRow renders one compact flow row: direction arrow, port, the peer port
// on the other end (so the operator sees WHO originates from WHICH port —
// UAT round 7), msgs, the MTI histogram, and the signon marker. Each
// direction row is its own selectable unit (●/○ under the cursor).
func (a *Analyze) flowRow(f AnalyzeFlowRow, w int, atCursor bool) string {
	arrow, rel := "\u2192", "from"
	word := analyzer.DirectionDst
	if a.th.ASCII {
		arrow = "->"
	}
	if f.Direction == analyzer.DirectionSrc {
		word = analyzer.DirectionSrc
		rel = "to"
		if a.th.ASCII {
			arrow = "<-"
		} else {
			arrow = "\u2190"
		}
	}
	cursor := "  "
	if atCursor {
		cursor = a.pick("\u25b8", ">") + " "
	}
	marker := a.pick("\u25cb ", "[ ] ")
	if f.Selected {
		marker = a.pick("\u25cf ", "[x] ")
	}

	ident := arrow + " " + word + " :" + strconv.Itoa(f.Port)
	if f.PeerPort > 0 {
		ident += "  " + rel + " :" + strconv.Itoa(f.PeerPort)
	}
	cells := []string{
		cursor + marker + ident,
		padRight(strconv.Itoa(f.Msgs)+" msgs", 9),
		dashIf(a.th, f.MTIs),
	}
	if f.Signon {
		cells = append(cells, a.th.Accent.Render("signon"))
	}

	return clipCells(strings.Join(cells, "  "), w, clipTail(a.th))
}

// flowFilterRow renders the folded "/" flow-filter row: the live draft
// with a caret while open, and the match count.
func (a *Analyze) flowFilterRow(w int) string {
	caret := ""
	if a.filtering {
		caret = cursorGlyph(a.th)
	}
	value := a.draft
	if value == "" {
		value = "(all flows)"
	}
	line := a.th.Deemphasized.Render(padRight("flows /", analyzeListLabelWidth)) +
		a.th.Accent.Render(value+caret)
	if n := a.matchedFlowCount(); value != "(all flows)" && n == 0 {
		line += a.th.Dim.Render("  no flows match")
	}

	return clipCells(line, w, clipTail(a.th))
}

// matchedFlowCount is the dst-flow count the current filter selects
// (the run's port set size).
func (a *Analyze) matchedFlowCount() int {
	n := 0
	for _, f := range a.state.Flows {
		if f.Selectable && FlowMatchesFilter(f, a.draft) {
			n++
		}
	}

	return n
}
