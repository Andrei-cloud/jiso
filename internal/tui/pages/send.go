// send.go is the §D live exchange view (wireframe §D): presentation only.
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
	// content-relative origin (Phase 8's hit-map finalises the
	// absolute offsets into the frame chrome).
	sections []geom.Rect
}

// sendNav is the §D keymap: Esc pops, Enter re-sends through the same
// TxSendMsg path the §B page uses (root ignores it while one op is
// in flight), h toggles the panes between Describe and hexdump.
type sendNav struct {
	Cancel key.Binding
	Resend key.Binding
	Hex    key.Binding

	help []HelpEntry // §M registry, built from the bindings above
}

func newSendNav() sendNav {
	nav := sendNav{
		Cancel: key.NewBinding(key.WithKeys(sendKeyPop)),
		Resend: key.NewBinding(key.WithKeys(sendKeyResend)),
		Hex:    key.NewBinding(key.WithKeys(sendKeyHex)),
	}
	nav.help = []HelpEntry{
		actEntry("send again", nav.Resend),
		actEntry("describe/hexdump toggle", nav.Hex),
		actEntry("back", nav.Cancel),
	}

	return nav
}

// NewSend builds the page. A nil theme selects theme.Default()
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
// while the op runs, frozen after completion). The `h` toggle is
// page-owned presentation state and survives the push.
func (s *Send) SetState(state SendState) {
	state.HexOn = s.state.HexOn
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
	}

	return s, nil
}

// Hints is the §D context keymap; re-send/back are primary so the narrow
// footer keeps them (the router appends the global bindings).
func (s *Send) Hints() []frame.KeyHint {
	return []frame.KeyHint{
		{Key: sendKeyResend, Desc: "send again", Primary: true},
		{Key: sendKeyPop, Desc: "back", Primary: true},
		{Key: sendKeyHex, Desc: "hexdump"},
	}
}
