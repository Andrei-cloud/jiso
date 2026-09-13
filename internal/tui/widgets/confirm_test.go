package widgets

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/theme"
)

// decide drives one key through the dialog and returns the message the
// emitted command produced (nil if no command).
func decide(t *testing.T, m *ConfirmDialog, msg tea.Msg) tea.Msg {
	t.Helper()
	_, cmd := m.Update(msg)
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestConfirmOpenEmitsWaitResult(t *testing.T) {
	t.Parallel()

	m := NewConfirmDialog(asciiTheme(t), "Stop all workers?")
	cmd := m.Open()
	if cmd == nil {
		t.Fatal("Open returned nil command")
	}
	msg, ok := cmd().(WaitResultMsg)
	if !ok || msg.Question != "Stop all workers?" {
		t.Fatalf("Open cmd produced %T %+v", msg, msg)
	}
	if !m.Pending() {
		t.Fatal("dialog not pending after Open")
	}
}

func TestConfirmOnlyExplicitYesConfirms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  tea.Msg
		want tea.Msg
	}{
		{"y", ch('y'), ConfirmedMsg{}},
		{"Y", ch('Y'), ConfirmedMsg{}},
		{"n", ch('n'), CancelledMsg{}},
		{"N", ch('N'), CancelledMsg{}},
		{"esc", special(tea.KeyEscape), CancelledMsg{}},
		{"enter-defaults-no", special(tea.KeyEnter), CancelledMsg{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewConfirmDialog(asciiTheme(t), "Overwrite output file?")
			m.Open()
			got := decide(t, m, tc.msg)
			if got == nil {
				t.Fatalf("no result for %q", tc.name)
			}
			if strings.Contains(fmtT(got), fmtT(tc.want)) == false {
				t.Fatalf("got %T, want %T", got, tc.want)
			}
			if m.Pending() {
				t.Fatal("still pending after decision")
			}
		})
	}
}

func TestConfirmNonCommittalKeysDoNothing(t *testing.T) {
	t.Parallel()

	m := NewConfirmDialog(asciiTheme(t), "Quit with active workers?")
	m.Open()
	for _, k := range []rune{'x', ' ', 'q', '!', '1'} {
		if got := decide(t, m, ch(k)); got != nil {
			t.Fatalf("key %q produced result %T", k, got)
		}
	}
	if !m.Pending() {
		t.Fatal("dialog closed without a decision")
	}
}

func TestConfirmDecisionIsTerminal(t *testing.T) {
	t.Parallel()

	m := NewConfirmDialog(asciiTheme(t), "Q?")
	m.Open()
	decide(t, m, ch('y'))
	if got := decide(t, m, ch('n')); got != nil {
		t.Fatalf("second decision produced %T", got)
	}
	if got := decide(t, m, special(tea.KeyEscape)); got != nil {
		t.Fatalf("esc after decision produced %T", got)
	}
	if v := m.View(); v != "" {
		t.Fatalf("closed dialog still renders %q", v)
	}
}

func TestConfirmNonKeyMsgsIgnored(t *testing.T) {
	t.Parallel()

	m := NewConfirmDialog(asciiTheme(t), "Q?")
	m.Open()
	if _, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24}); cmd != nil {
		t.Fatal("window size moved the dialog")
	}
	if !m.Pending() {
		t.Fatal("non-key msg closed the dialog")
	}
}

func TestConfirmViewHasQuestionAndHints(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		th   func(*testing.T) *theme.Theme
		sep  string
	}{
		{"tc", tcTheme, "·"},
		{"ascii", asciiTheme, "|"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewConfirmDialog(tc.th(t), "Drop the session database?")
			m.Open()
			ls := lines(m.View())
			if len(ls) != 2 {
				t.Fatalf("view is %d lines, want 2:\n%s", len(ls), m.View())
			}
			if ls[0] != "Drop the session database?" && strip(ls[0]) != "Drop the session database?" {
				t.Errorf("question line %q", strip(ls[0]))
			}
			hint := strip(ls[1])
			for _, want := range []string{"y confirm", "n", "esc cancel", "default: no", tc.sep} {
				if !strings.Contains(hint, want) {
					t.Errorf("hint %q lacks %q", hint, want)
				}
			}
		})
	}
}

func TestConfirmAsciiViewIsPlain(t *testing.T) {
	t.Parallel()

	m := NewConfirmDialog(asciiTheme(t), "Q?")
	m.Open()
	out := m.View()
	if strings.ContainsAny(out, "\x1b\u009b") {
		t.Errorf("ascii dialog contains escapes: %q", out)
	}
	for _, r := range out {
		if r > 127 {
			t.Fatalf("ascii dialog contains non-ASCII rune %q", r)
		}
	}
}

func TestConfirmQuestionAccessor(t *testing.T) {
	t.Parallel()

	m := NewConfirmDialog(asciiTheme(t), "Stop-all?")
	if m.Question() != "Stop-all?" {
		t.Fatalf("Question() = %q", m.Question())
	}
}

func fmtT(v any) string {
	if v == nil {
		return "<nil>"
	}
	s := reflect.TypeOf(v).String()

	return s[strings.LastIndex(s, ".")+1:]
}
