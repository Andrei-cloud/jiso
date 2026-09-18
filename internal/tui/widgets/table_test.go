package widgets

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/theme"
)

func tableFixture(th *theme.Theme, width int) *Table {
	m := NewTable(th, width)
	m.SetColumns([]Column{{Title: "ID", Width: 4}, {Title: "NAME", Width: 20, Flex: true}, {Title: "STATUS", Width: 8}})
	m.SetRows([]Row{
		{"a-1", "alpha", "ok"},
		{"b-22", "bravo-charlie-delta-echo-foxtrot", "timeout"},
		{"c-3", "charlie", "ok"},
		{"d-4", "delta", "warn"},
	})
	return m
}

// TestTableSetFocusedHidesCursor: an unfocused table
// renders its cursor row as plain text (no selector marker, no
// selection background) so a multi-pane page shows one obvious cursor.
func TestTableSetFocusedHidesCursor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		grid bool
	}{
		{name: "flat", grid: false},
		{name: "grid", grid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tableFixture(asciiTheme(t), 40)
			m.SetGrid(tc.grid)
			focused := m.View()
			if !strings.Contains(focused, theme.ASCIISelected) {
				t.Fatalf("a fresh table must be focused by default (selector marker):\n%s", focused)
			}

			m.SetFocused(false)
			if strings.Contains(m.View(), theme.ASCIISelected) {
				t.Fatalf("unfocused table must hide the selector marker:\n%s", m.View())
			}
			m.SetFocused(true)
			if !strings.Contains(m.View(), theme.ASCIISelected) {
				t.Fatal("SetFocused(true) must restore the marker")
			}
		})
	}
}

