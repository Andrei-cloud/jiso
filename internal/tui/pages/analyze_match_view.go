// analyze_match_view.go renders the §J routes goal's matching wizard: the
// condition table with its live scan line, and the group-by pane over it.
// All data is the root-derived snapshot; the page contributes only the
// cursors and the editor draft.
package pages

import (
	"strings"

	"jiso/internal/tui/theme"
)

// matchColumnWidths are the conds table columns (SIDE FIELD WHEN VALUE);
// the value runs to the row end, clipped by the usual clipTail.
const (
	matchSideWidth  = 6
	matchFieldWidth = 9
	matchWhenWidth  = 8
)

// condValueText is a condition's display/edit text: the scalar for
// equals/exists/prefix/regex, the comma-joined list for one-of/not-in.
func condValueText(c AnalyzeCond) string {
	if c.ValuesText != "" {
		return c.ValuesText
	}

	return c.Value
}

// matchingBody renders the conds screen, or the group-by pane when open.
func (a *Analyze) matchingBody(w int) string {
	if a.groupOpen && len(a.state.Variances) > 0 {
		return a.groupBody(w)
	}
	var b strings.Builder
	b.WriteString(a.th.Deemphasized.Render("MATCHING CONDITIONS") +
		a.th.Dim.Render("  "+dashIf(a.th, a.liveMatchFold())) + "\n")

	head := "  " + padRight("SIDE", matchSideWidth) + padRight("FIELD", matchFieldWidth) + padRight("WHEN", matchWhenWidth) + "VALUE"
	b.WriteString(a.th.Deemphasized.Render(clipCells(head, w, clipTail(a.th))) + "\n")

	for i, c := range a.state.Conds {
		cursor := a.th.Selector(i == a.sel)
		side := c.Side
		if side == "" {
			side = "req"
		}
		row := cursor + " " + padRight(side, matchSideWidth) +
			padRight(dashIf(a.th, c.Field), matchFieldWidth) +
			padRight(dashIf(a.th, c.When), matchWhenWidth) +
			dashIf(a.th, condValueText(c))
		if side == "resp" {
			b.WriteString(clipCells(a.th.TextMuted.Render(row), w, clipTail(a.th)) + "\n")
		} else {
			b.WriteString(clipCells(a.th.TextPrimary.Render(row), w, clipTail(a.th)) + "\n")
		}
	}

	if len(a.state.Conds) == 0 {
		b.WriteString(a.th.Dim.Render("  no conditions - [a] adds one; the wizard never runs the whole capture as one route") + "\n")
	}
	if a.state.MatchWarn != "" {
		b.WriteString(clipCells(a.th.Status(theme.KindWarn, a.state.MatchWarn), w, clipTail(a.th)) + "\n")
	}
	if a.condEditing {
		b.WriteString(a.condEditorRow(w) + "\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// liveMatchFold is the scan fold text: "scanning the capture..." while the
// leg is in flight, the folded count line once it is not.
func (a *Analyze) liveMatchFold() string {
	if a.state.MatchScanning {
		return "scanning the capture..."
	}

	return dashIf(a.th, a.state.MatchLine)
}

// condEditorRow renders the inline mini-prompt (the [o] output-editor
// idiom): which half is being typed, the draft, and its verbs.
func (a *Analyze) condEditorRow(w int) string {
	what := "value"
	if a.condEditField {
		what = "field"
	}

	return clipCells(a.th.Key(what)+": "+
		a.th.Accent.Render(a.condDraft+cursorGlyph(a.th))+
		a.th.Dim.Render("  enter set · esc cancel"), w, clipTail(a.th))
}

// groupBody renders the group-by pane: fields the scan saw vary, checkbox
// style (this is a multiselect, not the header radio list), the chosen
// ones marked on. Enter/esc returns to the conds screen.
func (a *Analyze) groupBody(w int) string {
	var b strings.Builder
	head := a.th.Deemphasized.Render("GROUP BY") +
		a.th.Dim.Render("  fields whose captured values varied · [space] toggles · enter/esc back")
	b.WriteString(clipCells(head, w, clipTail(a.th)) + "\n")

	for i, v := range a.state.Variances {
		marker := "\u25cb"
		if v.On {
			marker = "\u25cf"
		}
		if a.th.ASCII {
			marker = "[ ]"
			if v.On {
				marker = "[*]"
			}
		}
		cursor := a.th.Selector(i == a.groupSel)
		row := cursor + " " + marker + " " + padRight(v.Field, matchFieldWidth) +
			padRight(v.Side, matchSideWidth) + dashIf(a.th, v.Vary)
		if v.On {
			b.WriteString(clipCells(a.th.TextPrimary.Render(row), w, clipTail(a.th)) + "\n")
		} else {
			b.WriteString(clipCells(a.th.TextMuted.Render(row), w, clipTail(a.th)) + "\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}
