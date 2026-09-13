package palette

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/theme"
)

// ch builds a printable key press like the terminal delivers it.
func ch(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c, Text: string(c)} }

// special builds a named key press (tab, esc, enter, arrows, backspace).
func special(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func newTestModel(t *testing.T) *Model {
	t.Helper()

	return New(theme.NewWith(colorprofile.ASCII, true), SeedMatcher(), 40, 12)
}

func typeQuery(t *testing.T, m *Model, s string) {
	t.Helper()

	for _, r := range s {
		m, _ = m.Update(ch(r))
	}
}

func TestPaletteOpensWithAllActions(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)

	if m.list.Len() != len(Seed().Actions()) {
		t.Fatalf("open palette: got %d rows, want all %d", m.list.Len(), len(Seed().Actions()))
	}
	if m.query != "" || m.closed {
		t.Fatalf("fresh model: query=%q closed=%v", m.query, m.closed)
	}
}

func TestPaletteLiveFilterNarrows(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	all := m.list.Len()

	m, _ = m.Update(ch('s'))
	afterS := m.list.Len()

	m, _ = m.Update(ch('e'))
	afterSe := m.list.Len()

	if all <= afterS || afterS <= afterSe {
		t.Fatalf("narrowing: all=%d s=%d se=%d (want strictly shrinking)", all, afterS, afterSe)
	}
	sel, _ := m.list.Selected()
	if act, _ := sel.Data.(Action); act.ID != "send-wizard" {
		t.Fatalf("after 'se' top hit: got %v, want send-wizard (keyword \"send\" prefix, registration-order tie-break)", sel.Label)
	}
}

func TestPaletteEnterExecutesSend(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	typeQuery(t, m, "send")

	_, cmd := m.Update(special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter on a match must return a cmd")
	}
	msg := cmd()
	if _, ok := msg.(OpenSendWizardMsg); !ok {
		t.Fatalf("enter 'send': got %#v, want OpenSendWizardMsg", msg)
	}
}

func TestPaletteEnterPassesArgs(t *testing.T) {
	t.Parallel()

	var got []string
	r := NewRegistry()
	r.Register(Action{
		ID:    "rec",
		Title: "record",
		Run: func(args []string) tea.Msg {
			got = args

			return GoToPageMsg{ID: "recorded"}
		},
	})
	m := New(theme.NewWith(colorprofile.ASCII, true), NewMatcher(r), 40, 12)

	typeQuery(t, m, "rec --flag value")
	if m.list.Len() != 1 {
		t.Fatalf("args must not break matching: %d rows", m.list.Len())
	}

	_, cmd := m.Update(special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter must submit")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("cmd must yield the action Msg")
	}
	want := []string{"--flag", "value"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("args: got %v, want %v", got, want)
	}
}

func TestPaletteZeroMatchEnterIsNoop(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	typeQuery(t, m, "qqqqzzz")

	if m.list.Len() != 0 {
		t.Fatalf("expected zero matches, got %d", m.list.Len())
	}
	_, cmd := m.Update(special(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("enter with zero matches must not run anything")
	}
	if m.closed {
		t.Fatal("enter with zero matches must not close the palette")
	}
}

func TestPaletteEscCloses(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	typeQuery(t, m, "se")

	_, cmd := m.Update(special(tea.KeyEscape))
	if !m.closed {
		t.Fatal("esc must close")
	}
	if cmd != nil {
		t.Fatal("esc must not run an action")
	}
}

func TestPaletteNavMovesCursorNotQuery(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)

	for _, k := range []rune{'j', 'j'} {
		m, _ = m.Update(ch(k)) // vim aliases navigate
	}
	if m.query != "" {
		t.Fatalf("j must not append to the query, got %q", m.query)
	}
	if m.Cursor() != 2 {
		t.Fatalf("cursor: got %d, want 2", m.Cursor())
	}

	m, _ = m.Update(special(tea.KeyUp))
	if m.Cursor() != 1 {
		t.Fatalf("cursor after up: got %d, want 1", m.Cursor())
	}
	m, _ = m.Update(ch('k'))
	if m.Cursor() != 0 {
		t.Fatalf("cursor after k: got %d, want 0", m.Cursor())
	}
}

func TestPaletteSwallowsDigitKeys(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)

	// Digit keys are text in the palette, never page jumps: no cmd, no
	// close, and the query captures them (root test asserts the stack).
	for _, r := range "12" {
		_, cmd := m.Update(ch(r))
		if cmd != nil {
			t.Fatalf("digit %c must not produce a cmd", r)
		}
	}
	if m.query != "12" || m.closed {
		t.Fatalf("digits: query=%q closed=%v", m.query, m.closed)
	}
}

func TestPaletteUnicodeInputSafety(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	typeQuery(t, m, "日本")

	if m.query != "日本" {
		t.Fatalf("unicode query: got %q", m.query)
	}
	m, _ = m.Update(special(tea.KeyBackspace))
	if m.query != "日" {
		t.Fatalf("backspace must drop one rune, got %q", m.query)
	}
	m, _ = m.Update(special(tea.KeyBackspace))
	m, _ = m.Update(special(tea.KeyBackspace))
	if m.query != "" || m.list.Len() != len(Seed().Actions()) {
		t.Fatalf("emptying unicode query: query=%q rows=%d", m.query, m.list.Len())
	}
}

func TestPaletteBackspaceRefilters(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	typeQuery(t, m, "se")
	narrow := m.list.Len()

	m, _ = m.Update(special(tea.KeyBackspace))
	if m.list.Len() <= narrow {
		t.Fatalf("backspace must widen: %d -> %d", narrow, m.list.Len())
	}
}

func TestSplitCommand(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line string
		cmd  string
		args string // comma-joined, "" means nil
	}{
		{"", "", ""},
		{"send", "send", ""},
		{"send --flag value", "send", "--flag,value"},
		{`send "a b"`, "send", "a b"},
		{`send "unbalanced`, `send "unbalanced`, ""}, // lex error: raw cmd, no args
		{"  spaced  out  ", "spaced", "out"},
	}
	for _, c := range cases {
		cmd, args := splitCommand(c.line)
		if cmd != c.cmd {
			t.Errorf("splitCommand(%q) cmd = %q, want %q", c.line, cmd, c.cmd)
		}
		if strings.Join(args, ",") != c.args {
			t.Errorf("splitCommand(%q) args = %v, want %q", c.line, args, c.args)
		}
	}
}

func TestPaletteSetSizeClamps(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	m.SetSize(0, 0) // must ignore degenerate sizes
	if m.width < 2 || m.height < 2 {
		t.Fatalf("size: got %dx%d", m.width, m.height)
	}
	m.SetSize(60, 40)
	if m.width != 60 || m.height != 40 {
		t.Fatalf("size: got %dx%d, want 60x40", m.width, m.height)
	}
	if !strings.Contains(m.View(), ":") {
		t.Fatal("view must contain the input line")
	}
}
