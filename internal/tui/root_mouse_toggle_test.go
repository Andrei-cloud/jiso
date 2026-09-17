// root_mouse_toggle_test.go pins the F9 mouse-mode toggle: bubbletea has
// no button-events-only mode, so F9 releases DECSET 1002 entirely. The
// next View either arms the mouse (CellMotion + OnMouse + populated hit
// map) or disarms it (None + nil OnMouse + provably empty hit map).
package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/geom"
	"jiso/internal/tui/pages"
)

// f9Key is the F9 function-key press as the terminal delivers it after
// ultraviolet decodes "\x1b[20~" (key_table.go: "20" → KeyF9).
func f9Key() tea.KeyPressMsg { return special(tea.KeyF9) }

// NewRootModel must arm the mouse: the zero value would silently disable it.
func TestMouseEnabledByDefault(t *testing.T) {
	m := NewRootModel(nil)
	if !m.mouseEnabled {
		t.Fatal("NewRootModel must start with the mouse enabled (round-8 parity)")
	}
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m.View()
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("boot View MouseMode = %v, want MouseModeCellMotion", v.MouseMode)
	}
	if v.OnMouse == nil {
		t.Error("boot View must carry an OnMouse handler while the mouse is enabled")
	}
}

// F9 flips mouseEnabled and the very next View reflects it in both
// directions; as a global key it works from any page without moving the stack.
func TestF9TogglesMouseMode(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	_, _ = m.Update(f9Key())
	if m.mouseEnabled {
		t.Fatal("F9 must disable the mouse")
	}
	if v := m.View(); v.MouseMode != tea.MouseModeNone || v.OnMouse != nil {
		t.Errorf("mouse-off View: MouseMode=%v OnMouse!=%v, want MouseModeNone and nil OnMouse",
			v.MouseMode, v.OnMouse != nil)
	}

	_, _ = m.Update(f9Key())
	if !m.mouseEnabled {
		t.Fatal("F9 again must re-enable the mouse")
	}
	if v := m.View(); v.MouseMode != tea.MouseModeCellMotion || v.OnMouse == nil {
		t.Errorf("mouse-on View: MouseMode=%v OnMouse!=%v, want MouseModeCellMotion and a handler",
			v.MouseMode, v.OnMouse != nil)
	}
}

// the hit map is gated by the same flag: off empties it, on repopulates it.
func TestF9OffEmptiesHitMap(t *testing.T) {
	m := serverAt(t, 80, 24, 30)
	_ = m.View()
	if len(m.buildHitMap()) == 0 {
		t.Fatal("precondition: §G with a log must register hits while the mouse is on")
	}

	_, _ = m.Update(f9Key())
	_ = m.View()
	if hm := m.buildHitMap(); len(hm) != 0 {
		t.Fatalf("mouse-off buildHitMap registered %d entries, want 0: %+v", len(hm), hm)
	}

	_, _ = m.Update(f9Key())
	_ = m.View()
	hm := m.buildHitMap()
	if len(hm) == 0 {
		t.Fatal("F9-on must repopulate the hit map (wheel/click restored)")
	}
	// the wheel-over-log hit works again (absolute coords)
	ox, oy := m.contentOrigin()
	rel := m.server.ScrollRegions()
	if len(rel) != 1 {
		t.Fatalf("§G must publish exactly the log region, got %#v", rel)
	}
	want := geom.Rect{X: rel[0].Rect.X + ox, Y: rel[0].Rect.Y + oy, W: rel[0].Rect.W, H: rel[0].Rect.H}
	act, ok := hm.resolve(want.X+want.W/2, want.Y+want.H/2)
	if !ok || act.kind != hitScroll || act.region != rel[0].ID {
		t.Fatalf("log-pane cell after re-enable = %+v,%v, want a scroll hit on %q",
			act, ok, rel[0].ID)
	}
}

// edit-mode claims run before global keys: while typing, F9 belongs to the
// field (no-op there) and must not toggle; esc leaves the field first.
func TestF9ClaimedByEditModeField(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(ch('2')) // §B transactions
	wantStack(t, m, "transactions")
	_, _ = m.Update(ch('/')) // open the live filter: edit mode

	kc, ok := m.Current().(pages.KeyboardClaimer)
	if !ok || !kc.ClaimsKeyboard() {
		t.Fatal("transactions filter must claim the keyboard while editing")
	}

	_, _ = m.Update(ch('q'))
	_, _ = m.Update(f9Key())
	if !m.mouseEnabled {
		t.Fatal("F9 toggled the mouse while an edit-mode field claimed the keyboard")
	}
	if got, _ := m.tx.Filter(); got != "q" {
		t.Errorf("filter = %q, want %q (F9 must no-op in the field, never edit text or toggle)", got, "q")
	}

	// Leaving the field returns the key to the global layer.
	_, _ = m.Update(special(tea.KeyEscape))
	if kc, ok := m.Current().(pages.KeyboardClaimer); ok && kc.ClaimsKeyboard() {
		t.Fatal("esc must leave the field before any global key works")
	}
	_, _ = m.Update(f9Key())
	if m.mouseEnabled {
		t.Fatal("F9 outside edit mode must toggle the mouse")
	}
}
