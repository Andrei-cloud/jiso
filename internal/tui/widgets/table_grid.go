package widgets

import (
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/geom"
)

// This file holds the wireframe grid renderer for Table; the shared
// plumbing (column definitions, clamping, width resolution, the flat
// renderer, the fit clamp) stays in table.go.

// gridGlyphs are the table's border runes for the theme's glyph mode.
type gridGlyphs struct {
	topLeft, topMid, topRight string
	midLeft, midMid, midRight string
	botLeft, botMid, botRight string
	vert, horiz               string
}

var (
	gridRounded = gridGlyphs{"┌", "┬", "┐", "├", "┼", "┤", "└", "┴", "┘", "│", "─"}
	gridASCII   = gridGlyphs{"+", "+", "+", "+", "+", "+", "+", "+", "+", "|", "-"}
)

func (m *Table) gridGlyphs() gridGlyphs {
	if m.theme.ASCII {
		return gridASCII
	}

	return gridRounded
}

// border styles the grid runes with the subtle border colour (identity
// render under a colorless profile).
func (m *Table) border(s string) string {
	return lipgloss.NewStyle().Foreground(m.theme.SubtleBorder.GetBorderTopForeground()).Render(s)
}

// rule builds a horizontal grid line (top/mid/bottom) over the fields
// (field widths already include their padding).
func (m *Table) rule(g gridGlyphs, left, mid, right string, fields []int) string {
	var b strings.Builder
	b.WriteString(left)
	for i, w := range fields {
		b.WriteString(strings.Repeat(g.horiz, w))
		if i == len(fields)-1 {
			b.WriteString(right)
		} else {
			b.WriteString(mid)
		}
	}

	return m.border(b.String())
}

// gridRow joins already-padded field contents with border columns:
// "│" field "│" field "│".
func (m *Table) gridRow(g gridGlyphs, cells []string) string {
	var b strings.Builder
	b.WriteString(m.border(g.vert))
	for _, c := range cells {
		b.WriteString(c)
		b.WriteString(m.border(g.vert))
	}

	return b.String()
}

// renderGrid draws the wireframe grid: top rule, header row, mid rule,
// data rows, bottom rule (chrome rows cost a fixed 4 lines). Cells
// truncate and never wrap; the sort caret is NOT rendered here. Only the
// rowWindow slice is drawn once a height is set.
func (m *Table) renderGrid() string {
	ws := m.resolvedWidths()
	g := m.gridGlyphs()
	tail := truncateTail(m.theme)

	fields := make([]int, len(m.cols))
	for i := range m.cols {
		fields[i] = ws[i] + 2
		if i == 0 {
			fields[0] += 2
		}
	}

	header := make([]string, len(m.cols))
	for i, c := range m.cols {
		text := clip(c.Title, max(1, ws[i]), tail)
		if c.AlignRight {
			// The right-aligned header cell sits over the last cell of its
			// numbers, not the space it would otherwise wear on the right.
			header[i] = c.padCell(text, fields[i])

			continue
		}

		header[i] = pad(" "+text+" ", fields[i])
	}

	// top rule, header, mid rule, one per visible row, bottom rule
	lo, hi := m.rowWindow()
	lines := make([]string, 1, 3+hi-lo+1)
	lines[0] = m.rule(g, g.topLeft, g.topMid, g.topRight, fields)
	lines = append(lines, m.gridRow(g, header))
	lines = append(lines, m.rule(g, g.midLeft, g.midMid, g.midRight, fields))

	for i, r := range m.rows[lo:hi] {
		cells := make([]string, len(m.cols))
		for j := range m.cols {
			var raw string
			if j < len(r) {
				raw = r[j]
			}
			field := " "
			if j == 0 {
				field = m.theme.Selector(m.focused && lo+i == m.cursor)
			}
			field += clip(raw, ws[j], tail)
			field = m.cols[j].padCell(field, fields[j])
			if m.focused && lo+i == m.cursor {
				field = m.theme.Selection.Render(field)
			}
			cells[j] = field
		}
		lines = append(lines, m.gridRow(g, cells))
	}
	lines = append(lines, m.rule(g, g.botLeft, g.botMid, g.botRight, fields))

	for i, l := range lines {
		lines[i] = m.fit(l)
	}

	// Record the drawn data-row rects for the click hit map.
	m.recordGridRows(lines, lo, hi)

	return strings.Join(lines, "\n")
}

// recordGridRows records the drawn data-row rects: lines[0..2] are the
// top rule, header row and mid rule, so each row starts one cell inside
// the border, under the header; the bottom rule is not a row.
func (m *Table) recordGridRows(lines []string, lo, hi int) {
	m.rowHits = m.rowHits[:0]
	for i := lo; i < hi; i++ {
		m.rowHits = append(m.rowHits, RowHit{
			Rect:  geom.Rect{X: 1, Y: 3 + i - lo, W: max(lipgloss.Width(lines[3+i-lo])-2, 1), H: 1},
			Index: i,
		})
	}
}
