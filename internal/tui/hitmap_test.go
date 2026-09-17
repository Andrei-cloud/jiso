// hitmap_test.go pins the mouse hit-map machinery: resolve geometry
// (absolute cells, last-added wins), the content-relative → absolute
// translation, and the synthetic messages a resolved hit emits.
package tui

import (
	"fmt"
	"testing"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/pages"
)

// seedServerLogLines fills the §G log ring with n numbered lines and syncs the page.
func seedServerLogLines(m *RootModel, n int) {
	for i := 1; i <= n; i++ {
		m.serverLog = append(m.serverLog, fmt.Sprintf("log line %02d", i))
	}
	m.syncServer()
}

// serverAt opens §G on a fresh root at size (w,h) with an n-line log.
func serverAt(t *testing.T, w, h, lines int) *RootModel {
	t.Helper()

	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	_, _ = m.Update(ch('4'))
	seedServerLogLines(m, lines)

	return m
}

// the §G log pane publishes its drawn rect into the hit map in absolute
// coords; the wheel over it resolves the scroll region, dead space does not.
func TestBuildHitMapServerLogRegion(t *testing.T) {
	m := serverAt(t, 80, 24, 30)
	_ = m.View() // renders §G (recording the pane) and rebuilds the map

	hm := m.buildHitMap()

	ox, oy := m.contentOrigin()
	rel := m.server.ScrollRegions()
	if len(rel) != 1 {
		t.Fatalf("§G must publish exactly the log region, got %#v", rel)
	}
	// the entry is the published rect translated by the content origin
	want := geom.Rect{X: rel[0].Rect.X + ox, Y: rel[0].Rect.Y + oy, W: rel[0].Rect.W, H: rel[0].Rect.H}
	act, ok := hm.resolve(want.X+want.W/2, want.Y+want.H/2)
	if !ok || act.kind != hitScroll || act.region != pages.RegionServerLog {
		t.Fatalf("log-pane cell = %+v,%v, want a scroll hit on %q", act, ok, pages.RegionServerLog)
	}
	// Above the pane (header/stats box): dead space, no phantom scroll.
	if _, ok := hm.resolve(ox+5, oy); ok {
		t.Error("a cell above the log pane must not resolve a scroll region")
	}

	// no log lines: no pane, no page geometry (footer key hits aside)
	bare := serverAt(t, 80, 24, 0)
	_ = bare.View()
	for _, e := range bare.buildHitMap() {
		if e.act.kind != hitKey {
			t.Fatalf("§G without log lines registered a non-footer hit: %+v", e)
		}
	}
}

// the sub-MinWidth frame registers no hits: pages still record rects, but
// registering them would resolve phantom hits over un-drawn ink.
func TestBuildHitMapBelowMinWidthInert(t *testing.T) {
	m := serverAt(t, frame.MinWidth-1, 24, 30)
	_ = m.View()

	if hm := m.buildHitMap(); len(hm) != 0 {
		t.Fatalf("sub-MinWidth frame registered %d hits, want 0 (phantom-hit policy)", len(hm))
	}
	// the pages did record geometry: the skip is policy, not layout accident
	if len(m.server.ScrollRegions()) == 0 {
		t.Fatal("precondition: the page should still record its panes below MinWidth")
	}
}

// the region resolves to the active page's scrollable; the content-direction
// delta passes through with no negation.
func TestScrollMsgDispatchServerLog(t *testing.T) {
	m := serverAt(t, 80, 24, 30)

	_, _ = m.Update(scrollMsg{region: pages.RegionServerLog, delta: -3})
	if got := m.server.LogScroll(); got != 3 {
		t.Fatalf("delta -3 (wheel up) = logScroll %d, want 3 (content-direction, no negation)", got)
	}
	_, _ = m.Update(scrollMsg{region: pages.RegionServerLog, delta: 1})
	if got := m.server.LogScroll(); got != 2 {
		t.Fatalf("delta +1 (wheel down) = logScroll %d, want 2", got)
	}
	// An unknown region stays inert (no panic, no wrong pane moved).
	_, _ = m.Update(scrollMsg{region: "nope:region", delta: 1})
	if got := m.server.LogScroll(); got != 2 {
		t.Fatalf("unknown region changed logScroll to %d", got)
	}
}

// with §M open its region dispatches to the overlay's own offset, never
// to the page underneath.
func TestScrollMsgDispatchHelpOverlay(t *testing.T) {
	m := NewRootModel(nil)
	// height 6 leaves a 2-line canvas: the keymap can't fit, so the box is genuinely scrollable
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 6})
	_, _ = m.Update(ch('?'))
	if m.help == nil {
		t.Fatal("? must open the §M overlay")
	}
	if m.help.maxScroll() <= 0 {
		t.Fatal("the overlay must get a real pane height on open so ScrollBy is not a no-op")
	}

	_, _ = m.Update(scrollMsg{region: regionHelp, delta: 1})
	if m.help.scrollOff != 1 {
		t.Fatalf("wheel down = scrollOff %d, want 1", m.help.scrollOff)
	}
	_, _ = m.Update(scrollMsg{region: regionHelp, delta: -1})
	if m.help.scrollOff != 0 {
		t.Fatalf("wheel up = scrollOff %d, want 0", m.help.scrollOff)
	}
	// A straggler after the overlay closed must not panic and must not
	// touch the page: close §M the way the keyboard does (Esc) and re-send.
	_, _ = m.Update(special(tea.KeyEsc))
	if m.help != nil {
		t.Fatal("esc must close the overlay")
	}
	if _, cmd := m.Update(scrollMsg{region: regionHelp, delta: 1}); cmd != nil {
		t.Fatal("a straggler help scrollMsg must be inert, not replayed")
	}
}