func TestTableSortStableBothDirections(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		asc  bool
		want []string
	}{
		{"asc", true, []string{"b", "d", "a", "c"}},
		{"desc", false, []string{"a", "c", "b", "d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewTable(asciiTheme(t), 40)
			m.SetColumns([]Column{{Title: "K", Width: 2}, {Title: "V", Width: 2}})
			m.SetRows([]Row{{"a", "2"}, {"b", "1"}, {"c", "2"}, {"d", "1"}})
			m.SortBy(1, tc.asc)
			var got []string
			for _, r := range m.rows {
				got = append(got, r[0])
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("order %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTableSortIndicator(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		th   func(*testing.T) *theme.Theme
		asc  bool
		want string
	}{
		{"asc-tc", tcTheme, true, GlyphSortAsc},
		{"desc-tc", tcTheme, false, GlyphSortDesc},
		{"asc-ascii", asciiTheme, true, ASCIISortAsc},
		{"desc-ascii", asciiTheme, false, ASCIISortDesc},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th := tc.th(t)
			m := tableFixture(th, 40)
			m.SortBy(1, tc.asc)
			if got := sortGlyph(th, m.SortAsc()); got != tc.want {
				t.Errorf("SortGlyph %q, want %q", got, tc.want)
			}
			if strings.Contains(strip(lines(m.View())[1]), tc.want) {
				t.Errorf("sort caret leaked into the grid header; it belongs in the page title")
			}
		})
	}
}

func TestTableSortOutOfRangeIgnored(t *testing.T) {
	t.Parallel()

	m := tableFixture(asciiTheme(t), 40)
	before := m.View()
	m.SortBy(99, true)
	m.SortBy(-1, false)
	if m.View() != before || m.SortCol() != -1 {
		t.Fatal("out-of-range sort mutated the table")
	}
}

func TestTableTruncationNeverWraps(t *testing.T) {
	t.Parallel()

	th := tcTheme(t)
	m := NewTable(th, 40)
	m.SetColumns([]Column{{Title: "A", Width: 6}, {Title: "B", Width: 30, Flex: true}, {Title: "C", Width: 8}})
	m.SetRows([]Row{
		{"id-123456789", strings.Repeat("x", 60), "st"},
		{"nl", "first\nsecond", "st"},
		{"styled", th.Accent.Render(strings.Repeat("y", 60)), "st"},
	})
	ls := lines(m.View())
	if len(ls) != 7 {
		t.Fatalf("wrapped into %d lines, want 7 (rules + header + 3 rows)", len(ls))
	}
	for _, l := range ls {
		if w := ansi.StringWidth(l); w > 40 {
			t.Errorf("line %q width %d > 40", strip(l), w)
		}
	}
	body := strings.Join(ls[3:6], "\n")
	if strings.Count(strip(body), theme.GlyphEllipsis) < 3 {
		t.Errorf("expected ellipses on all three long cells:\n%s", strip(body))
	}
	if !strings.Contains(ls[5], "\x1b") {
		t.Errorf("styled cell lost its styling: %q", ls[5])
	}
}

func TestTableFlexShrinksOnlyLastFlexColumn(t *testing.T) {
	t.Parallel()

	m := NewTable(asciiTheme(t), 35)
	m.SetColumns([]Column{{Title: "A", Width: 10, Flex: true}, {Title: "B", Width: 30, Flex: true}})
	m.SetRows([]Row{{"a", "b"}})
	got := m.resolvedWidths()
	if got[0] != 10 || got[1] != 16 {
		t.Fatalf("widths %v, want [10 16] (only last flex gives)", got)
	}
}

func TestTableExactFitAfterFlex(t *testing.T) {
	t.Parallel()

	m := tableFixture(asciiTheme(t), 40)
	var width int
	for _, l := range lines(m.View()) {
		w := ansi.StringWidth(l)
		if w > 40 {
			t.Fatalf("overflow: line %q width %d", strip(l), w)
		}
		if width == 0 {
			width = w
		} else if w != width {
			t.Fatalf("grid ragged: line %q width %d, want %d", strip(l), w, width)
		}
	}
}

func TestTableNoFlexStillNeverOverflows(t *testing.T) {
	t.Parallel()

	m := NewTable(asciiTheme(t), 30)
	m.SetColumns([]Column{{Title: "A", Width: 20}, {Title: "B", Width: 20}})
	m.SetRows([]Row{{strings.Repeat("a", 20), strings.Repeat("b", 20)}})
	for _, l := range lines(m.View()) {
		if w := ansi.StringWidth(l); w > 30 {
			t.Fatalf("overflow: line %q width %d", strip(l), w)
		}
	}
}

func TestTableRowCursorClamp(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  tea.KeyPressMsg
		want int
	}{
		{"down-past-end", special(tea.KeyDown), 3},
		{"j-past-end", ch('j'), 3},
		{"up-past-start", special(tea.KeyUp), 0},
		{"k-past-start", ch('k'), 0},
		{"home", special(tea.KeyHome), 0},
		{"end", special(tea.KeyEnd), 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tableFixture(asciiTheme(t), 40)
			if strings.Contains(tc.name, "past-start") {
				m.SetCursor(0)
			} else {
				m.SetCursor(3)
			}
			for i := 0; i < 20; i++ {
				m.Update(tc.msg)
			}
			if m.Cursor() != tc.want {
				t.Fatalf("cursor %d, want %d", m.Cursor(), tc.want)
			}
			if _, ok := m.Selected(); !ok {
				t.Fatal("no selection with rows present")
			}
		})
	}
}

func TestTableEmptyState(t *testing.T) {
	t.Parallel()

	m := tableFixture(asciiTheme(t), 40)
	m.SetRows(nil)
	if got := m.View(); got != DefaultEmptyMessage {
		t.Fatalf("empty %q, want %q", got, DefaultEmptyMessage)
	}
	m.SetEmptyMessage("No workers. b background-send.")
	if got := m.View(); got != "No workers. b background-send." {
		t.Fatalf("custom empty %q", got)
	}
	m.Update(special(tea.KeyEnd))
	if m.Cursor() != 0 {
		t.Fatal("cursor moved on empty table")
	}
}

func TestTableAsciiIsPlainWithAsciiGlyphs(t *testing.T) {
	t.Parallel()

	m := tableFixture(asciiTheme(t), 30) // forces truncation + flex shrink
	m.SortBy(1, false)
	out := m.View()
	if strings.ContainsAny(out, "\x1b\u009b") {
		t.Errorf("ascii render contains escapes: %q", out)
	}
	for _, r := range out {
		if r > 127 {
			t.Fatalf("ascii render contains non-ASCII rune %q", r)
		}
	}
	if !strings.Contains(out, theme.ASCIIEllipsis) {
		t.Errorf("ascii glyphs missing:\n%s", out)
	}
}

