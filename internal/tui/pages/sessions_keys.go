// sessions_keys.go is §I's keyboard: the three modes the page can be in (navigate,
// filter, drill into a session's transactions) and what each key means in them. The
// page never reads the database; it emits the message the root acts on.
package pages

import (
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
)

// Update routes sizes, the router's pane-focus tabs, and keys; bus
// events are root-side truth (refresh is root-armed) and are ignored
// with a nil command.
func (s *Sessions) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
	case PaneFocusMsg:
		if !s.reviewOpen {
			s.pane = (s.pane + paneCount + boolToStep(msg.Reverse)) % paneCount
		}
	case tea.KeyPressMsg:
		return s.updateKey(msg)
	}

	return s, nil
}

// boolToStep maps shift-Tab to the reverse cycle step.
func boolToStep(reverse bool) int {
	if reverse {
		return -1
	}

	return 1
}

// updateKey is the page-local keymap: the review overlay owns the
// keyboard first, then the narrow drill, then filter mode, then the
// page triggers, then the focused table's navigation.
func (s *Sessions) updateKey(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	if s.reviewOpen {
		return s.updateReview(msg)
	}
	if s.drill {
		return s.updateDrill(msg)
	}
	if s.filtering {
		return s.updateFilter(msg)
	}

	switch {
	case key.Matches(msg, s.nav.Filter):
		s.filtering = true

		return s, nil
	case key.Matches(msg, s.nav.Refresh):
		return s, func() tea.Msg { return SessionsRefreshMsg{} }
	case key.Matches(msg, s.nav.Review):
		if id := s.SelectedTxID(); id != 0 {
			return s, func() tea.Msg { return SessionsReviewMsg{TxID: id} }
		}

		return s, nil
	case key.Matches(msg, s.nav.Enter):
		return s.updateEnter()
	case key.Matches(msg, s.nav.Cancel):
		return s, func() tea.Msg { return SessionsPopMsg{} }
	default:
		return s.updateNav(msg)
	}
}

// updateReview is the review overlay's keyboard: Esc closes first, then
// j/k (and arrows) scroll one line and pgup/pgdn page (UAT round 5: a
// review taller than the window was unreachable — the RESPONSE sections
// and even the esc hint were clipped off-screen with no way to reach
// them). Unknown keys are swallowed; the overlay owns the keyboard.
func (s *Sessions) updateReview(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	switch {
	case key.Matches(msg, s.nav.Cancel):
		s.reviewOpen = false
	case key.Matches(msg, s.nav.Up):
		s.scrollReviewBy(-1)
	case key.Matches(msg, s.nav.Down):
		s.scrollReviewBy(1)
	case key.Matches(msg, s.nav.PageUp):
		s.scrollReviewBy(-s.reviewPageStep())
	case key.Matches(msg, s.nav.PageDown):
		s.scrollReviewBy(s.reviewPageStep())
	}

	return s, nil
}

// reviewPageStep is the review viewport's visible line count, the page
// size for pgup/pgdn.
func (s *Sessions) reviewPageStep() int {
	return max(s.reviewViewport(), 1)
}

// reviewViewport is the review body's visible line count (the frame
// content height minus the pinned head lines). 0 when the overlay has
// no review to show.
func (s *Sessions) reviewViewport() int {
	if !s.reviewOpen || s.state.Review == nil {
		return 0
	}
	_, h := frame.ContentSize(s.width, s.height)

	return max(h-s.reviewHeadH(), 1)
}

// scrollReviewBy moves the overlay's top line, clamped to the scroll
// extent (the view clamps again against the live content, so a stale
// scroll after a resize still renders sanely).
func (s *Sessions) scrollReviewBy(delta int) {
	s.reviewScroll = max(s.reviewScroll+delta, 0)
	if ext := s.reviewScrollExtent(); ext > 0 && s.reviewScroll > ext {
		s.reviewScroll = ext
	}
}

// reviewHeadH counts the pinned head lines the review render prepends
// (mirrors render's head composition: title + note + empty hint).
func (s *Sessions) reviewHeadH() int {
	n := 1
	if s.state.Note != "" {
		n++
	}
	if s.emptyHintLine() != "" {
		n++
	}

	return n
}

// reviewScrollExtent is the maximum top-line offset: total review lines
// (head + body) minus the viewport, 0 when everything fits.
func (s *Sessions) reviewScrollExtent() int {
	if s.state.Review == nil {
		return 0
	}
	total := len(strings.Split(strings.TrimRight(s.reviewBody(), "\n"), "\n"))
	_, h := frame.ContentSize(s.width, s.height)

	return max(total+s.reviewHeadH()-h, 0)
}

// updateEnter: Enter in SESSIONS selects the session (root loads
// stats+history; in the narrow fallback it also drills in); Enter in TX
// HISTORY opens the reconstructed review.
func (s *Sessions) updateEnter() (Page, tea.Cmd) {
	if s.pane == paneSessions {
		id := s.SelectedSessionID()
		if id == "" {
			return s, nil
		}
		if s.width < frame.FullWidth {
			s.drill = true
		}

		return s, func() tea.Msg { return SessionsSelectMsg{ID: id} }
	}
	if id := s.SelectedTxID(); id != 0 {
		return s, func() tea.Msg { return SessionsReviewMsg{TxID: id} }
	}

	return s, nil
}

// updateDrill is the narrow fallback's stacked stats+history view: Esc
// returns to the list; Enter/[t] review the selected tx; the rest
// navigate the history table.
func (s *Sessions) updateDrill(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	switch {
	case key.Matches(msg, s.nav.Cancel):
		s.drill = false

		return s, nil
	case key.Matches(msg, s.nav.Review), key.Matches(msg, s.nav.Enter):
		if id := s.SelectedTxID(); id != 0 {
			return s, func() tea.Msg { return SessionsReviewMsg{TxID: id} }
		}

		return s, nil
	default:
		next, cmd := s.history.Update(msg)
		s.history = next
		s.syncTxID()

		return s, cmd
	}
}

// updateFilter is filter-mode editing (the §B live-filter contract):
// Esc clears, Enter yields the keyboard, printable keys narrow the
// list as typed.
func (s *Sessions) updateFilter(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	switch {
	case key.Matches(msg, s.nav.Cancel):
		s.filter, s.filtering = "", false
		s.rebuild()

		return s, nil
	case key.Matches(msg, s.nav.Backspace):
		s.filter = dropLastRune(s.filter)
		s.rebuild()

		return s, nil
	case key.Matches(msg, s.nav.Enter):
		s.filtering = false

		return s, nil
	}
	if r, ok := printableRune(msg.Text); ok {
		s.filter += string(r)
		s.rebuild()

		return s, nil
	}

	return s.updateNav(msg)
}

// updateNav forwards navigation to the focused table and re-syncs the
// tracked identity; unknown keys reach the table and are ignored there.
func (s *Sessions) updateNav(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	table := s.list
	if s.pane == paneHistory {
		table = s.history
	}
	next, cmd := table.Update(msg)
	if s.pane == paneHistory {
		s.history = next
		s.syncTxID()
	} else {
		s.list = next
		s.syncSelID()
	}

	return s, cmd
}
