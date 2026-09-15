// root_keys.go is the routing contract at one seam: a page whose field
// is in EDIT mode (ClaimsKeyboard()==true) receives EVERY key — no
// global hotkey fires while the user is typing — and a page that is
// NOT typing still routes the global layer (?/digits/:/q work).
// UAT round 8 finding 6 (D2): the old SCR-513 "?"-on-empty carve-out
// let "?" escape a claiming field into the §M overlay; it is gone, and
// claims are scoped to edit mode so "?" stays reachable when the user
// is not typing.
package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/pages"
)

// TestEditingFieldSwallowsGlobalKeys: with the §B live filter open
// (edit mode), q / 4 / : / ? / c all type into the filter; no page
// jump, no palette, no help overlay and no quit confirmation may fire.
func TestEditingFieldSwallowsGlobalKeys(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(ch('2')) // §B transactions
	wantStack(t, m, "transactions")

	_, _ = m.Update(ch('/')) // open the live filter: edit mode

	kc, ok := m.Current().(pages.KeyboardClaimer)
	if !ok || !kc.ClaimsKeyboard() {
		t.Fatal("transactions filter must claim the keyboard while editing")
	}

	for _, k := range "q4:?c" {
		_, cmd := m.Update(ch(k))
		if isQuit(t, cmd) {
			t.Fatalf("%q quit while the filter claimed the keyboard", k)
		}
	}
	wantStack(t, m, "transactions")
	if m.pal != nil {
		t.Error(": opened the palette while editing")
	}
	if m.help != nil {
		t.Error("? opened the §M overlay while editing")
	}
	if got, _ := m.tx.Filter(); got != "q4:?c" {
		t.Errorf("filter = %q, want %q (every key must reach the field)", got, "q4:?c")
	}
}

// TestNavigateModeStillRoutesGlobalKeys: UAT round 8 (D2) scoped the §J
// capture claim to edit mode (a typed path in progress). On the fresh
// step the user is not typing, so the global layer must work: "?"
// opens the §M overlay and "q" arms the quit confirm — the old
// whole-step claim swallowed both.
func TestNavigateModeStillRoutesGlobalKeys(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	r.gotoAnalyze()
	r.mustStep(t, pages.StepCapture)

	if kc, ok := r.m.Current().(pages.KeyboardClaimer); ok && kc.ClaimsKeyboard() {
		t.Fatal("a fresh capture step is navigate mode and must not claim the keyboard")
	}
	_, _ = r.m.Update(ch('?'))
	if r.m.help == nil {
		t.Fatal("? must open the §M overlay when no field is being typed into")
	}
	_, _ = r.m.Update(special(tea.KeyEscape))
	r.m.help = nil

	_, _ = r.m.Update(ch('q'))
	if !r.m.confirmQuit {
		t.Fatal("q must arm the quit confirm in navigate mode, not type into a fresh draft")
	}
}

// TestEditBufferClaimsEveryKey: the [o] output-path editor is an open
// edit buffer, so the page must claim every key while it is open —
// q, 4, : and ? all belong to the buffer. Before UAT round 8 the run
// step edited text without claiming, and the keys escaped into the
// global layer (q armed the quit, : opened the palette, ? the overlay).
func TestEditBufferClaimsEveryKey(t *testing.T) {
	r := newAnalyzeTestRoot(t, fakeAnalyzeFixture())
	r.walkToRun(t)
	r.closePicker()

	r.pump(ch('o')) // open the output-path editor: edit mode
	kc, ok := r.m.Current().(pages.KeyboardClaimer)
	if !ok || !kc.ClaimsKeyboard() {
		t.Fatal("the open [o] editor must claim the keyboard")
	}
	for _, k := range "q4:?" {
		r.pump(ch(k))
	}
	if r.m.help != nil || r.m.pal != nil || r.m.confirmQuit {
		t.Fatalf("a global key fired while the editor claimed the keyboard: help=%v pal=%v quit=%v",
			r.m.help != nil, r.m.pal != nil, r.m.confirmQuit)
	}
	if body := ansi.Strip(r.view()); !strings.Contains(body, "q4:?") {
		t.Fatalf("the keys must reach the editor buffer:\n%s", body)
	}
}