// only the left button replays key hits and only the vertical wheel
// scrolls; everything else stays inert.
func TestInstallMouseIgnoresOtherButtons(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	hm := hitMap{}
	hm.add(geom.Rect{X: 2, Y: 2, W: 20, H: 10}, keyHit("4"))
	hm.add(geom.Rect{X: 40, Y: 2, W: 20, H: 10}, scrollHit("page"))
	var v tea.View
	m.installMouse(&v, hm)

	// Middle/right clicks over a key hit replay nothing.
	if cmd := v.OnMouse(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseMiddle}); cmd != nil {
		t.Fatal("middle-button click must stay inert")
	}
	if cmd := v.OnMouse(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseRight}); cmd != nil {
		t.Fatal("right-button click must stay inert")
	}
	// Left still replays.
	if got := v.OnMouse(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})(); got != ch('4') {
		t.Fatalf("left click = %#v, want the ch('4') replay", got)
	}
	// Horizontal wheel steps over a scroll region scroll nothing.
	if cmd := v.OnMouse(tea.MouseWheelMsg{X: 45, Y: 5, Button: tea.MouseWheelLeft}); cmd != nil {
		t.Fatal("wheel-left must not emit a vertical scrollMsg")
	}
	if cmd := v.OnMouse(tea.MouseWheelMsg{X: 45, Y: 5, Button: tea.MouseWheelRight}); cmd != nil {
		t.Fatal("wheel-right must not emit a vertical scrollMsg")
	}
	// a left click on a scroll region is inert too: only the wheel acts
	if cmd := v.OnMouse(tea.MouseClickMsg{X: 45, Y: 5, Button: tea.MouseLeft}); cmd != nil {
		t.Fatal("a click over a scroll region must stay inert")
	}
	// Vertical wheel still routes.
	if got := v.OnMouse(tea.MouseWheelMsg{X: 45, Y: 5, Button: tea.MouseWheelDown})(); got != (scrollMsg{region: "page", delta: 1}) {
		t.Fatalf("wheel down = %#v, want scrollMsg{page +1}", got)
	}
}

func TestHitMapResolveTopmost(t *testing.T) {
	hm := hitMap{}
	// the zero-kind literal must mean the same action as the keyHit ctor
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

	// a later-added rect wins: the last-drawn overlay shadows the page underneath
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
	// a content-relative rect added via addAbs must resolve at the absolute cell
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
	// must track the frame's live shrink decisions, not a frozen constant
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if x, y := m.contentOrigin(); x != 2 || y != 1 {
		t.Fatalf("contentOrigin at 80x24 = %d,%d, want 2,1 (side rule + top rule)", x, y)
	}
	// height pressure drops the top rule first: content moves up to y=0
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
	// named spellings resolve to the codes the terminal delivers, so
	// bindings written "enter"/"esc"/"shift+tab" match a synthetic press
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
	// a click on a scroll region is inert: only the wheel scrolls
	// (it used to emit a no-op scrollMsg{delta: 0})
	if cmd := scrollHit("tx-list").cmd(); cmd != nil {
		t.Fatal("a click on a scroll region must be inert, not a scrollMsg")
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
	// the shipped frame's map is empty: every cell resolves to nothing
	if cmd := v.OnMouse(tea.MouseClickMsg{X: 5, Y: 20, Button: tea.MouseLeft}); cmd != nil {
		t.Fatal("an unregistered cell must resolve to nothing")
	}

	// one registered key hit + a wheel region, installed on a fresh view
	hm := hitMap{}
	hm.add(geom.Rect{X: 2, Y: 20, W: 40, H: 1}, keyHit("4"))
	hm.add(geom.Rect{X: 2, Y: 2, W: 40, H: 10}, scrollHit("page"))
	var v2 tea.View
	m.installMouse(&v2, hm)

	click := tea.MouseClickMsg{X: 5, Y: 20, Button: tea.MouseLeft}
	cmd := v2.OnMouse(click)
	if cmd == nil {
		t.Fatal("click on a registered hit must return a Cmd")
	}
	if got := cmd(); got != ch('4') {
		t.Fatalf("click msg = %#v, want %#v", got, ch('4'))
	}

	// a left click over a scroll region is inert: only the wheel scrolls
	if cmd := v2.OnMouse(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft}); cmd != nil {
		t.Fatal("a click over a scroll region must stay inert")
	}

	// content-direction wheel convention (ScrollPreview): wheel-down = +1, wheel-up = -1
	down := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown}
	if got := v2.OnMouse(down)(); got != (scrollMsg{region: "page", delta: 1}) {
		t.Fatalf("wheel down = %#v, want scrollMsg{page +1} (content-direction convention)", got)
	}
	up := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp}
	if got := v2.OnMouse(up)(); got != (scrollMsg{region: "page", delta: -1}) {
		t.Fatalf("wheel up = %#v, want scrollMsg{page -1} (content-direction convention)", got)
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
	// the root router owns the synthetic msgs and must swallow the raw
	// terminal mouse msgs too: bubbletea delivers them to Update as well,
	// so forwarding would double-fire on a future mouse-aware page
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Replace(recordingPage{id: "spy"})
	baseline := len(seenOf(m)) // Replace seeds the page with its size

	for _, msg := range []tea.Msg{
		scrollMsg{region: "page", delta: -1},
		selectMsg{region: "page", index: 0},
		focusMsg{region: "split", index: 1},
		tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown},
		tea.MouseClickMsg{X: 5, Y: 20, Button: tea.MouseLeft},
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
	// And the raw swallow must not have eaten real keys on the way.
	_, _ = m.Update(ch('7'))
	if got := m.Current().ID(); got != PageIDs[6] {
		t.Fatalf("after a typed key the page = %q, want %q (mouse case must not shadow keys)", got, PageIDs[6])
	}
}
