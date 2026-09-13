// inspector_fields.go renders one fields-tree row: the selector marker,
// the field number, the (indented, expand-marked) name, and the value
// cell — auto rows as "raw → preview", masked rows pre-masked by root
// with a "(masked)" tag (never colour alone), error rows red-lined with
// the error symbol.
package pages

import (
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
)

// fieldNumWidth/fieldNameWidth are the fields-table column widths; the
// value cell takes the rest and is clipped by clipBlockStyled.
const (
	fieldNumWidth  = 3
	fieldNameWidth = 28
)

// maskedTag labels a masked display value (the non-colour signal).
const maskedTag = "(masked)"

// fieldLine renders one visible row; selected rows get the widget-list
// treatment (Selector marker + Selection background padded to the pane
// width w).
func (in *Inspector) fieldLine(vr visRow, selected bool, w int) string {
	cells := []string{
		padCell(dashIf(in.th, vr.row.Num), fieldNumWidth),
		in.nameCell(vr),
	}
	line := strings.Join(cells, " ")

	value := in.valueCell(vr.row)
	if vr.row.Error != "" {
		value += "  " + in.th.Status(theme.KindError, vr.row.Error)
	}

	return pad(in.th.Selector(selected)+line+" "+value, w)
}

// nameCell renders the indented name with the composite expand marker
// (collapsed ▸ / expanded ▾; ">" / "v" in ascii mode).
func (in *Inspector) nameCell(vr visRow) string {
	indent := strings.Repeat("  ", vr.depth)
	marker := "  "
	if vr.composite {
		if vr.expanded {
			marker = expandMark(in.th, true)
		} else {
			marker = expandMark(in.th, false)
		}
	}

	return padCell(indent+marker+dashIf(in.th, vr.row.Name), fieldNameWidth)
}

// expandMark is the two-cell composite marker for th's glyph mode.
func expandMark(th *theme.Theme, expanded bool) string {
	if th.ASCII {
		if expanded {
			return "v "
		}

		return "> "
	}
	if expanded {
		return "▾ "
	}

	return "▸ "
}

// valueCell renders the display value: auto rows carry the
// "raw → preview" prefix, masked rows keep root's pre-masked display in
// the warn style plus the (masked) tag, plain rows are primary text.
func (in *Inspector) valueCell(r FieldRow) string {
	switch {
	case r.Auto:
		raw := dashIf(in.th, r.RawPreview)
		if raw == "" {
			raw = "auto"
		}

		return in.th.Dim.Render(raw+" "+autoArrow(in.th)) + " " +
			in.th.TextPrimary.Render(in.displayCell(r))
	case r.Masked:
		return in.th.StatusWarn.Render(in.displayCell(r)) + " " +
			in.th.Deemphasized.Render(maskedTag)
	default:
		return in.th.TextPrimary.Render(dashIf(in.th, r.Display))
	}
}

// padCell right-pads s to n cells (lipgloss.Width keeps styled text
// honest; over-long cells stay intact — the block clip truncates).
func padCell(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}

	return s
}

// pad right-pads a whole line to the pane width (widget-list idiom:
// padded lines make the selection background span the row).
func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}

	return s
}

// describeLine renders one Describe output line as a fields-pane row:
// the selector marker, the label and dot padding deemphasized, the
// value primary; the whole line is clipped+padded to w.
func (in *Inspector) describeLine(ln string, selected bool, w int) string {
	styled := in.th.Deemphasized.Render(ln)
	if head, value, ok := strings.Cut(ln, ": "); ok {
		styled = in.th.Deemphasized.Render(head+": ") + in.th.TextPrimary.Render(value)
	}

	return pad(in.th.Selector(selected)+clipCells(styled, max(w-2, 1), clipTail(in.th)), w)
}
