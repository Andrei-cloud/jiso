package widgets

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/theme"
)

// Column describes one table column. Width is the display width of the
// cell content (padding excluded). In grid mode (the wireframe §B/§H
// tables) columns are separated by border columns and the selector lives
// inside the first cell; in flat mode columns are joined by single
// spaces behind a two-cell selector. Flex marks a column that may be
// shrunk to keep the table inside its width; when the total still
// exceeds the width, the remaining deficit is taken from the other
// columns right to left (floor 4 cells) before the last-resort fit
// clamp.
type Column struct {
	Title string
	Width int
	Flex  bool
	// AlignRight lays a numeric column out on its units. Left-aligned, "3ms",
	// "1.9ms" and "118ms" put the units in three different cells and the column
	// cannot be compared at a glance, which is the only reason to put numbers in
	// a column. The header follows the cells.
	AlignRight bool
}

// Row is one table row: one string per column (ANSI styling allowed,
// shorter rows are padded with empty cells).
type Row []string

// Table is a selectable, sortable table. Cells truncate with an ellipsis
// and NEVER wrap (a cell containing a newline has it flattened); rendered
// lines never exceed the table width. Row selection mirrors List: ▸/>
// marker + theme selection background, cursor clamped to [0, len-1].
// SortBy is a stable sort on the visible (ANSI-stripped) cell text with a
// ▲/▼ (ascii ^/v) indicator in the header. Build with NewTable.
type Table struct {
	theme   *theme.Theme
	cols    []Column
	rows    []Row
	width   int
	cursor  int
	sortCol int // -1 until SortBy
	sortAsc bool
	empty   string
	keys    navKeys
	grid    bool // bordered wireframe grid (default true); false = flat
	// focused is the pane-focus flag: an unfocused table renders its
	// cursor row as plain text (no selector marker, no selection
	// background) so a multi-pane page shows one obvious cursor (UAT
	// round 5). Single-table pages keep the default true.
	focused bool
}

// NewTable builds an empty table with the given total width.
func NewTable(th *theme.Theme, width int) *Table {
	if width < 4 {
		width = 4
	}

	return &Table{theme: th, width: width, sortCol: -1, empty: DefaultEmptyMessage, keys: newNavKeys(), grid: true, focused: true}
}

// SetFocused reports pane focus to the table: an unfocused table draws
// its cursor row as plain text (no ▸ marker, no selection background),
// making the active pane obvious on multi-pane pages (UAT round 5).
func (m *Table) SetFocused(on bool) { m.focused = on }

// SetGrid toggles the bordered wireframe grid (on by default). Panes
// that already draw their own box border (§G/§I/§J) pass false so the
// inner list stays quiet.
func (m *Table) SetGrid(on bool) { m.grid = on }

// SetColumns replaces the column definitions.
func (m *Table) SetColumns(cols []Column) {
	m.cols = cols
	if m.sortCol >= len(cols) {
		m.sortCol = -1
	}
}

// SetRows replaces the rows and clamps the cursor.
func (m *Table) SetRows(rows []Row) {
	m.rows = rows
	m.clampCursor()
}

// SetWidth resizes the table (SIGWINCH path) and re-clamps.
func (m *Table) SetWidth(width int) {
	if width >= 4 {
		m.width = width
	}
	m.clampCursor()
}

// SetEmptyMessage overrides the empty-state line.
func (m *Table) SetEmptyMessage(s string) { m.empty = s }

// Len reports the row count.
func (m *Table) Len() int { return len(m.rows) }

// Cursor reports the absolute cursor row index (0 when empty).
func (m *Table) Cursor() int { return m.cursor }

// SetCursor moves the cursor (clamped).
func (m *Table) SetCursor(i int) {
	m.cursor = i
	m.clampCursor()
}

// Selected returns the row under the cursor; ok is false when empty.
func (m *Table) Selected() (Row, bool) {
	if len(m.rows) == 0 {
		return nil, false
	}
	return m.rows[m.cursor], true
}

// SortCol is the column the table currently sorts by, or -1 when the rows are in
// their file order.
func (m *Table) SortCol() int { return m.sortCol }

// SortAsc is the direction SortCol sorts in: ascending, or descending when false.
func (m *Table) SortAsc() bool { return m.sortAsc }

// SortBy stably sorts rows by the visible text of column col (ascending
// when asc). Out-of-range columns are ignored. The cursor keeps its
// index (clamped) — pages that track identity re-select after sorting.
func (m *Table) SortBy(col int, asc bool) {
	if col < 0 || col >= len(m.cols) {
		return
	}
	m.sortCol, m.sortAsc = col, asc
	cell := func(r Row, c int) string {
		if c >= len(r) {
			return ""
		}
		return ansi.Strip(r[c])
	}
	sort.SliceStable(m.rows, func(i, j int) bool {
		if asc {
			return cell(m.rows[i], col) < cell(m.rows[j], col)
		}
		return cell(m.rows[i], col) > cell(m.rows[j], col)
	})
	m.clampCursor()
}

