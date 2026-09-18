// hitmap.go is the mouse hit machinery: every View rebuilds a per-frame
// cell→action map and OnMouse resolves terminal cells against it. A key hit
// replays as exactly the tea.KeyPressMsg a typed key would (updateKey and
// key.Matches fire unchanged); wheel/select/focus are root-owned msgs routed
// in root_routes.go.
//
// Geometry: mouse X/Y are ABSOLUTE terminal cells, page section Rects are
// CONTENT-RELATIVE — translate by RootModel.contentOrigin before resolving,
// never absolute coords against relative Rects. Frame chrome (the footer)
// registers in ABSOLUTE coords via hitMap.add.
//
// Registration order is z-order: the LAST rect containing a cell wins,
// mirroring the draw order in root_view.go; stale hits cannot survive a frame.
package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// hitKind discriminates which hitAction payload field is live. hitKey is the
// zero value, so hitAction{key: "4"} needs no kind term.
type hitKind int

const (
	hitKey    hitKind = iota // replay a synthetic key press
	hitScroll                // wheel-only: a click on it is inert
	hitSelect                // selectMsg{region, select}
	hitFocus                 // focusMsg{region, focus}
)

// hitAction is what a resolved cell does: exactly one payload field is live,
// named by kind. The select target is spelled sel — select is a Go keyword.
type hitAction struct {
	kind   hitKind
	key    string // hitKey: the key as key.Matches spells it ("4", "enter")
	region string // hitScroll/hitSelect/hitFocus: the region id routed to root
	sel    int    // hitSelect: target row index (0 is a legal first row)
	focus  int    // hitFocus: target pane index
}

// keyHit records a cell that replays a key press.
func keyHit(key string) hitAction { return hitAction{kind: hitKey, key: key} }

// scrollHit records a cell whose wheel scrolls region. The delta comes from
// the wheel event itself; a click on the cell is inert.
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

// cmd builds the Cmd delivering this action's message into Update; a key hit
// arrives exactly as a typed key, so the updateKey/key.Matches chain accepts
// it unchanged.
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

// synthKeyPress spells key as a tea.KeyPressMsg: single runes carry their
// printable char, named specials map to tea.Key* codes ("backtab" is
// Tab+ModShift). Unknown spellings and ctrl/alt chords return false, leaving
// the hit inert (see registerFooterHits).
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

// addAbs registers a CONTENT-RELATIVE page rect translated to absolute coords
// by the frame's content origin — the only honest way to admit page geometry.
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

// buildHitMap assembles one frame's cell→action map in z-order: page body
// first, overlays last. An empty map is legal (§G before its first log line,
// pages without scrollable panes) and mandatory below frame.MinWidth, where
// page geometry would resolve phantom hits over ink that is not drawn.
func (m *RootModel) buildHitMap() hitMap {
	hm := hitMap{}
	// mouse off: register nothing, so a straggler report resolves no hit.
	if !m.mouseEnabled {
		return hm
	}
	if m.width < frame.MinWidth {
		return hm // too-small frame: no page geometry is truthful
	}
	ox, oy := m.contentOrigin()
	if sc, ok := m.Current().(pages.Scroller); ok {
		for _, r := range sc.ScrollRegions() {
			hm.addAbs(ox, oy, r.Rect, scrollHit(r.ID))
		}
	}
	// rows register AFTER the pane scrollHits: a click resolves the ROW,
	// while the wheel over the same cell still scrolls the PANE.
	if sl, ok := m.Current().(pages.Selector); ok {
		for _, r := range sl.SelectRegions() {
			hm.addAbs(ox, oy, r.Rect, selectHit(r.ID, r.Index))
		}
	}
	// click-to-focus (registerFocusHits): page rects first, modals over the
	// page; the §M box and picker rows below shadow the modals they cover.
	m.registerFocusHits(&hm)
	// §M box: root-owned, added last so the wheel over it never scrolls
	// the page underneath.
	if m.help != nil {
		hm.add(m.helpHitRect(), scrollHit(regionHelp))
	}
	// picker rows: likewise root-owned, over the page and box — but never
	// while a §N3 confirm draws over the picker (phantom hits under its ink).
	if m.filePick != nil && !m.confirmPending() {
		for _, r := range m.pickerRowHits() {
			hm.add(r.Rect, selectHit(regionPicker, r.Index))
		}
	}
	// §4 error screen: root-owned and topmost-drawn among the boxes, so it
	// registers over everything they drew. The canvas-wide entry closes it
	// (a dead-space click replays esc, the same key the footer spells); the
	// box rect registers after, so the wheel over the body scrolls the
	// body and a click inside the box stays inert.
	if m.errModal != nil {
		inner := m.innerWS()
		hm.add(geom.Rect{X: ox, Y: oy, W: inner.Width, H: inner.Height}, keyHit(theme.KeyEsc))
		hm.add(m.errModalHitRect(), scrollHit(regionErrModal))
	}
	// footer legend (hitmap_footer.go): topmost drawn, added last.
	m.registerFooterHits(&hm)

	return hm
}

