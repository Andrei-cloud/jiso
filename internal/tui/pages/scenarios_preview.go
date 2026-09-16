// scenarios_preview.go is the §F step message preview (UAT round 9
// F-9e c): the Enter-on-step trigger, the overlay's keyboard, and its
// scroll maths. The page never loads anything — Enter emits
// ScenarioStepDetailMsg and root pushes ScenariosState.Preview back;
// SetState arms the overlay when that Preview's identity changes (the
// §I sessions review pattern, sessions_keys.go / sessions.go).
package pages

import (
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
)

// StepCursor reports the STEPS-pane cursor index (0 when the stream is
// empty; tests; the §I ListCursor/HistoryCursor accessor pattern).
func (s *Scenarios) StepCursor() int { return s.stepCursor }

// StepPreviewOpen reports the message-preview overlay state (tests; the
// §I ReviewOpen accessor).
func (s *Scenarios) StepPreviewOpen() bool { return s.stepPreviewOpen }

// stepDetail yields the detail request for the step under the STEPS
// cursor (Enter on the steps pane, the Task 9.7 Minor: it must NOT run
// the list scenario). An empty stream yields nothing; the overlay opens
// later from the pushed Preview, never optimistically here (the §I
// handleSessionsReview doctrine: the overlay opens when the result
// arrives).
func (s *Scenarios) stepDetail() (Page, tea.Cmd) {
	steps := s.state.SelectedSteps
	if len(steps) == 0 {
		return s, nil
	}
	st := steps[min(s.stepCursor, len(steps)-1)]
	id := s.selectedID

	return s, func() tea.Msg { return ScenarioStepDetailMsg{StepIndex: st.Index, ScenarioID: id} }
}

// updateStepPreview is the preview overlay's keyboard: Esc closes
// first, then j/k (and arrows) scroll one line and pgup/pgdn page (the
// finding-8 scrollable-overlay doctrine: an overlay taller than the
// window must stay reachable, esc hint included). Unknown keys are
// swallowed; the overlay owns the keyboard.
func (s *Scenarios) updateStepPreview(msg tea.KeyPressMsg) (Page, tea.Cmd) {
	switch {
	case key.Matches(msg, s.nav.Cancel):
		s.stepPreviewOpen = false
	case key.Matches(msg, s.nav.Up):
		s.scrollStepPreviewBy(-1)
	case key.Matches(msg, s.nav.Down):
		s.scrollStepPreviewBy(1)
	case key.Matches(msg, s.nav.PageUp):
		s.scrollStepPreviewBy(-s.stepPreviewPageStep())
	case key.Matches(msg, s.nav.PageDown):
		s.scrollStepPreviewBy(s.stepPreviewPageStep())
	}

	return s, nil
}

// scrollStepPreviewBy moves the overlay's top line, clamped to the
// scroll extent (the view clamps again against the live content, so a
// stale scroll after a resize still renders sanely).
func (s *Scenarios) scrollStepPreviewBy(delta int) {
	s.stepPreviewScroll = max(s.stepPreviewScroll+delta, 0)
	if ext := s.stepPreviewScrollExtent(); ext > 0 && s.stepPreviewScroll > ext {
		s.stepPreviewScroll = ext
	}
}

// stepPreviewPageStep is the overlay viewport's visible line count, the
// page size for pgup/pgdn.
func (s *Scenarios) stepPreviewPageStep() int {
	return max(s.stepPreviewViewport(), 1)
}

// stepPreviewViewport is the overlay body's visible line count (the
// frame content height minus the pinned title row).
func (s *Scenarios) stepPreviewViewport() int {
	_, h := frame.ContentSize(s.width, s.height)

	return max(h-stepPreviewHeadH, 1)
}

// stepPreviewScrollExtent is the maximum top-line offset: total overlay
// lines (title row + body) minus the content height, 0 when everything
// fits.
func (s *Scenarios) stepPreviewScrollExtent() int {
	total := len(strings.Split(strings.TrimRight(s.stepPreviewBody(), "\n"), "\n"))

	_, h := frame.ContentSize(s.width, s.height)

	return max(total+stepPreviewHeadH-h, 0)
}

// stepPreviewHeadH is the number of pinned head lines the overlay
// render puts above the scrolling body (the page title row).
const stepPreviewHeadH = 1