func (m *Table) clampCursor() {
	if n := len(m.rows); n == 0 {
		m.cursor = 0
	} else if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// Update advances the table for a message it understands: the navigation keys
// move the row cursor, mirroring List. Anything else is returned untouched with
// a nil command, which is what lets a page pass every message through without
// swallowing the ones the table does not own.
func (m *Table) Update(msg tea.Msg) (*Table, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch m.keys.resolve(km) {
	case navUp:
		m.cursor--
	case navDown:
		m.cursor++
	case navPageUp:
		m.cursor -= max(1, m.rowsPerPageHint())
	case navPageDown:
		m.cursor += max(1, m.rowsPerPageHint())
	case navHome:
		m.cursor = 0
	case navEnd:
		m.cursor = len(m.rows) - 1
	case navNone:
		return m, nil
	}
	m.clampCursor()

	return m, nil
}

// rowsPerPageHint keeps pgup/pgdn meaningful without a height field;
// pages that track height can call SetCursor themselves.
func (m *Table) rowsPerPageHint() int { return 10 }

// resolvedWidths applies the flex rule: flexible columns give first
// (right to left, floor 4), then the remaining deficit is taken from
// every column right to left (floor 4). The grid's border budget is
// counted when grid mode is on.
func (m *Table) resolvedWidths() []int {
	ws := make([]int, len(m.cols))
	total := 2 + len(m.cols) - 1 // selector cells + separators
	if m.grid {
		// left border + one border column per field + per-cell padding
		// + the selector inside the first field.
		total = 3 + 3*len(m.cols)
	}
	for i, c := range m.cols {
		if c.Width < 1 {
			ws[i] = 1
		} else {
			ws[i] = c.Width
		}
		total += ws[i]
	}
	if total > m.width {
		deficit := total - m.width
		for pass := 0; pass < 2 && deficit > 0; pass++ {
			for i := len(ws) - 1; i >= 0 && deficit > 0; i-- {
				if pass == 0 && !m.cols[i].Flex {
					continue
				}
				give := min(deficit, max(0, ws[i]-4))
				ws[i] -= give
				deficit -= give
			}
		}
	}

	return ws
}

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

// View renders the grid (wireframe tables) or the flat list (boxed
// panes), clipped line-by-line so nothing ever overflows or wraps.
func (m *Table) View() string {
	if len(m.rows) == 0 {
		return m.theme.TextMuted.Render(m.empty)
	}
	if m.grid {
		return m.renderGrid()
	}

	return m.renderFlat()
}

// renderFlat is the quiet pane form: header + rows joined by spaces
// behind a two-cell selector, no borders.
func (m *Table) renderFlat() string {
	ws := m.resolvedWidths()
	tail := truncateTail(m.theme)

	header := make([]string, len(m.cols))
	for i, c := range m.cols {
		text := clip(c.Title, max(1, ws[i]-2), tail)
		if i == m.sortCol {
			text += " " + sortGlyph(m.theme, m.sortAsc)
		}
		header[i] = c.padCell(text, ws[i])
	}
	// one line per row plus the header: the table re-renders every frame, so the
	// capacity is the number of rows rather than a growth series of copies
	lines := make([]string, 1, 1+len(m.rows))
	lines[0] = m.theme.TextMuted.Render(pad(m.fit("  "+strings.Join(header, " ")), m.width))

	for i, r := range m.rows {
		cells := make([]string, len(m.cols))
		for j := range m.cols {
			var raw string
			if j < len(r) {
				raw = r[j]
			}
			cells[j] = m.cols[j].padCell(clip(raw, ws[j], tail), ws[j])
		}
		selected := m.focused && i == m.cursor
		line := pad(m.fit(m.theme.Selector(selected)+strings.Join(cells, " ")), m.width)
		if selected {
			line = m.theme.Selection.Render(line)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// renderGrid draws the wireframe grid: top rule, header row, header
// rule, data rows (selector ▸ inside the first cell, theme selection
// background), bottom rule. Cells truncate with an ellipsis and never
// wrap; the sort caret is NOT rendered here (pages put it in their
// title).
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
			// The last cell of the header has to sit over the last cell of its
			// numbers, so the header cannot keep the space it would otherwise
			// wear on the right.
			header[i] = c.padCell(text, fields[i])

			continue
		}

		header[i] = pad(" "+text+" ", fields[i])
	}

	// top rule, header, mid rule, one per row, bottom rule
	lines := make([]string, 1, 3+len(m.rows)+1)
	lines[0] = m.rule(g, g.topLeft, g.topMid, g.topRight, fields)
	lines = append(lines, m.gridRow(g, header))
	lines = append(lines, m.rule(g, g.midLeft, g.midMid, g.midRight, fields))

	for i, r := range m.rows {
		cells := make([]string, len(m.cols))
		for j := range m.cols {
			var raw string
			if j < len(r) {
				raw = r[j]
			}
			field := " "
			if j == 0 {
				field = m.theme.Selector(m.focused && i == m.cursor)
			}
			field += clip(raw, ws[j], tail)
			field = m.cols[j].padCell(field, fields[j])
			if m.focused && i == m.cursor {
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

	return strings.Join(lines, "\n")
}

// fit is the last-resort horizontal clamp: even when no flex column can
// absorb the shortfall, a line never exceeds the table width.
func (m *Table) fit(line string) string {
	if lipgloss.Width(line) > m.width {
		return ansi.Truncate(line, m.width, "")
	}
	return line
}

// padCell lays one clipped cell out in w cells, honouring the alignment the column
// declares. Styled spans are measured rather than counted, so a status cell cannot
// shift the column, and a cell already at the full width is returned untouched.
//
// It is a method rather than a helper taking an align flag because the column is
// the thing that knows how its cells are laid out, and because a boolean control
// parameter at four call sites is the shape this linter is right about.
func (c Column) padCell(s string, w int) string {
	if !c.AlignRight {
		return pad(s, w)
	}

	if gap := w - lipgloss.Width(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}

	return s
}