func TestTableSelectionOnlyOnCursor(t *testing.T) {
	t.Parallel()

	m := tableFixture(tcTheme(t), 40)
	m.SetCursor(2)
	data := lines(m.View())[3:]
	selected := -1
	for i, l := range data {
		// The selection background may only appear on the cursor row;
		// border colouring is expected everywhere.
		if strings.Contains(l, "48;2") {
			if selected >= 0 {
				t.Fatalf("selection background on rows %d and %d", selected, i)
			}
			selected = i
		}
	}
	if selected == -1 {
		t.Fatal("no selection background rendered")
	}
	if !strings.HasPrefix(strip(data[selected]), "|"+theme.GlyphSelected+" ") &&
		!strings.HasPrefix(strip(data[selected]), "│"+theme.GlyphSelected+" ") {
		t.Errorf("selector missing on cursor row: %q", data[selected])
	}
}

// TestTableAlignRight pins a numeric column to its units, in both table shapes and
// including the header. Left-aligned, "3ms" / "1.9ms" / "118ms" put the units in
// three different cells, so the column cannot be compared at a glance -- the one
// thing a column of numbers has to support. The empty cell is included because a
// dash for "no latency" must not be the thing that decides where the column sits.
func TestTableAlignRight(t *testing.T) {
	t.Parallel()

	values := []string{"3ms", "1.9ms", "118ms"}

	for _, grid := range []bool{true, false} {
		m := NewTable(asciiTheme(t), 44)
		m.SetGrid(grid)
		m.SetColumns([]Column{
			{Title: "TX", Width: 8},
			{Title: "LATENCY", Width: 9, AlignRight: true},
		})
		m.SetRows([]Row{
			{"Purchase", "3ms"},
			{"Purchase+2", "1.9ms"},
			{"Sign On", "118ms"},
			{"Echo", "-"},
		})

		lines := strings.Split(strip(m.View()), "\n")

		// Where each value's last cell lands. Measured on the render, because a
		// column's position is what the operator sees, not what padCell was asked
		// to do.
		ends := map[string]int{}
		for _, l := range lines {
			for _, v := range append(values, "LATENCY") {
				if i := strings.LastIndex(l, v); i >= 0 {
					ends[v] = i + len(v)
				}
			}
		}

		for _, v := range append(values, "LATENCY") {
			if _, ok := ends[v]; !ok {
				t.Fatalf("grid=%v: %q never reached the render\n%s", grid, v, strings.Join(lines, "\n"))
			}
		}

		for _, v := range values {
			if ends[v] != ends["LATENCY"] {
				t.Errorf("grid=%v: %q ends at cell %d but its header ends at %d; a numeric column lines its units up under the title\n%s",
					grid, v, ends[v], ends["LATENCY"], strings.Join(lines, "\n"))
			}
		}
	}
}

// tallTableFixture is a 25-row table (labels r00..r24) in either mode.
func tallTableFixture(t *testing.T, grid bool) *Table {
	t.Helper()

	m := NewTable(asciiTheme(t), 40)
	m.SetGrid(grid)
	m.SetColumns([]Column{{Title: "K", Width: 4}})
	rows := make([]Row, 0, 25)
	for i := 0; i < 25; i++ {
		rows = append(rows, Row{fmt.Sprintf("r%02d", i)})
	}
	m.SetRows(rows)
	return m
}

// Unset height keeps the unbounded render (goldens byte-identical);
// SetHeight windows View and the pgup/pgdn step.
func TestTableSetHeightWindowsRows(t *testing.T) {
	t.Parallel()

	for _, grid := range []bool{true, false} {
		m := tallTableFixture(t, grid)

		if got := m.rowsPerPageHint(); got != 10 {
			t.Fatalf("grid=%v: hint %d, want the unset fallback 10", grid, got)
		}
		// Every row renders: grid adds 4 rule lines, flat the header.
		wantLines := 26
		if grid {
			wantLines = 29
		}
		if got := len(lines(strip(m.View()))); got != wantLines {
			t.Fatalf("grid=%v: unset height must render every row: %d lines, want %d", grid, got, wantLines)
		}

		m.SetHeight(5)
		if got := m.rowsPerPageHint(); got != 5 {
			t.Fatalf("grid=%v: hint after SetHeight %d, want 5", grid, got)
		}
		wantLines = 6
		if grid {
			wantLines = 9
		}
		view := strip(m.View())
		if got := len(lines(view)); got != wantLines {
			t.Fatalf("grid=%v: height 5 must render 5 rows: %d lines, want %d\n%s", grid, got, wantLines, view)
		}
		if !strings.Contains(view, "r00") || !strings.Contains(view, "r04") || strings.Contains(view, "r05") {
			t.Fatalf("grid=%v: fresh window must be [0,5):\n%s", grid, view)
		}
	}
}

