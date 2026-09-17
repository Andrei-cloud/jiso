// hitmap_footer_test.go pins footer legend clicks: a click on a packed
// (visible) entry replays its dispatch key byte-identically; width-dropped
// entries get no rect and stay inert; with a modal owning the keyboard the
// synthesized key reaches the modal, never navigating underneath it.
package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/pages"
)

// footerCellFor returns the absolute footer cell frame.FooterHits publishes
// for a dispatch key — the same inputs buildHitMap consumes.
func footerCellFor(t *testing.T, m *RootModel, dispatch string) frame.FooterHit {
	t.Helper()

	for _, hh := range frame.FooterHits(m.themeOrNil(), m.footerHints(), m.width, m.height) {
		if hh.Dispatch == dispatch {
			return hh
		}
	}
	t.Fatalf("the footer publishes no cell for dispatch %q: %#v", dispatch,
		frame.FooterHits(m.themeOrNil(), m.footerHints(), m.width, m.height))

	return frame.FooterHit{}
}

// sendKey delivers a key through Update and returns the root back.
func sendKey(t *testing.T, m *RootModel, msg tea.KeyPressMsg) *RootModel {
	t.Helper()

	next, _ := m.Update(msg)
	rm, ok := next.(*RootModel)
	if !ok {
		t.Fatalf("Update returned %T, want *RootModel", next)
	}

	return rm
}

func TestClickFooterFiresAction(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	sendKey(t, m, ch('2'))
	if got := m.Current().ID(); got != pages.TransactionsPageID {
		t.Fatalf("precondition: page = %q, want §B", got)
	}
	v := m.View() // renders §B and rebuilds the hit map with the footer leg

	hit := footerCellFor(t, m, "4")
	// The cell sits on the footer row the frame actually draws (y = h-2).
	if _, fy, ok := frame.FooterOrigin(120, 32); !ok || hit.Y != fy {
		t.Fatalf("footer hit y = %d, want the drawn footer row %d", hit.Y, fy)
	}
	click := tea.MouseClickMsg{X: hit.X + hit.W/2, Y: hit.Y, Button: tea.MouseLeft}

	cmd := v.OnMouse(click)
	if cmd == nil {
		t.Fatal("a click on the visible \"4 server\" footer entry must replay its key, got nil")
	}
	pressed, ok := cmd().(tea.KeyPressMsg)
	if !ok || pressed != ch('4') {
		t.Fatalf("footer click = %#v, want the ch('4') replay", cmd())
	}
	rm := sendKey(t, m, pressed)
	if got := rm.Current().ID(); got != pages.ServerPageID {
		t.Fatalf("after the footer click the page = %q, want §G server", got)
	}
	// one click one key: the release over the same cell fires nothing
	v2 := rm.View()
	if cmd := v2.OnMouse(tea.MouseReleaseMsg{X: click.X, Y: click.Y, Button: tea.MouseLeft}); cmd != nil {
		t.Fatal("the release over a footer hit must not double-fire")
	}
}

// only visible hints are clickable: a width-dropped entry has no rect in
// frame.FooterHits or the hit map, while the surviving primary still resolves.
func TestClickDroppedFooterHintInert(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2')) // §B advertises "o sort" among its non-primary keys

	// Narrow frame: the level filter drops every non-primary hint.
	_, _ = m.Update(tea.WindowSizeMsg{Width: frame.NarrowWidth - 1, Height: 32})
	_, _ = m.Update(ch('2'))
	v := m.View()

	if !advertisesDispatch(m, "o") {
		t.Fatal("precondition: §B must still advertise the \"o sort\" hint at this width")
	}
	hits := frame.FooterHits(m.themeOrNil(), m.footerHints(), m.width, m.height)
	for _, hh := range hits {
		if hh.Dispatch == "o" {
			t.Fatalf("the dropped \"o sort\" entry got a rect: %#v (only visible hints are clickable)", hh)
		}
	}
	hm := m.buildHitMap()
	for _, e := range hm {
		if e.act.kind == hitKey && e.act.key == "o" {
			t.Fatalf("the hit map registered a key hit for the dropped entry: %+v", e)
		}
	}
	// the surviving primary still resolves: the map is alive, only the dropped entry is inert
	keep := footerCellFor(t, m, "q")
	if act, ok := hm.resolve(keep.X+keep.W/2, keep.Y); !ok || act.kind != hitKey || act.key != "q" {
		t.Fatalf("the visible \"q quit\" entry = %+v,%v, want its key hit", act, ok)
	}
	// the "…+N" marker tail is dead space: a click right after the last packed entry fires nothing
	dead := tea.MouseClickMsg{X: keep.X + keep.W + 1, Y: keep.Y, Button: tea.MouseLeft}
	if cmd := v.OnMouse(dead); cmd != nil {
		t.Fatalf("the overflow marker cell replayed %#v, want inert", cmd())
	}
}

// advertisesDispatch reports whether the root's full hint list (before width filtering) carries a dispatch key.
func advertisesDispatch(m *RootModel, key string) bool {
	for _, h := range m.footerHints() {
		if h.Key == key {
			return true
		}
	}

	return false
}

// a footer click synthesizes a key: with a modal owning the keyboard the
// key lands in the modal and never navigates the page behind it.
func TestFooterClickReachesModal(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('2'))
	_, _ = m.Update(ch(':'))
	if m.pal == nil {
		t.Fatal("precondition: : must open the command palette")
	}
	v := m.View()

	hit := footerCellFor(t, m, "4")
	cmd := v.OnMouse(tea.MouseClickMsg{X: hit.X + hit.W/2, Y: hit.Y, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("the footer key must reach the modal chain, not be swallowed")
	}
	pressed, ok := cmd().(tea.KeyPressMsg)
	if !ok {
		t.Fatalf("footer click = %#v, want a synthetic key press", cmd())
	}
	rm := sendKey(t, m, pressed)
	if rm.pal == nil {
		t.Fatal("the palette must still own the screen after the synthesized key")
	}
	if got := rm.pal.Query(); got != "4" {
		t.Fatalf("the palette did not receive the synthesized key: Query() = %q, want %q", got, "4")
	}
	if got := rm.Current().ID(); got != pages.TransactionsPageID {
		t.Fatalf("the palette leaked the jump key: page = %q, want §B behind the modal", got)
	}

	// §M swallows the same key, again without navigating underneath
	_, _ = rm.Update(special(tea.KeyEsc))
	_, _ = rm.Update(ch('?'))
	if rm.help == nil {
		t.Fatal("precondition: ? must open the §M overlay")
	}
	v2 := rm.View()
	cmd2 := v2.OnMouse(tea.MouseClickMsg{X: hit.X + hit.W/2, Y: hit.Y, Button: tea.MouseLeft})
	if cmd2 == nil {
		t.Fatal("the footer key must reach the §M overlay chain, not be swallowed")
	}
	pressed2, ok := cmd2().(tea.KeyPressMsg)
	if !ok {
		t.Fatalf("footer click = %#v, want a synthetic key press", cmd2())
	}
	rm2 := sendKey(t, rm, pressed2)
	if rm2.help == nil {
		t.Fatal("§M must stay open on an unbound key")
	}
	if got := rm2.Current().ID(); got != pages.TransactionsPageID {
		t.Fatalf("§M leaked the jump key: page = %q, want §B behind the overlay", got)
	}
}
