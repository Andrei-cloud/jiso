// scenarios_preview_view.go renders the step message-preview overlay:
// the page title pinned above a scrollable window of the preview body.
// The overlay's keyboard and scroll maths live in scenarios_preview.go.
package pages

import (
	"strconv"
	"strings"

	"jiso/internal/tui/theme"
)

// The overlay's fixed lines; the pendingGlyph degrades to ".." under the
// ascii theme, so goldens stay 7-bit.
const (
	scenPreviewLoadingWord    = "loading"
	scenPreviewEmptyText      = "run the scenario to capture the message"
	scenPreviewNoResponseText = "no response - run the scenario"
	// scenPreviewComposedText labels a REQUEST shown for a step that never
	// ran: an honest composition, not a capture. ASCII-only for 7-bit goldens.
	scenPreviewComposedText = "request composed from template - not sent yet"
)

// renderStepPreview draws the title pinned above a window of the preview
// body starting at stepPreviewScroll, clamped here against the live
// content; the panes publish no section rects this frame.
func (s *Scenarios) renderStepPreview(title string, h, w int) string {
	body := strings.Split(strings.TrimRight(s.stepPreviewBody(), "\n"), "\n")
	avail := max(h-stepPreviewHeadH, 1)
	top := min(max(s.stepPreviewScroll, 0), max(len(body)-avail, 0))
	window := strings.Join(body[top:min(top+avail, len(body))], "\n")

	return clipBlockStyled(s.th, title+"\n"+window, h, w)
}

// stepPreviewBody renders the preview headline, the reconstructed REQUEST
// and RESPONSE when captured, and the esc hint. An in-flight load shows
// the loading marker; a step that captured nothing shows the run hint —
// the page never invents a message.
func (s *Scenarios) stepPreviewBody() string {
	p := s.state.Preview

	var b strings.Builder

	if p == nil {
		// Defensive: reachable only if root clears the payload mid-flight.
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
			// A folded load failure names its cause: root-stamped text,
			// the page adds only the error styling.
			b.WriteString(s.th.Status(theme.KindError, p.Note) + "\n")
		} else {
			b.WriteString(s.th.TextMuted.Render(scenPreviewEmptyText) + "\n")
		}
	default:
		if p.Composed {
			// A pending step's REQUEST is a composition, not a capture:
			// the muted label sits above the REQUEST title.
			b.WriteString(s.th.TextMuted.Render(scenPreviewComposedText) + "\n")
		}
		b.WriteString(s.stepPreviewMessage("REQUEST", p.Request))
		if p.Response != nil {
			b.WriteString(s.stepPreviewMessage("RESPONSE", p.Response))
		} else {
			// The missing half is named, never invented.
			b.WriteString(titleLine(s.th, "RESPONSE") + "\n" +
				s.th.TextMuted.Render(scenPreviewNoResponseText) + "\n")
		}
	}
	b.WriteString(s.previewHintLine())

	return b.String()
}

// stepPreviewMessage renders one reconstructed message section: packed
// HEX block, then the parsed FIELDS block; raw fallbacks and parse errors
// are surfaced, never hidden.
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

// previewHintLine closes the overlay body with the scroll hint.
func (s *Scenarios) previewHintLine() string {
	sep := s.th.Separator()

	return s.th.Deemphasized.Render("j" + sep + "k scroll" + sep + "esc close")
}