// ScrollBy(d) moves the row window (d>0 = down), clamped at both ends.
func TestTableScrollByClampsToWindow(t *testing.T) {
	t.Parallel()

	for _, grid := range []bool{true, false} {
		m := tallTableFixture(t, grid)

		// No height: all rows visible, nothing to scroll.
		m.ScrollBy(3)
		if !strings.Contains(strip(m.View()), "r00") {
			t.Fatalf("grid=%v: an unwindowed table must not scroll its first row away", grid)
		}

		m.SetHeight(5)

		m.ScrollBy(2)
		view := strip(m.View())
		if !strings.Contains(view, "r02") || !strings.Contains(view, "r06") {
			t.Fatalf("grid=%v: window after ScrollBy(2) must span r02..r06:\n%s", grid, view)
		}
		if strings.Contains(view, "r01") || strings.Contains(view, "r07") {
			t.Fatalf("grid=%v: ScrollBy(2) must window to [2,7):\n%s", grid, view)
		}

		m.ScrollBy(-100)
		view = strip(m.View())
		if !strings.Contains(view, "r00") || strings.Contains(view, "r05") {
			t.Fatalf("grid=%v: ScrollBy must clamp at the top:\n%s", grid, view)
		}

		m.ScrollBy(100)
		view = strip(m.View())
		if !strings.Contains(view, "r24") || strings.Contains(view, "r19") {
			t.Fatalf("grid=%v: ScrollBy must clamp at the bottom window [20,25):\n%s", grid, view)
		}

		// Growing the height pulls the stale offset back; window stays full.
		m.SetHeight(10)
		view = strip(m.View())
		if !strings.Contains(view, "r15") || strings.Contains(view, "r14") || !strings.Contains(view, "r24") {
			t.Fatalf("grid=%v: SetHeight must re-clamp a stale offset to [15,25):\n%s", grid, view)
		}
	}
}

// TotalWidth is the width the current columns ask for: every requested
// cell width (never below 1) plus the mode's chrome. A caller that must
// not shrink columns below what they were given sizes the table by at
// least this.
func TestTableTotalWidth(t *testing.T) {
	t.Parallel()

	m := NewTable(asciiTheme(t), 40)
	m.SetColumns([]Column{
		{Title: "NAME", Width: 10},
		{Title: "MTI", Width: 4},
		{Title: "DESCRIPTION", Width: 12, Flex: true},
		{Title: "DATASET", Width: 8},
		{Title: "SPEC", Width: 8},
	})
	if got := m.TotalWidth(); got != 60 {
		t.Errorf("grid total = %d, want 60 (42 cells + 18 chrome)", got)
	}

	m.SetGrid(false)
	if got := m.TotalWidth(); got != 48 {
		t.Errorf("flat total = %d, want 48 (42 cells + 2 selector + 4 separators)", got)
	}

	m.SetGrid(true)
	m.SetColumns([]Column{{Width: 0}, {Width: 5}})
	if got := m.TotalWidth(); got != 1+5+GridChrome(2) {
		t.Errorf("total = %d, want %d (a zero width counts as 1)", got, 1+5+GridChrome(2))
	}
}

// GridChrome is the bordered grid's non-cell width cost: the left border
// + one border column per field + per-cell padding + the selector inside
// the first field. resolvedWidths budgets exactly this, so the two can
// never drift.
func TestGridChrome(t *testing.T) {
	t.Parallel()

	if got := GridChrome(5); got != 18 {
		t.Errorf("GridChrome(5) = %d, want 18", got)
	}

	m := NewTable(asciiTheme(t), 40)
	m.SetColumns([]Column{{Width: 10, Flex: true}, {Width: 4}})
	m.SetRows([]Row{{"a", "b"}})
	for _, l := range lines(m.View()) {
		if w := ansi.StringWidth(l); w != 10+4+GridChrome(2) {
			t.Fatalf("drawn line %d cells, want the natural %d: %q",
				w, 10+4+GridChrome(2), strip(l))
		}
	}
}
