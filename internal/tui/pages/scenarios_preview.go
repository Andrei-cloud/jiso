// scenarios_preview.go is the step message preview: the Enter-on-step
// trigger, the overlay's keyboard, and its scroll maths. The page never
// loads anything — Enter emits ScenarioStepDetailMsg and root pushes
// ScenariosState.Preview back, arming the overlay when its identity changes.
package pages

import (
	"strings"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
)

// StepCursor reports the STEPS-pane cursor index (0 when the stream is empty).
func (s *Scenarios) StepCursor() int { return s.stepCursor }

// StepPreviewOpen reports whether the message-preview overlay is open.
func (s *Scenarios) StepPreviewOpen() bool { return s.stepPreviewOpen }

// stepDetail yields the detail request for the step under the STEPS
// cursor; an empty stream yields nothing. The overlay opens later from
// the pushed Preview, never optimistically here.
func (s *Scenarios) stepDetail() (Page, tea.Cmd) {
	steps := s.state.SelectedSteps
	if len(steps) == 0 {
		return s, nil
	}
	st := steps[min(s.stepCursor, len(steps)-1)]
	id := s.selectedID

	return s, func() tea.Msg { return ScenarioStepDetailMsg{StepIndex: st.Index, ScenarioID: id} }
}

// updateStepPreview is the overlay's keyboard: Esc closes first, then
// j/k scroll one line and pgup/pgdn page; unknown keys are swallowed —
// the overlay owns the keyboard.
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

// scrollStepPreviewBy moves the overlay's top line within the scroll
// extent (the view clamps again against the live content).
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
