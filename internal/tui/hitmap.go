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
	"strings"

	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// hitKind discriminates which hitAction field carries the payload. hitKey
// is the zero value so the common literal hitAction{key: "4"} needs no
// kind term.
type hitKind int

const (
	hitKey    hitKind = iota // replay a synthetic key press
	hitScroll                // wheel-only: a click on it is inert
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
	sel    int    // hitSelect: target row index (0 is a legal first row)
	focus  int    // hitFocus: target pane index
}

// keyHit records a cell that replays a key press.
func keyHit(key string) hitAction { return hitAction{kind: hitKey, key: key} }

// scrollHit records a cell whose WHEEL scrolls region. The delta comes
// from the wheel event itself (installMouse decodes it), so the payload
// carries only the region id; a left-click on the cell is inert —
// clicking a scrollable pane does nothing, only the wheel acts.
func scrollHit(region string) hitAction {
	return hitAction{kind: hitScroll, region: region}
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
		return nil // clicking a scrollable pane does nothing; only the wheel acts
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

// buildHitMap assembles one frame's cell map. Task 8.2b registered the
// first real regions — the top page's wheel-scroll regions (published
// through pages.Scroller, so no page region id is hardcoded here) and the
// §M overlay's box; Task 8.3 added the pages' click-selectable rows
// (pages.Selector, registered over the pane scrollHits) and the file
// picker's entry rows; footer hint cells and focus targets arrive with
// Tasks 8.4–8.5.
//
// Registration order is z-order (page body first, overlays last), and an
// empty map is legal: it is §G before its first log line, every page
// without scrollable panes, and — by policy — every frame narrower than
// frame.MinWidth, where the body is the too-small notice but the pages
// have still recorded their section Rects, so registering them would
// resolve phantom hits over ink that is not on screen.
func (m *RootModel) buildHitMap() hitMap {
	hm := hitMap{}
	if m.width < frame.MinWidth {
		return hm // too-small frame: no page geometry is truthful
	}
	ox, oy := m.contentOrigin()
	if sc, ok := m.Current().(pages.Scroller); ok {
		for _, r := range sc.ScrollRegions() {
			hm.addAbs(ox, oy, r.Rect, scrollHit(r.ID))
		}
	}
	// Task 8.3: the pages' drawn rows register AFTER the pane scrollHits
	// so a click resolves the ROW (topmost), while the wheel over the
	// same cell still scrolls the PANE — a selectHit carries the pane's
	// own region id, and installMouse routes the wheel by action.region.
	if sl, ok := m.Current().(pages.Selector); ok {
		for _, r := range sl.SelectRegions() {
			hm.addAbs(ox, oy, r.Rect, selectHit(r.ID, r.Index))
		}
	}
	// The §M overlay is root-owned modal state, not a page: its region is
	// spelled here and dispatched directly in handleScrollMsg. Added last
	// so the wheel over the box never scrolls the page underneath it.
	if m.help != nil {
		hm.add(m.helpHitRect(), scrollHit(regionHelp))
	}
	// The file picker (Task 8.3) is likewise root-owned: its entry rows
	// register over everything the page and the box below them drew —
	// but NOT while a §N3 confirm draws over the picker itself, which
	// would resolve phantom hits under the confirm's ink.
	if m.filePick != nil && !m.confirmPending() {
		for _, r := range m.pickerRowHits() {
			hm.add(r.Rect, selectHit(regionPicker, r.Index))
		}
	}

	return hm
}

// regionHelp names the §M help box's scroll region (overlay: the box
// scrolls its own keymap window through helpOverlay.ScrollBy).
const regionHelp = "help:box"

// regionPicker names the shared file picker's entry rows (root-owned
// overlay, Task 8.3): a click on a row moves the picker cursor AND runs
// the widget's own entry selection, so the region is dispatched directly
// in handleSelectMsg instead of through a page seam.
const regionPicker = "picker:entries"

// helpHitRect is the §M box's ABSOLUTE drawn rect: it re-measures the
// same View string root_view.go composited and mirrors overlayCenter's
// centering math (root_overlay.go) on the content canvas, offset by the
// frame's content origin. The height clamps to the visible canvas so a
// box taller than the content area never shadows rows below it.
func (m *RootModel) helpHitRect() geom.Rect {
	hv := m.help.View()
	inner := m.innerWS()
	ox, oy := m.contentOrigin()

	bw, bl := lipgloss.Width(hv), lipgloss.Height(hv)
	x := max((inner.Width-bw)/2, 0)
	y := max((inner.Height-bl)/2, 0)

	return geom.Rect{X: ox + x, Y: oy + y, W: bw, H: min(bl, inner.Height-y)}
}

// pickerRowHits is the file picker's ABSOLUTE drawn entry-row rects
// (Task 8.3): the box string is recomposed exactly as root_view.go
// composites it (boxed + overlayCenter) and the centering math mirrors
// overlayCenter on the content canvas, offset by the frame's content
// origin — the helpHitRect trick applied to the widget's measured row
// rects (one border column and one header offset in). Rows whose box
// line overlayCenter clips below the canvas publish nothing, so a click
// on dead space below the picker stays inert.
func (m *RootModel) pickerRowHits() []widgets.RowHit {
	inner := m.innerWS()
	box := modalBox(m.themeOrNil(), modalBoxWidth(inner.Width), m.filePick.View())
	lines := strings.Split(strings.TrimRight(box, "\n"), "\n")
	x := max((inner.Width-boxWidth(lines))/2, 0)
	y := max((inner.Height-len(lines))/2, 0)
	ox, oy := m.contentOrigin()

	var out []widgets.RowHit
	for _, rh := range m.filePick.RowHits() {
		row := geom.Rect{X: ox + x + 1 + rh.Rect.X, Y: oy + y + 1 + rh.Rect.Y, W: rh.Rect.W, H: 1}
		if row.Y-oy >= inner.Height {
			continue // the box row is not drawn: overlayCenter stops splicing
		}
		out = append(out, widgets.RowHit{Rect: row, Index: rh.Index})
	}

	return out
}

// installMouse arms the view for mouse input: CellMotion mode makes the
// terminal report clicks/wheel, and OnMouse — which bubbletea invokes with
// the mouse message against the LAST rendered view, on the event-loop
// goroutine — resolves the cell against the map View just built. The
// closure captures that map by value: stale hits cannot outlive their
// frame, and no RootModel field (or lock) is needed. Clicks replay their
// hit; the wheel becomes a scrollMsg for the hit's region; releases and
// motion stay inert so a click never double-fires. Task 8.2b's button
// policy narrows this further: only the LEFT button replays a hit and
// only the vertical wheel steps produce a scrollMsg — middle/right
// clicks and horizontal wheel steps stay inert, so they can neither
// replay key hits nor fake a vertical scroll.
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
			var delta int
			switch wheel.Button {
			case tea.MouseWheelUp:
				delta = -1
			case tea.MouseWheelDown:
				delta = 1
			default:
				return nil // horizontal wheel steps scroll nothing vertical
			}
			if act.region == "" {
				return nil // no scroll region under the cursor
			}

			return func() tea.Msg { return scrollMsg{region: act.region, delta: delta} }
		}
		if _, isClick := msg.(tea.MouseClickMsg); !isClick {
			return nil // releases and motion replay nothing
		}
		if mm.Button != tea.MouseLeft {
			return nil // middle/right clicks replay no key hit (Task 8.2b policy)
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

// handleScrollMsg is the scrollMsg seam (Tasks 8.2b/8.2c): it resolves
// the region to the ACTIVE scrollable and applies the delta with the
// content-direction convention (+1 = down, no negation). The §M overlay
// is checked first — while open it is the topmost layer, and its region
// is root-owned. Then a modal gate (UAT round 8 Task 8.2c): while ANY
// root-owned modal is open (palette, dialog/wizard, file picker, a
// pending confirm, or §M itself) the page branch is inert, so a wheel
// over the page area behind a modal — or in the margins around the §M
// box — never scrolls the frozen page underneath. Otherwise the delta
// goes to the top page through pages.Scroller, which reports whether it
// owns the region. Unknown regions and closed overlays stay inert: a
// mouse msg never reaches a page as a key.
func (m *RootModel) handleScrollMsg(msg scrollMsg) (tea.Model, tea.Cmd) {
	if msg.region == regionHelp {
		if m.help != nil { // a straggler after Esc closed the overlay: inert
			m.help.ScrollBy(msg.delta)
		}

		return m, nil
	}
	if m.modalOpen() {
		return m, nil // the modal owns the screen; the page behind stays frozen
	}
	if sc, ok := m.Current().(pages.Scroller); ok {
		sc.ScrollRegion(msg.region, msg.delta)
	}

	return m, nil
}

// modalOpen reports whether a root-owned modal owns the screen: the
// command palette, the connect/server dialogs, the send/worker wizards,
// the shared file picker, the §M help overlay, or any pending §N3
// confirm. This mirrors the modal chain updateKey routes ahead of the
// page (root_keys.go) and the overlay stack View composes (root_view.go)
// — a wheel resolved to page geometry while any of these is open must
// stay inert instead of scrolling the frozen page behind the modal.
func (m *RootModel) modalOpen() bool {
	if m.pal != nil || m.dlg != nil || m.wizard != nil || m.serverDlg != nil ||
		m.workerWiz != nil || m.help != nil || m.filePick != nil {
		return true
	}

	return m.confirmPending()
}

// confirmPending reports a pending §N3 confirm — the overlay stack draws
// these LAST, above even the file picker, so the picker's click rows
// stop publishing while one is up (buildHitMap).
func (m *RootModel) confirmPending() bool {
	for _, c := range []*widgets.ConfirmDialog{
		m.serverConfirm, m.workersConfirm, m.analyzeConfirm, m.ctfConfirm,
		m.analyzeOverwriteConfirm, m.scenarioConfirm, m.disconnectConfirm,
	} {
		if c != nil && c.Pending() {
			return true
		}
	}

	return false
}

// handleSelectMsg is the selectMsg seam (Task 8.3): a left click
// resolved to a drawn row selects it — the cursor moves to the row's
// data index through the page's own clamping and identity rules, exactly
// like the keyboard. The file picker is checked first: like the §M box
// in handleScrollMsg it is root-owned modal state whose own rows stay
// clickable while it owns the screen, and its click additionally runs
// the widget's entry selection (descend a directory, commit a selectable
// file). Then the SAME modal gate handleScrollMsg uses: while any
// root-owned modal is open, a page-behind region stays inert, so a click
// can never select under the overlay. Otherwise the region resolves
// through pages.Selector on the top page (the Scroller pattern); unknown
// regions and pages without the seam stay inert: a mouse msg never
// reaches a page as a key.
func (m *RootModel) handleSelectMsg(msg selectMsg) (tea.Model, tea.Cmd) {
	if msg.region == regionPicker {
		if m.filePick != nil { // a straggler after the picker closed: inert
			return m, m.filePick.SelectRow(msg.index)
		}

		return m, nil
	}
	if m.modalOpen() {
		return m, nil // the modal owns the screen; the page behind stays frozen
	}
	if sl, ok := m.Current().(pages.Selector); ok {
		sl.SelectRegion(msg.region, msg.index)
	}

	return m, nil
}

// handleFocusMsg is the focusMsg seam Tasks 8.2–8.5 fill with pane focus
// (click a split pane = Tab there).
func (m *RootModel) handleFocusMsg(focusMsg) (tea.Model, tea.Cmd) { return m, nil }
