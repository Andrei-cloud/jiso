// hitmap_test.go pins the mouse hit-map machinery (Task 8.1, finding 9):
// resolve geometry (absolute terminal cells, last-added/topmost wins), the
// content-relative → absolute translation, and the synthetic messages a
// resolved hit produces — a key hit must arrive as EXACTLY the key press
// ch(...) types in these tests, so every existing key.Matches binding fires
// as if the key were typed.
package tui

import (
	"testing"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/geom"
)

func TestHitMapResolveTopmost(t *testing.T) {
	hm := hitMap{}
	// keyHit is the ctor; the zero-kind literal must mean the same action
	// (the brief's spelling), which the footer registration will use.
	hm.add(geom.Rect{X: 0, Y: 20, W: 40, H: 1}, keyHit("4")) // footer "4 server" (absolute)
	lit := hitAction{key: "4"}
	if lit != keyHit("4") {
		t.Fatalf("hitAction{key} literal != keyHit: %+v vs %+v", lit, keyHit("4"))
	}
	a, ok := hm.resolve(5, 20)
	if !ok || a.key != "4" {
		t.Fatalf("resolve footer cell = %+v,%v want key 4", a, ok)
	}
	if _, ok := hm.resolve(5, 5); ok {
		t.Fatal("non-hit cell must not resolve")
	}

	// A later-added rect that overlaps wins: the last-drawn overlay sits
	// topmost, so its hits must shadow the page body underneath.
	hm.add(geom.Rect{X: 4, Y: 19, W: 3, H: 3}, hitAction{key: "?"})
	if a, _ := hm.resolve(5, 20); a.key != "?" {
		t.Fatalf("overlapping hit = key %q, want the last-added (topmost) ?", a.key)
	}
	// Outside the overlay still resolves the footer underneath.
	if a, _ := hm.resolve(30, 20); a.key != "4" {
		t.Fatalf("outside overlay = key %q, want the footer 4 underneath", a.key)
	}
	// Exclusive right/bottom edges: a W×H rect covers [X, X+W) × [Y, Y+H).
	if _, ok := hm.resolve(40, 20); ok {
		t.Fatal("x == X+W must not resolve (exclusive right edge)")
	}
	if _, ok := hm.resolve(0, 21); ok {
		t.Fatal("y == Y+H must not resolve (exclusive bottom edge)")
	}
}

func TestHitMapAddAbsTranslatesContentOrigin(t *testing.T) {
	// The frame puts the page body at x=2 (side rule + space) and y=1
	// (top rule) at normal sizes; a page's section Rect is recorded
	// content-relative, so a hit added through addAbs must land on the
	// ABSOLUTE cell the mouse reports.
	hm := hitMap{}
	rel := geom.Rect{X: 0, Y: 0, W: 10, H: 3}
	hm.addAbs(2, 1, rel, hitAction{key: "j"})

	if _, ok := hm.resolve(0, 0); ok {
		t.Fatal("content-relative coords must NOT resolve: mouse X/Y are absolute")
	}
	a, ok := hm.resolve(2, 1)
	if !ok || a.key != "j" {
		t.Fatalf("absolute (2,1) = %+v,%v want key j", a, ok)
	}
	if _, ok := hm.resolve(12, 1); ok {
		t.Fatal("(2+10,1) is past the translated right edge")
	}
}

func TestContentOriginTracksFrame(t *testing.T) {
	// RootModel.contentOrigin is the offset 8.2–8.5 feed to addAbs; it must
	// track the frame's live shrink decisions, not a frozen constant.
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if x, y := m.contentOrigin(); x != 2 || y != 1 {
		t.Fatalf("contentOrigin at 80x24 = %d,%d, want 2,1 (side rule + top rule)", x, y)
	}
	// Height pressure drops the top rule first (frame.chromeParts), so the
	// content area moves up to y=0.
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 4})
	if x, y := m.contentOrigin(); x != 2 || y != 0 {
		t.Fatalf("contentOrigin at 80x4 = %d,%d, want 2,0 (top rule dropped)", x, y)
	}
}

func TestHitActionCmdKeyMatchesBindings(t *testing.T) {
	// The synthetic key must be indistinguishable from a typed one for
	// key.Matches: ch('4') is the harness spelling of a typed '4'.
	cmd := hitAction{key: "4"}.cmd()
	if cmd == nil {
		t.Fatal("key hit must return a Cmd")
	}
	msg := cmd()
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		t.Fatalf("key hit msg = %T, want tea.KeyPressMsg", msg)
	}
	if want := ch('4'); km != want {
		t.Fatalf("synthetic key = %#v, want %#v (the ch() construction)", km, want)
	}
	jump := key.NewBinding(key.WithKeys("4"))
	if !key.Matches(km, jump) {
		t.Fatal("synthetic key must satisfy key.Matches like a typed key")
	}
}

