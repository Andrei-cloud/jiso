package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/palette"
)

// root_palette_test.go covers the ":" palette state machine on the root:
// open, query capture, backspace, escape, action submit and Ctrl+C.

func TestPaletteStateMachine(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)

	_, _ = m.Update(ch(':'))
	if m.pal == nil {
		t.Fatal("':' must open palette mode")
	}

	// Typing "send" (a transactions keyword) must be captured by the
	// palette, not jump pages.
	for _, c := range "send" {
		_, _ = m.Update(ch(c))
	}

	wantStack(t, m, "dashboard")

	if m.pal.Query() != "send" {
		t.Fatalf("palette query: got %q, want %q", m.pal.Query(), "send")
	}

	_, _ = m.Update(special(tea.KeyBackspace))
	if m.pal.Query() != "sen" {
		t.Fatalf("backspace: got %q, want %q", m.pal.Query(), "sen")
	}

	_, _ = m.Update(special(tea.KeyEscape))
	if m.pal != nil {
		t.Fatal("esc must close palette mode")
	}

	// Reopen and submit a match: "x" is a fuzzy hit on the "tx" keyword
	// of goto.transactions (registration order wins the tie against
	// goto.ctf's "export"), so enter runs that action and closes.
	_, _ = m.Update(ch(':'))
	_, _ = m.Update(ch('x'))
	_, cmd := m.Update(special(tea.KeyEnter))
	if m.pal != nil {
		t.Fatal("enter on a match must close palette mode")
	}
	if isQuit(t, cmd) {
		t.Fatal("palette enter must not quit")
	}
	if cmd == nil {
		t.Fatal("enter on a match must return a cmd")
	}
	if pg, ok := cmd().(palette.GoToPageMsg); !ok || pg.ID != "transactions" {
		t.Fatalf("enter 'x': got %#v, want GoToPageMsg{transactions}", cmd())
	}
}

func TestPaletteBackspaceOnEmptyQuery(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(ch(':'))

	_, _ = m.Update(special(tea.KeyBackspace))
	_, _ = m.Update(special(tea.KeyBackspace))

	if m.pal == nil || m.pal.Query() != "" {
		t.Fatalf("unexpected state: pal=%v query=%q", m.pal, m.pal.Query())
	}
}

func TestPaletteCtrlCStillQuits(t *testing.T) {
	t.Parallel()

	m := NewRootModel(nil)
	_, _ = m.Update(ch(':'))

	_, cmd := m.Update(mod('c', tea.ModCtrl))
	if !isQuit(t, cmd) {
		t.Fatal("ctrl+c must quit even in palette mode")
	}
}
