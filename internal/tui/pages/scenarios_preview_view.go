// scenarios_preview_view.go renders the §F step message-preview overlay
// (UAT round 9 F-9e c): the page title pinned above a scrollable window
// of the preview body, with the reconstructed request/response sections
// in the §I review shape. The overlay's keyboard and scroll maths live
// in scenarios_preview.go; the panes live in scenarios_view.go.
package pages

import (
	"strconv"
	"strings"

	"jiso/internal/tui/theme"
)

// The step message-preview overlay's non-payload lines (UAT round 9
// F-9e c). One line each, symbol+word for the loading marker (the
// pendingGlyph degrades to ".." under the ascii theme, so goldens stay
// 7-bit).
const (
	scenPreviewLoadingWord    = "loading"
	scenPreviewEmptyText      = "run the scenario to capture the message"
	scenPreviewNoResponseText = "no response - run the scenario"
)

// renderStepPreview draws the message-preview overlay: the page title
// pinned above a WINDOW of the preview body starting at
// s.stepPreviewScroll (the §I renderReview contract — the scroll is
// clamped here against the live content, so a stale offset after a
// resize still renders sanely). The panes are not drawn this frame and
// publish no section rects.
func (s *Scenarios) renderStepPreview(title string, h, w int) string {
	body := strings.Split(strings.TrimRight(s.stepPreviewBody(), "\n"), "\n")
	avail := max(h-stepPreviewHeadH, 1)
	top := min(max(s.stepPreviewScroll, 0), max(len(body)-avail, 0))
	window := strings.Join(body[top:min(top+avail, len(body))], "\n")

	return clipBlockStyled(s.th, title+"\n"+window, h, w)
}

// stepPreviewBody renders the step message preview: a headline (step
// number + scenario), the reconstructed REQUEST — and RESPONSE when
// root captured one — in the same §C-style sections as the §I review,
// closed by the esc hint (Esc owns the keyboard first). An in-flight
// load shows the loading marker and a step that captured nothing shows
// the run hint; the page never invents a message.
func (s *Scenarios) stepPreviewBody() string {
	p := s.state.Preview

	var b strings.Builder

	if p == nil {
		// Defensive: the overlay arms from a pushed Preview, so this is
		// only reachable if root clears the payload mid-flight.
		b.WriteString(s.th.TextMuted.Render(s.pendingGlyph()+" "+scenPreviewLoadingWord) + "\n")
		b.WriteString(s.previewHintLine())

		return b.String()
	}

	head := plainDecor(s.th, "STEP "+strconv.Itoa(p.StepIndex)+" · "+p.ScenarioID)
	b.WriteString(s.th.TextPrimary.Render(head) + "\n")

	switch {
	case p.Request == nil && p.Response == nil && p.Loading:
		b.WriteString(s.th.TextMuted.Render(s.pendingGlyph()+" "+scenPreviewLoadingWord) + "\n")
	case p.Request == nil && p.Response == nil:
		if p.Note != "" {
			// A folded load failure names its cause (task 9.8b); the note is
			// root-stamped text, the page adds only the error styling.
			b.WriteString(s.th.Status(theme.KindError, p.Note) + "\n")
		} else {
			b.WriteString(s.th.TextMuted.Render(scenPreviewEmptyText) + "\n")
		}
	default:
		b.WriteString(s.stepPreviewMessage("REQUEST", p.Request))
		if p.Response != nil {
			b.WriteString(s.stepPreviewMessage("RESPONSE", p.Response))
		} else {
			// The missing half is named, not invented (task 9.8b): the
			// pending template composition and a run step that captured no
			// reply both show the title plus an honest cause line.
			b.WriteString(titleLine(s.th, "RESPONSE") + "\n" +
				s.th.TextMuted.Render(scenPreviewNoResponseText) + "\n")
		}
	}
	b.WriteString(s.previewHintLine())

	return b.String()
}

// stepPreviewMessage renders one reconstructed message section (the §I
// reviewMessage shape: packed HEX block, then the parsed FIELDS block;
// raw fallbacks and parse errors are surfaced, never hidden).
func (s *Scenarios) stepPreviewMessage(title string, m *TxReviewMessage) string {
	if m == nil {
		return titleLine(s.th, title) + "\n" +
			s.th.TextMuted.Render(dashIf(s.th, "(no message data available)")) + "\n"
	}

	var b strings.Builder

	b.WriteString(titleLine(s.th, title+" HEX") + "\n")
	b.WriteString(s.th.Dim.Render(plainBlock(m.HEX)) + "\n")
	b.WriteString(titleLine(s.th, title+" FIELDS") + "\n")
	if m.RawFallback {
		b.WriteString(s.th.Status(theme.KindWarn, "raw hex fallback") + "\n")
	}
	if m.ParseError != "" {
		b.WriteString(s.th.Status(theme.KindError, "parse: "+m.ParseError) + "\n")
	}
	b.WriteString(s.th.TextPrimary.Render(plainBlock(m.Describe)) + "\n")

	return b.String()
}

// previewHintLine closes the overlay body with the scroll hint (the §I
// review hint; the separator follows the theme's glyph mode).
func (s *Scenarios) previewHintLine() string {
	sep := s.th.Separator()

	return s.th.Deemphasized.Render("j" + sep + "k scroll" + sep + "esc close")
}