func TestHitActionCmdNamedKeys(t *testing.T) {
	// Named key spellings resolve to the special codes the terminal
	// delivers, so bindings written as "enter"/"esc"/"shift+tab" match a
	// synthetic press exactly (the String() side key.Matches compares).
	cases := []struct {
		key  string
		want tea.KeyPressMsg
		str  string // KeyPressMsg.String(), what key.Matches compares
	}{
		{"j", ch('j'), "j"},
		{"enter", special(tea.KeyEnter), "enter"},
		{"esc", special(tea.KeyEsc), "esc"},
		{"tab", special(tea.KeyTab), "tab"},
		{"backtab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, "shift+tab"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			cmd := keyHit(tc.key).cmd()
			if cmd == nil {
				t.Fatalf("keyHit(%q) must return a Cmd", tc.key)
			}
			got, ok := cmd().(tea.KeyPressMsg)
			if !ok {
				t.Fatalf("keyHit(%q) msg = %#v, want tea.KeyPressMsg", tc.key, cmd)
			}
			if got != tc.want {
				t.Fatalf("keyHit(%q) msg = %#v, want %#v", tc.key, got, tc.want)
			}
			if s := got.String(); s != tc.str {
				t.Fatalf("String() = %q, want %q (key.Matches compares this)", s, tc.str)
			}
			if !key.Matches(got, key.NewBinding(key.WithKeys(tc.str))) {
				t.Fatalf("synthetic %q must match a binding written %q", tc.key, tc.str)
			}
		})
	}
	// An unspellable key stays inert rather than injecting a wrong press.
	if cmd := keyHit("ctrl+alt+del").cmd(); cmd != nil {
		t.Fatal("unknown key spelling must yield no Cmd")
	}
}

func TestHitActionCmdNonKeyMsgs(t *testing.T) {
	cases := []struct {
		name string
		act  hitAction
		want any
	}{
		{"scroll", scrollHit("tx-list", -1), scrollMsg{region: "tx-list", delta: -1}},
		// Index 0 must be routable: the first row is a legal click.
		{"select", selectHit("tx-list", 0), selectMsg{region: "tx-list", index: 0}},
		{"focus", focusHit("split", 1), focusMsg{region: "split", index: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.act.cmd()
			if cmd == nil {
				t.Fatal("hit must return a Cmd")
			}
			if got := cmd(); got != tc.want {
				t.Fatalf("msg = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestInstallMouseContract(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	v := m.View()
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("View.MouseMode = %v, want MouseModeCellMotion", v.MouseMode)
	}
	if v.OnMouse == nil {
		t.Fatal("View.OnMouse must be installed")
	}
	// The shipped frame's map is empty (real hits arrive with 8.2–8.5):
	// every cell resolves to nothing.
	if cmd := v.OnMouse(tea.MouseClickMsg{X: 5, Y: 20, Button: tea.MouseLeft}); cmd != nil {
		t.Fatal("an unregistered cell must resolve to nothing")
	}

	// One registered key hit + a wheel region, installed on a fresh view
	// (real hits arrive with Tasks 8.2–8.5).
	hm := hitMap{}
	hm.add(geom.Rect{X: 2, Y: 20, W: 40, H: 1}, keyHit("4"))
	hm.add(geom.Rect{X: 2, Y: 2, W: 40, H: 10}, scrollHit("page", 0))
	var v2 tea.View
	m.installMouse(&v2, hm)

	// Left click on the footer hit replays the key.
	click := tea.MouseClickMsg{X: 5, Y: 20, Button: tea.MouseLeft}
	cmd := v2.OnMouse(click)
	if cmd == nil {
		t.Fatal("click on a registered hit must return a Cmd")
	}
	if got := cmd(); got != ch('4') {
		t.Fatalf("click msg = %#v, want %#v", got, ch('4'))
	}

	// Wheel down over the scroll region → scrollMsg (delta convention:
	// down = -1); wheel up → +1.
	down := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown}
	if got := v2.OnMouse(down)(); got != (scrollMsg{region: "page", delta: -1}) {
		t.Fatalf("wheel down = %#v, want scrollMsg{page -1}", got)
	}
	up := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp}
	if got := v2.OnMouse(up)(); got != (scrollMsg{region: "page", delta: 1}) {
		t.Fatalf("wheel up = %#v, want scrollMsg{page 1}", got)
	}

	// Release and motion must NOT replay (press+release would double-fire).
	if cmd := v2.OnMouse(tea.MouseReleaseMsg{X: 5, Y: 20, Button: tea.MouseLeft}); cmd != nil {
		t.Fatal("mouse release must not replay the click action")
	}
	// A click over dead space is silently ignored.
	if cmd := v2.OnMouse(tea.MouseClickMsg{X: 90, Y: 15, Button: tea.MouseLeft}); cmd != nil {
		t.Fatal("dead-space click must return nil")
	}
	// A wheel over a hit without a region (the footer key hit) is inert.
	if cmd := v2.OnMouse(tea.MouseWheelMsg{X: 5, Y: 20, Button: tea.MouseWheelDown}); cmd != nil {
		t.Fatal("wheel over a key hit (no region) must return nil")
	}
}

func TestMouseMsgsRouteAtRoot(t *testing.T) {
	// The three synthetic msgs are owned by the root router: Update must
	// consume them (the routing skeleton 8.2–8.5 flesh out) without
	// panicking and without forwarding to the page.
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Replace(recordingPage{id: "spy"})
	baseline := len(seenOf(m)) // Replace seeds the page with its size

	for _, msg := range []tea.Msg{
		scrollMsg{region: "page", delta: -1},
		selectMsg{region: "page", index: 0},
		focusMsg{region: "split", index: 1},
	} {
		model, cmd := m.Update(msg)
		if model != m {
			t.Fatalf("%T: model = %v, want the same root", msg, model)
		}
		if cmd != nil {
			t.Fatalf("%T: cmd = %v, want nil (stub)", msg, cmd)
		}
	}
	if seen := seenOf(m); len(seen) != baseline {
		t.Fatalf("mouse msgs reached the page (%d new msgs); the root must own them", len(seen)-baseline)
	}
}
