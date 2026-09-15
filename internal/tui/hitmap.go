// hitmap.go is the mouse hit machinery (UAT round 8, finding 9): every
// View rebuilds a per-frame map from screen cells to actions, and the
// view's OnMouse resolves the terminal-reported cell against it. A resolved
// hit replays the existing input chain — a key hit arrives as exactly the
// tea.KeyPressMsg a typed key would, so updateKey and every key.Matches
// binding fire unchanged; wheel/select/focus arrive as small root-owned
// msgs routed in root_routes.go.
//
// Geometry contract (the crux Tasks 8.2–8.5 build on):
//   - tea.Mouse X/Y are ABSOLUTE terminal cells (0-based, upper-left).
//   - Pages record their section geom.Rects CONTENT-RELATIVE (a page lays
//     out from its own origin, see pages.sectionRect). Before resolving,
//     such a Rect must be translated by the frame's content origin —
//     RootModel.contentOrigin, backed by frame.ContentOrigin (the same
//     chromeParts oracle as frame.ContentSize). NEVER resolve absolute
//     mouse coords against content-relative Rects.
//   - Frame-chrome hits (the footer row) are recorded in ABSOLUTE coords
//     directly with hitMap.add.
//
// Registration order is z-order: the LAST rect containing a cell wins, so
// overlays added after the page body shadow it, mirroring the draw order
// in root_view.go. This map is rebuilt from scratch every View; stale hits
// cannot survive a frame.
package tui

import (
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
)

// hitKind discriminates which hitAction field carries the payload. hitKey
// is the zero value so the common literal hitAction{key: "4"} needs no
// kind term.
type hitKind int

const (
	hitKey    hitKind = iota // replay a synthetic key press
	hitScroll                // scrollMsg{region, scroll}
	hitSelect                // selectMsg{region, select}
	hitFocus                 // focusMsg{region, focus}
)

// hitAction is what a resolved cell does. Exactly one payload field is
// live, named by kind; the others stay zero. (The brief's `select` field
// is spelled `sel` here: `select` is a Go keyword and cannot name a struct
// field — verified against the compiler.)
type hitAction struct {
	kind   hitKind
	key    string // hitKey: the key as key.Matches spells it ("4", "enter")
	region string // hitScroll/hitSelect/hitFocus: the region id routed to root
	scroll int    // hitScroll: content-direction delta (+1 down/forward, -1 up/back)
	sel    int    // hitSelect: target row index (0 is a legal first row)
	focus  int    // hitFocus: target pane index
}

// keyHit records a cell that replays a key press.
func keyHit(key string) hitAction { return hitAction{kind: hitKey, key: key} }

// scrollHit records a cell that scrolls region with the wheel (the delta
// itself comes from the wheel event, so the payload is filled at resolve).
func scrollHit(region string, delta int) hitAction {
	return hitAction{kind: hitScroll, region: region, scroll: delta}
}

// selectHit records a cell that selects row index in region.
func selectHit(region string, index int) hitAction {
	return hitAction{kind: hitSelect, region: region, sel: index}
}

// focusHit records a cell that focuses pane index.
func focusHit(region string, index int) hitAction {
	return hitAction{kind: hitFocus, region: region, focus: index}
}

// cmd builds the Cmd delivering this action's message into Update. A key
// hit yields the tea.KeyPressMsg exactly as the terminal delivers a typed
// key (the ch(...) construction the tests use), so the whole existing
// updateKey/key.Matches chain accepts it unchanged.
func (a hitAction) cmd() tea.Cmd {
	switch a.kind {
	case hitKey:
		kp, ok := synthKeyPress(a.key)
		if !ok {
			return nil
		}

		return func() tea.Msg { return kp }
	case hitScroll:
		return func() tea.Msg { return scrollMsg{region: a.region, delta: a.scroll} }
	case hitSelect:
		return func() tea.Msg { return selectMsg{region: a.region, index: a.sel} }
	case hitFocus:
		return func() tea.Msg { return focusMsg{region: a.region, index: a.focus} }
	}

	return nil
}

// synthKeyPress spells key as a tea.KeyPressMsg. A one-rune key mirrors the
// test helper ch exactly (Code plus Text carries the printable char, which
// is what Key.String reports for key.Matches). Longer spellings name a
// special key and map to the tea.Key* codes, which String renders back to
// the same name ("shift+tab" is Tab+ModShift, the shape the terminal
// delivers). An unknown spelling yields no key (the hit stays inert).
func synthKeyPress(key string) (tea.KeyPressMsg, bool) {
	if len(key) == 1 {
		r := rune(key[0])
		return tea.KeyPressMsg{Code: r, Text: string(r)}, true
	}
	var code rune
	var mod tea.KeyMod
	switch key {
	case "enter":
		code = tea.KeyEnter
	case "esc":
		code = tea.KeyEsc
	case "space":
		code = tea.KeySpace
	case "tab":
		code = tea.KeyTab
	case "backtab":
		code, mod = tea.KeyTab, tea.ModShift
	case "backspace":
		code = tea.KeyBackspace
	case "delete":
		code = tea.KeyDelete
	case "up":
		code = tea.KeyUp
	case "down":
		code = tea.KeyDown
	case "left":
		code = tea.KeyLeft
	case "right":
		code = tea.KeyRight
	case "pgup":
		code = tea.KeyPgUp
	case "pgdown":
		code = tea.KeyPgDown
	case "home":
		code = tea.KeyHome
	case "end":
		code = tea.KeyEnd
	default:
		return tea.KeyPressMsg{}, false
	}

	return tea.KeyPressMsg{Code: code, Mod: mod}, true
}

