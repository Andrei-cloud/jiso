// send.go is the §D live exchange view: presentation only.
// It renders the request/response panes, the segmented stage indicator,
// the root-stamped elapsed/budget timer, the timeout banner, and the RC
// badge. The live operation itself (stage machine, timeout, validation,
// correlation) lives in root (root_send.go); this page never touches
// internal/app, never reads the clock, and never ticks itself.
package pages

import (
	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
)

// Send is the §D page. It is a reference type: the router pushes one
// canonical instance for the §D view, and SetState preserves the page-owned
// HexOn toggle across root pushes.
type Send struct {
	th    *theme.Theme
	state SendState

	nav sendNav

	width, height int // last tea.WindowSizeMsg (terminal, not content area)

	// sections records the geom.Rect of every widgets.Section this
	// page drew during the last render, in draw order and with a
	// content-relative origin (the hit-map finalises the
	// absolute offsets into the frame chrome).
	sections []geom.Rect

	// Scroll state (UAT: the panes clip long messages): a per-pane line
	// offset, the focused pane keyboard scrolls (tab switches), and the
	// inner content heights the last render drew, so clamps and page
	// steps can never go stale.
	reqScroll, respScroll       int
	scrollFocusResp             bool
	paneInnerReq, paneInnerResp int
}

// sendNav is the §D keymap: Esc pops, Enter re-sends through the same
// TxSendMsg path the §B page uses (root ignores it while one op is
// in flight), h toggles the panes between Describe and hexdump.
type sendNav struct {
	Cancel    key.Binding
	Resend    key.Binding
	Hex       key.Binding
	ScrollUp  key.Binding
	ScrollDn  key.Binding
	PageUp    key.Binding
	PageDn    key.Binding
	ScrollTop key.Binding
	ScrollEnd key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newSendNav() sendNav {
	nav := sendNav{
		Cancel:    key.NewBinding(key.WithKeys(sendKeyPop)),
		Resend:    key.NewBinding(key.WithKeys(sendKeyResend)),
		Hex:       key.NewBinding(key.WithKeys(sendKeyHex)),
		ScrollUp:  key.NewBinding(key.WithKeys("k", "up")),
		ScrollDn:  key.NewBinding(key.WithKeys("j", "down")),
		PageUp:    key.NewBinding(key.WithKeys("pgup")),
		PageDn:    key.NewBinding(key.WithKeys("pgdown")),
		ScrollTop: key.NewBinding(key.WithKeys("home")),
		ScrollEnd: key.NewBinding(key.WithKeys("end")),
	}
	nav.help = []HelpEntry{
		actEntry("send again", nav.Resend),
		actEntry("describe/hexdump toggle", nav.Hex),
		actEntry("scroll focused pane", nav.ScrollDn),
		actEntry("scroll focused pane up", nav.ScrollUp),
		actEntry("page down", nav.PageDn),
		actEntry("page up", nav.PageUp),
		actEntry("pane top", nav.ScrollTop),
		actEntry("pane bottom", nav.ScrollEnd),
		actEntry("back", nav.Cancel),
	}

	return nav
}

// NewSend builds the page. A nil theme selects theme.Default
// (production); golden tests inject an explicit NewWith profile.
func NewSend(th *theme.Theme) *Send {
	if th == nil {
		th = theme.Default()
	}

	return &Send{th: th, nav: newSendNav()}
}

// ID reports the router id of this page (SendPageID).
func (s *Send) ID() string { return SendPageID }

// Theme exposes the resolved theme (view helpers and tests).
func (s *Send) Theme() *theme.Theme { return s.th }

// Size reports the last terminal size seen via WindowSizeMsg.
func (s *Send) Size() (width, height int) { return s.width, s.height }

// State exposes the current snapshot (tests and future deep links).
func (s *Send) State() SendState { return s.state }

// SetState replaces the rendered snapshot (root pushes it on every Update
// while the op runs, frozen after completion). The `h` toggle and the
// scroll offsets are page-owned presentation state and survive pushes of
// the same exchange; a fresh exchange (new tx, or a completed snapshot
// giving way to a live run) starts back at the top.
func (s *Send) SetState(state SendState) {
	state.HexOn = s.state.HexOn
	if state.TxID != s.state.TxID || (!state.Done && s.state.Done) {
		s.reqScroll, s.respScroll = 0, 0
	}
	s.state = state
}

// Update routes sizes and keys; everything else (bus events, pane focus)
// does not concern this page and is ignored with a nil command.
func (s *Send) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return s.updateKey(msg)
	case PaneFocusMsg:
		s.scrollFocusResp = !s.scrollFocusResp // the keyboard now scrolls the other pane

		return s, nil
	}

	return s, nil
}

