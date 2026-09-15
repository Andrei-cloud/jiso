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
	// height is the visible row count once SetHeight has been called;
	// 0 keeps the unbounded render (every row) that predates the scroll
	// primitives, so goldens stay byte-identical until a caller opts in
	// (Task 8.2b/pages). scrollOff is the wheel window offset ScrollBy
	// moves; both zero values are the historical default path.
	height    int
	scrollOff int
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

// SetHeight gives the table a visible row count: View then renders the
// window [scrollOff, scrollOff+h) and the pgup/pgdn step follows the
// height. Until it is set every row renders and the step stays 10, which
// is the shape the existing goldens were cut against.
func (m *Table) SetHeight(h int) {
	if h >= 1 {
		m.height = h
	}
	m.clampCursor()
}

// ScrollBy scrolls the visible row window by d rows (d>0 = down, toward
// later rows), clamped to the row range at the current height. With no
// height set the table already renders every row, so there is nothing to
// scroll and the offset stays put. The cursor is deliberately NOT dragged
// (the wheel owns the window); keyboard cursor moves drag it back into
// view through dragWindow, mirroring List.
func (m *Table) ScrollBy(d int) {
	m.scrollOff += d
	m.clampScroll()
}

// Window reports the wheel window: the absolute index of the first
// rendered row and how many rows View draws (List's Window contract,
// exposed for scroll indicators and page tests). With no height set the
// window is the full row range.
func (m *Table) Window() (top, count int) {
	lo, hi := m.rowWindow()

	return lo, hi - lo
}

// dragWindow drags the wheel window the least amount that keeps the
// cursor row rendered (List's clampWindow semantics, Task 8.2c): once a
// page gives the table a pane height, a keyboard cursor that walks past
// the window must not disappear into invisible rows. A no-op while no
// height is set, so the unwindowed default path never moves.
func (m *Table) dragWindow() {
	if m.height <= 0 {
		return
	}
	if m.cursor < m.scrollOff {
		m.scrollOff = m.cursor
	}
	if m.cursor >= m.scrollOff+m.height {
		m.scrollOff = m.cursor - m.height + 1
	}
	m.clampScroll()
}

// SetEmptyMessage overrides the empty-state line.
func (m *Table) SetEmptyMessage(s string) { m.empty = s }

// Len reports the row count.
func (m *Table) Len() int { return len(m.rows) }

// Cursor reports the absolute cursor row index (0 when empty).
func (m *Table) Cursor() int { return m.cursor }

// SetCursor moves the cursor (clamped) and drags the wheel window along
// so the cursor row stays rendered (List's SetCursor contract). The
// drag fires only on an ACTUAL cursor move: pages re-push their state
// after every root Update (syncPages → SetState → SetCursor with the
// same index), and re-setting the same cursor must leave the wheel
// window where the user left it (UAT round 8 Task 8.2c review, C1).
func (m *Table) SetCursor(i int) {
	old := m.cursor
	m.cursor = i
	m.clampCursor()
	if m.cursor != old {
		m.dragWindow()
	}
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
	m.clampScroll()
}

// clampScroll keeps the wheel window inside the rows at the current
// height (the same maxTop rule List uses: the window stays full at the
// bottom end).
func (m *Table) clampScroll() {
	if maxOff := max(0, len(m.rows)-m.visibleRows()); m.scrollOff > maxOff {
		m.scrollOff = maxOff
	}
	if m.scrollOff < 0 {
		m.scrollOff = 0
	}
}

// visibleRows is the rendered row count: the height SetHeight gave the
// table, or every row while no height is set.
func (m *Table) visibleRows() int {
	if m.height > 0 && m.height < len(m.rows) {
		return m.height
	}
	return len(m.rows)
}

// rowWindow is the absolute [lo, hi) row range the renderers draw. With
// no height set it is the full range, so the default path never windows.
func (m *Table) rowWindow() (lo, hi int) {
	lo = m.scrollOff
	hi = len(m.rows)
	if m.height > 0 && lo+m.height < hi {
		hi = lo + m.height
	}
	return lo, hi
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
	m.dragWindow()

	return m, nil
}

// rowsPerPageHint is the pgup/pgdn step: the height the table was given,
// or 10 while no caller has set one (the pre-scroll default the goldens
// were cut against).
func (m *Table) rowsPerPageHint() int {
	if m.height > 0 {
		return m.height
	}
	return 10
}

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

	lo, hi := m.rowWindow()
	for i := lo; i < hi; i++ {
		r := m.rows[i]
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
