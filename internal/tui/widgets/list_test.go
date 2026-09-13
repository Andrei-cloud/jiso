package widgets

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/theme"
)

func items(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{Label: fmt.Sprintf("item-%05d", i), Data: i}
	}
	return out
}

// checkListInvariants asserts cursor/window clamping.
func checkListInvariants(t *testing.T, m *List) {
	t.Helper()
	n, top, count := m.Len(), 0, 0
	top, count = m.Window()
	cur := m.Cursor()
	if n == 0 {
		if cur != 0 || top != 0 || count != 0 {
			t.Fatalf("empty list: cursor=%d top=%d count=%d", cur, top, count)
		}
		return
	}
	if cur < 0 || cur >= n {
		t.Fatalf("cursor %d out of [0,%d)", cur, n)
	}
	if want := min(m.height, n); count != want {
		t.Fatalf("visible count %d, want %d", count, want)
	}
	if top < 0 || top+count > n {
		t.Fatalf("window [%d,%d) past ends of %d items", top, top+count, n)
	}
	if cur < top || cur >= top+count {
		t.Fatalf("cursor %d outside window [%d,%d)", cur, top, top+count)
	}
}

func TestListCursorClampAtBothEnds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		key  tea.KeyPressMsg
		want int
	}{
		{"down-past-end", special(tea.KeyDown), 9},
		{"j-past-end", ch('j'), 9},
		{"up-past-start", special(tea.KeyUp), 0},
		{"k-past-start", ch('k'), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewList(asciiTheme(t), 20, 4)
			m.SetItems(items(10))
			m.SetCursor(tc.want)
			for i := 0; i < 30; i++ {
				m.Update(tc.key)
				checkListInvariants(t, m)
			}
			if m.Cursor() != tc.want {
				t.Fatalf("cursor %d, want %d", m.Cursor(), tc.want)
			}
		})
	}
}

func TestListNavigationKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  tea.KeyPressMsg
		want int
	}{
		{"down", special(tea.KeyDown), 1},
		{"j", ch('j'), 1},
		{"up", special(tea.KeyUp), 0},
		{"k", ch('k'), 0},
		{"pgdn", special(tea.KeyPgDown), 4},
		{"pgup-from-9", special(tea.KeyPgUp), 5},
		{"home", special(tea.KeyHome), 0},
		{"end", special(tea.KeyEnd), 9},
		{"unknown-ignored", ch('x'), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewList(asciiTheme(t), 20, 5)
			m.SetItems(items(10))
			if tc.name == "pgup-from-9" {
				m.SetCursor(9)
			}
			m.Update(tc.msg)
			if m.Cursor() != tc.want {
				t.Fatalf("cursor %d, want %d", m.Cursor(), tc.want)
			}
			checkListInvariants(t, m)
		})
	}
}

func TestListWindowNeverPastEnds(t *testing.T) {
	t.Parallel()

	m := NewList(asciiTheme(t), 20, 4)
	m.SetItems(items(10))
	for _, msg := range []tea.KeyPressMsg{
		special(tea.KeyEnd), special(tea.KeyDown), special(tea.KeyDown),
		special(tea.KeyHome), special(tea.KeyUp), special(tea.KeyPgUp),
		special(tea.KeyEnd), special(tea.KeyPgDown),
	} {
		m.Update(msg)
		checkListInvariants(t, m)
	}
}

func TestListVirtualizationTenK(t *testing.T) {
	t.Parallel()

	const n = 10000
	m := NewList(asciiTheme(t), 24, 10)
	m.SetItems(items(n))

	out := m.View()
	if got := len(lines(out)); got != 10 {
		t.Fatalf("rendered %d lines, want 10 (windowed)", got)
	}
	for _, want := range []string{"item-00000", "item-00009"} {
		if !strings.Contains(out, want) {
			t.Errorf("top window missing %s", want)
		}
	}
	for _, bad := range []string{"item-00010", "item-00011", "item-09999"} {
		if strings.Contains(out, bad) {
			t.Errorf("top window contains invisible %s", bad)
		}
	}

	m.Update(special(tea.KeyEnd))
	out = m.View()
	if !strings.Contains(out, "item-09999") || strings.Contains(out, "item-09989") {
		t.Errorf("bottom window wrong:\n%s", out)
	}

	// Bounded time: the whole point of virtualization.
	start := time.Now()
	for i := 0; i < 2000; i++ {
		_ = m.View()
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("2000 Views of a 10k list took %v", el)
	}
}