// updateKey is the page-local keymap. The Enter re-send yields exactly the
// same TxSendMsg the §B page yields, so root's one-in-flight rule applies
// to both entry points.
func (s *Send) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	switch {
	case key.Matches(msg, s.nav.Cancel):
		return s, func() tea.Msg { return SendPopMsg{} }
	case key.Matches(msg, s.nav.Resend):
		id := s.state.TxID
		return s, func() tea.Msg { return TxSendMsg{ID: id} }
	case key.Matches(msg, s.nav.Hex):
		s.state.HexOn = !s.state.HexOn

		return s, nil
	case key.Matches(msg, s.nav.ScrollUp):
		s.scrollFocused(-1)
	case key.Matches(msg, s.nav.ScrollDn):
		s.scrollFocused(1)
	case key.Matches(msg, s.nav.PageUp):
		s.scrollFocused(-s.pageStep())
	case key.Matches(msg, s.nav.PageDn):
		s.scrollFocused(s.pageStep())
	case key.Matches(msg, s.nav.ScrollTop):
		s.setFocused(0)
	case key.Matches(msg, s.nav.ScrollEnd):
		s.setFocusedEnd()
	}

	return s, nil
}

// --- focused-pane scrolling (UAT: long messages clipped the panes) ------
//
// The keyboard scrolls whichever pane tab last focused; the wheel scrolls
// the pane under the cursor directly (Scroller below). Offsets clamp
// against the INNER height the last render actually drew and the lines
// the pane currently shows (rows or, with h, the hexdump), so a resize or
// a shorter late response can never strand the view below its content.

// focusedIsRequest reports the keyboard scroll target.
func (s *Send) focusedIsRequest() bool { return !s.scrollFocusResp }

// contentLen counts the lines the pane currently shows.
func (s *Send) contentLen(request bool) int {
	dump, rows := s.state.ResponseHex, s.state.Response
	if request {
		dump, rows = s.state.RequestHex, s.state.Request
	}
	if s.state.HexOn && len(dump) > 0 {
		return len(dump)
	}
	if len(rows) == 0 {
		return 1 // the placeholder line is the pane's only line
	}

	return len(rows)
}

// maxScroll is the deepest offset the focused pane can reach; 0 before
// the first render (no truthful inner height yet).
func (s *Send) maxScroll(request bool) int {
	inner := s.paneInnerReq
	if !request {
		inner = s.paneInnerResp
	}
	if inner <= 0 {
		return 0
	}

	return max(s.contentLen(request)-inner, 0)
}

func (s *Send) scrollFocused(d int) { s.setFocused(s.curFocused() + d) }

func (s *Send) curFocused() int { return s.curScroll(s.focusedIsRequest()) }

func (s *Send) curScroll(request bool) int {
	if request {
		return s.reqScroll
	}

	return s.respScroll
}

// setFocused clamps and applies the focused pane's offset.
func (s *Send) setFocused(v int) {
	request := s.focusedIsRequest()
	v = min(max(v, 0), s.maxScroll(request))
	if request {
		s.reqScroll = v
	} else {
		s.respScroll = v
	}
}

// setFocusedEnd jumps to the deepest reachable line of the focused pane.
func (s *Send) setFocusedEnd() {
	if s.focusedIsRequest() {
		s.reqScroll = s.maxScroll(true)
	} else {
		s.respScroll = s.maxScroll(false)
	}
}

// pageStep is a comfortable overlap-free page jump for the focused pane.
func (s *Send) pageStep() int {
	inner := s.paneInnerReq
	if !s.focusedIsRequest() {
		inner = s.paneInnerResp
	}
	if inner < 3 {
		return 1
	}

	return inner - 2
}

// ScrollRegions publishes the two pane boxes the last render drew
// (sections are recorded in draw order: request first).
func (s *Send) ScrollRegions() []ScrollRegion {
	if len(s.sections) < 2 {
		return nil
	}

	return []ScrollRegion{
		{ID: RegionSendRequest, Rect: s.sections[0]},
		{ID: RegionSendResponse, Rect: s.sections[1]},
	}
}

// ScrollRegion applies a wheel delta (d>0 = toward later lines) to the
// pane under the cursor, clamped exactly like the keyboard.
func (s *Send) ScrollRegion(id string, d int) bool {
	switch id {
	case RegionSendRequest:
		s.reqScroll = min(max(s.reqScroll+d, 0), s.maxScroll(true))
	case RegionSendResponse:
		s.respScroll = min(max(s.respScroll+d, 0), s.maxScroll(false))
	default:
		return false
	}

	return true
}

// Hints is the §D context keymap; re-send/back are primary so the narrow
// footer keeps them (the router appends the global bindings).
func (s *Send) Hints() []frame.KeyHint {
	return []frame.KeyHint{
		{Key: sendKeyResend, Desc: "send again", Primary: true},
		{Key: sendKeyPop, Desc: "back", Primary: true},
		{Key: sendKeyHex, Desc: "hexdump"},
		{Key: "j/k", Desc: "scroll"},
		{Key: "tab", Desc: "scroll focus"},
	}
}
