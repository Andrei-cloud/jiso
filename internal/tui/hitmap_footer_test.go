// hitmap_footer_test.go pins Task 8.4 (UAT round 8 finding 9): clicking a
// footer legend cell fires its action. The frame's footer packer publishes
// the ABSOLUTE rect of every PACKED (visible) entry through
// frame.FooterHits; buildHitMap registers each as a key hit, so a left
// click replays the dispatch key byte-identically to a typed press
// (installMouse + updateKey unchanged). Entries the width pressure DROPPED
// (the "…+N" tail) get no rect and stay inert, and while a root-owned
// modal owns the keyboard the synthesized key reaches the MODAL — the
// footer never navigates underneath it and never double-fires.
//
// The resolve/addAbs/cmd machinery is pinned in hitmap_test.go; the select
// legs in hitmap_select_test.go.
package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/pages"
)

// footerCellFor returns the ABSOLUTE footer cell frame.FooterHits publishes
// for the dispatch key on the root's current size and hint list — the same
// inputs buildHitMap consumes, so the cell a test clicks is the cell the
// map registers (or the test fails: no such rect).
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

// sendKey delivers a key through Update and returns the root back (the
// shape every tui test drives).
func sendKey(t *testing.T, m *RootModel, msg tea.KeyPressMsg) *RootModel {
	t.Helper()

	next, _ := m.Update(msg)
	rm, ok := next.(*RootModel)
	if !ok {
		t.Fatalf("Update returned %T, want *RootModel", next)
	}

	return rm
}

// TestClickFooterFiresAction is the Task 8.4 tracer (brief Step 1): on §B,
// a left click at the ABSOLUTE cell of the "4 server" footer entry replays
// the ch('4') press and the current page becomes the §G server page.
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
	// One click, one key: the release over the same cell fires nothing
	// (the fresh frame re-registers the entry; only the click replays).
	v2 := rm.View()
	if cmd := v2.OnMouse(tea.MouseReleaseMsg{X: click.X, Y: click.Y, Button: tea.MouseLeft}); cmd != nil {
		t.Fatal("the release over a footer hit must not double-fire")
	}
}

// TestClickDroppedFooterHintInert pins that only VISIBLE hints are
// clickable: at LevelNarrow the packer hides the non-primary page keys
// behind the "…+N" marker, and a dropped entry gets NO rect — neither
// frame.FooterHits nor the hit map carries its key, while the surviving
// primary entry still resolves.
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
	// The surviving primary trio still resolves on the same footer row,
	// proving the map is alive and only the DROPPED entry is inert.
	keep := footerCellFor(t, m, "q")
	if act, ok := hm.resolve(keep.X+keep.W/2, keep.Y); !ok || act.kind != hitKey || act.key != "q" {
		t.Fatalf("the visible \"q quit\" entry = %+v,%v, want its key hit", act, ok)
	}
	// The "…+N" marker tail (where the dropped entries used to live) is
	// dead space: a click right after the last packed entry fires nothing.
	dead := tea.MouseClickMsg{X: keep.X + keep.W + 1, Y: keep.Y, Button: tea.MouseLeft}
	if cmd := v.OnMouse(dead); cmd != nil {
		t.Fatalf("the overflow marker cell replayed %#v, want inert", cmd())
	}
}

// advertisesDispatch reports whether the root's full footer hint list (the
// packer's input, BEFORE width filtering) carries a hint whose dispatch
// spelling is key.
func advertisesDispatch(m *RootModel, key string) bool {
	for _, h := range m.footerHints() {
		d := h.Dispatch
		if d == "" {
			d = h.Key
		}
		if d == key {
			return true
		}
	}

	return false
}

// TestFooterClickReachesModal pins the modal-safety contract (brief item
// 4): a footer click synthesizes a KEY, so with a root-owned modal owning
// the keyboard the key lands in the MODAL (palette filter text) and never
// navigates the page behind it. No modalOpen() swallow: the key must
// reach the modal.
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
	if got := rm.Current().ID(); got != pages.TransactionsPageID {
		t.Fatalf("the palette leaked the jump key: page = %q, want §B behind the modal", got)
	}

	// §M owns the keyboard wholesale: the same click's key is swallowed
	// by the overlay, again without navigating underneath it.
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