func TestListEmptyState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  string
		want string
	}{
		{"default", "", DefaultEmptyMessage},
		{"custom", "No transactions. Press t to pick a tx file.", "No transactions. Press t to pick a tx file."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewList(asciiTheme(t), 30, 5)
			if tc.msg != "" {
				m.SetEmptyMessage(tc.msg)
			}
			for _, k := range []tea.KeyPressMsg{special(tea.KeyDown), ch('j'), special(tea.KeyEnd), special(tea.KeyPgDown)} {
				m.Update(k)
				checkListInvariants(t, m)
			}
			if got := m.View(); got != tc.want {
				t.Fatalf("empty view %q, want %q", got, tc.want)
			}
		})
	}
}

func TestListAsciiVsTruecolorGlyphs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		th        func(*testing.T) *theme.Theme
		cursorPfx string
		escapes   bool
	}{
		{"ascii", asciiTheme, "> ", false},
		{"truecolor", tcTheme, "▸ ", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewList(tc.th(t), 20, 3)
			m.SetItems(items(5))
			m.SetCursor(1)
			ls := lines(m.View())
			if !strings.HasPrefix(strip(ls[1]), tc.cursorPfx) {
				t.Errorf("cursor line %q, want prefix %q", strip(ls[1]), tc.cursorPfx)
			}
			if strings.HasPrefix(strip(ls[0]), "▸") || strings.HasPrefix(strip(ls[0]), ">") {
				t.Errorf("non-cursor line %q carries a selector", strip(ls[0]))
			}
			hasEsc := strings.Contains(ls[1], "\x1b")
			if hasEsc != tc.escapes {
				t.Errorf("cursor line escapes=%v, want %v", hasEsc, tc.escapes)
			}
			if strings.Contains(ls[0], "\x1b") {
				t.Errorf("non-cursor line %q carries styling", ls[0])
			}
		})
	}
}

func TestListLabelTruncationNeverWraps(t *testing.T) {
	t.Parallel()

	m := NewList(asciiTheme(t), 12, 3)
	m.SetItems([]Item{{Label: "0123456789abcdefghijklmnop"}, {Label: "short\nnewline"}})
	ls := lines(m.View())
	if len(ls) != 2 {
		t.Fatalf("wrapped into %d lines: %q", len(ls), ls)
	}
	for _, l := range ls {
		if ansi.StringWidth(l) > 12 {
			t.Errorf("line %q wider than 12", l)
		}
	}
	if !strings.Contains(ls[0], "~") {
		t.Errorf("truncated line lacks ascii tail: %q", ls[0])
	}
	if strings.Contains(ls[1], "\n") {
		t.Errorf("newline survived into a row")
	}
}

func TestListSelectedAndWindowAccessors(t *testing.T) {
	t.Parallel()

	m := NewList(asciiTheme(t), 20, 4)
	if _, ok := m.Selected(); ok {
		t.Fatal("Selected ok on empty list")
	}
	m.SetItems(items(10))
	m.SetCursor(99)
	it, ok := m.Selected()
	if !ok || it.Data != 9 {
		t.Fatalf("Selected after over-clamp: %+v ok=%v", it, ok)
	}
	m.SetCursor(-5)
	if it, _ := m.Selected(); it.Data != 0 {
		t.Fatalf("Selected after under-clamp: %+v", it)
	}
	top, count := m.Window()
	if top != 0 || count != 4 {
		t.Fatalf("window (%d,%d), want (0,4)", top, count)
	}
}

func TestListSetSizeReclamps(t *testing.T) {
	t.Parallel()

	m := NewList(asciiTheme(t), 20, 10)
	m.SetItems(items(8))
	m.SetCursor(7)
	m.SetSize(20, 3)
	checkListInvariants(t, m)
	if len(lines(m.View())) != 3 {
		t.Fatalf("view not resized: %q", m.View())
	}
}

// strip removes ANSI styling for glyph assertions.
func strip(s string) string { return ansi.Strip(s) }