// hitEntry is one registered region and its action.
type hitEntry struct {
	rect geom.Rect // ABSOLUTE terminal cells
	act  hitAction
}

// hitMap is the per-frame cell→action map. Registration order is z-order;
// resolve scans backwards so the last-registered (topmost-drawn) rect wins.
type hitMap []hitEntry

// add registers an already-ABSOLUTE rect (frame chrome: the footer row).
func (h *hitMap) add(r geom.Rect, act hitAction) {
	*h = append(*h, hitEntry{rect: r, act: act})
}

// addAbs registers a CONTENT-RELATIVE rect (a page's recorded section
// Rect) translated to absolute coords by the frame's content origin
// (RootModel.contentOrigin). This is the only honest way to admit page
// geometry: mouse coords arrive absolute and pages lay out relative.
func (h *hitMap) addAbs(originX, originY int, contentRect geom.Rect, act hitAction) {
	abs := geom.Rect{
		X: contentRect.X + originX,
		Y: contentRect.Y + originY,
		W: contentRect.W,
		H: contentRect.H,
	}
	h.add(abs, act)
}

// resolve returns the action for the absolute cell (x, y): the last rect
// containing it (topmost), or false over dead space.
func (h hitMap) resolve(x, y int) (hitAction, bool) {
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].rect.Contains(x, y) {
			return h[i].act, true
		}
	}

	return hitAction{}, false
}

// contentOrigin is the absolute cell where the page body starts for the
// size the RootModel currently tracks — the offset hitMap.addAbs expects.
func (m *RootModel) contentOrigin() (x, y int) {
	return frame.ContentOrigin(m.width, m.height)
}

// buildHitMap assembles one frame's cell map. Tasks 8.2–8.5 register the
// real regions here (footer hint cells, page section rows, scroll regions,
// select/focus targets); an empty map is legal and is the state this
// foundation ships with — the mouse is enabled and every click resolves
// against whatever the map holds, which today is nothing.
func (m *RootModel) buildHitMap() hitMap {
	return hitMap{}
}

// installMouse arms the view for mouse input: CellMotion mode makes the
// terminal report clicks/wheel, and OnMouse — which bubbletea invokes with
// the mouse message against the LAST rendered view, on the event-loop
// goroutine — resolves the cell against the map View just built. The
// closure captures that map by value: stale hits cannot outlive their
// frame, and no RootModel field (or lock) is needed. Clicks replay their
// hit; the wheel becomes a scrollMsg for the hit's region; releases and
// motion stay inert so a click never double-fires.
//
// Wheel sign follows the CONTENT-DIRECTION convention shared with
// pages.Analyze.ScrollPreview (Task 7.3): wheel-DOWN is delta +1 (move the
// window down through the content), wheel-UP is -1 — so every scroll
// consumer (8.2–8.5) calls ScrollPreview(msg.delta)-style APIs directly
// with no negation.
func (m *RootModel) installMouse(out *tea.View, hm hitMap) {
	out.MouseMode = tea.MouseModeCellMotion
	out.OnMouse = func(msg tea.MouseMsg) tea.Cmd {
		mm := msg.Mouse()
		act, ok := hm.resolve(mm.X, mm.Y)
		if !ok {
			return nil
		}
		if wheel, isWheel := msg.(tea.MouseWheelMsg); isWheel {
			if act.region == "" {
				return nil // no scroll region under the cursor
			}
			delta := 1
			if wheel.Button == tea.MouseWheelUp {
				delta = -1
			}

			return func() tea.Msg { return scrollMsg{region: act.region, delta: delta} }
		}
		if _, isClick := msg.(tea.MouseClickMsg); !isClick {
			return nil // releases and motion replay nothing
		}

		return act.cmd()
	}
}

// scrollMsg moves a region's scroll position by delta rows in the
// CONTENT direction (+1 = down/forward, matching pages.Analyze.ScrollPreview;
// -1 = up/back). Routed at the root; the handler owns what the region id
// means.
type scrollMsg struct {
	region string
	delta  int
}

// selectMsg selects row index inside region.
type selectMsg struct {
	region string
	index  int
}

// focusMsg moves focus to pane index inside the page.
type focusMsg struct {
	region string
	index  int
}

// handleScrollMsg is the scrollMsg seam Tasks 8.2–8.5 fill with per-region
// scrolling; the routing skeleton exists so mouse msgs never leak to pages.
func (m *RootModel) handleScrollMsg(scrollMsg) (tea.Model, tea.Cmd) { return m, nil }

// handleSelectMsg is the selectMsg seam Tasks 8.2–8.5 fill with row
// selection (click a list row = move the cursor there).
func (m *RootModel) handleSelectMsg(selectMsg) (tea.Model, tea.Cmd) { return m, nil }

// handleFocusMsg is the focusMsg seam Tasks 8.2–8.5 fill with pane focus
// (click a split pane = Tab there).
func (m *RootModel) handleFocusMsg(focusMsg) (tea.Model, tea.Cmd) { return m, nil }