// regionHelp names the §M help box's scroll region (overlay: the box
// scrolls its own keymap window through helpOverlay.ScrollBy).
const regionHelp = "help:box"

// regionErrModal names the error screen's scroll region (overlay: the box
// scrolls its own wrapped body through errorModal.ScrollBy).
const regionErrModal = "error:box"

// regionPicker names the shared file picker's entry rows: a click moves the
// picker cursor and runs the widget's own entry selection, dispatched directly
// in handleSelectMsg instead of through a page seam.
const regionPicker = "picker:entries"

// helpHitRect re-measures the §M box's ABSOLUTE drawn rect, mirroring
// overlayCenter's centering math offset by the content origin; the height
// clamps so a taller box never shadows rows below it.
func (m *RootModel) helpHitRect() geom.Rect {
	hv := m.help.View()
	inner := m.innerWS()
	ox, oy := m.contentOrigin()

	bw, bl := lipgloss.Width(hv), lipgloss.Height(hv)
	x := max((inner.Width-bw)/2, 0)
	y := max((inner.Height-bl)/2, 0)

	return geom.Rect{X: ox + x, Y: oy + y, W: bw, H: min(bl, inner.Height-y)}
}

// errModalHitRect re-measures the error screen's ABSOLUTE drawn rect with
// overlayCenter's centering math, mirroring helpHitRect; View is pure
// display state, so re-rendering here matches the ink the frame just drew.
func (m *RootModel) errModalHitRect() geom.Rect {
	ev := m.errModal.View(m.innerWS().Width, m.innerWS().Height)
	inner := m.innerWS()
	ox, oy := m.contentOrigin()

	bw, bl := lipgloss.Width(ev), lipgloss.Height(ev)
	x := max((inner.Width-bw)/2, 0)
	y := max((inner.Height-bl)/2, 0)

	return geom.Rect{X: ox + x, Y: oy + y, W: bw, H: min(bl, inner.Height-y)}
}

// pickerRowHits re-measures the picker's ABSOLUTE entry-row rects with the
// same centering math as helpHitRect (one border column and one header offset
// in). Rows overlayCenter clips below the canvas publish nothing, so a click
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

// scrollMsg moves a region's scroll position by delta in the CONTENT
// direction (+1 = down/forward, matching pages.Analyze.ScrollPreview).
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

// handleScrollMsg resolves a scrollMsg to the active scrollable and applies
// the delta with the content-direction convention (no negation). The §M box is
// checked first; while any root-owned modal is open the page branch stays
// inert, so the wheel never scrolls the frozen page behind it. Unknown regions
// and closed overlays stay inert.
func (m *RootModel) handleScrollMsg(msg scrollMsg) (tea.Model, tea.Cmd) {
	if msg.region == regionHelp {
		if m.help != nil { // a straggler after Esc closed the overlay: inert
			m.help.ScrollBy(msg.delta)
		}

		return m, nil
	}
	if msg.region == regionErrModal {
		if m.errModal != nil { // a straggler after the screen closed: inert
			m.errModal.ScrollBy(msg.delta)
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

// modalOpen reports whether a root-owned modal (palette, dialogs, wizards,
// file picker, §M box, error screen, or a pending §N3 confirm) owns the
// screen; while one is open the page behind must stay frozen.
func (m *RootModel) modalOpen() bool {
	if m.pal != nil || m.dlg != nil || m.wizard != nil || m.serverDlg != nil ||
		m.workerWiz != nil || m.help != nil || m.filePick != nil || m.errModal != nil {
		return true
	}

	return m.confirmPending()
}

// confirmPending reports a pending §N3 confirm, which the overlay stack draws
// last — over even the file picker, whose click rows stop publishing while
// one is up.
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

// handleSelectMsg resolves a selectMsg to a drawn row and moves the cursor
// through the page's own clamping rules, exactly like the keyboard. The picker
// is checked first (its rows stay clickable while it owns the screen);
// otherwise the modalOpen gate keeps clicks from selecting under any overlay,
// and unknown regions stay inert.
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
		// a §I list-row click is a cursor move: the detail panes follow it
		// through handleSessionsFocus, the keyboard's own load seam.
		if msg.region == pages.RegionSessionsList && m.sessions != nil {
			return m.handleSessionsFocus(m.sessions.SelectedSessionID())
		}
	}

	return m, nil
}

// handleFocusMsg (the focusMsg seam) is implemented in hitmap_focus.go.
