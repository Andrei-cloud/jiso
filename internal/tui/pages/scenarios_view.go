// scenarios_view.go renders the §F body: a title row (SCENARIOS (N),
// live filter text, report path) above a master-detail split — the
// scenario list box and the STEPS box side by side at ≥ frame.FullWidth,
// the steps pane below the list below it (responsive contract). The
// frame owns the surrounding chrome; sizing comes from frame.ContentSize
// (dashboard layout.go pattern).
package pages

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

const (
	titleScenarios = "SCENARIOS"
	titleSteps     = "STEPS"

	// scenRunningWord accompanies the running glyph (symbol+word,
	// never color alone). The wireframe's ⏳ is an emoji; the design
	// contract forbids emoji chips, so running uses the theme ok/error
	// glyph family's neutral marker: "…" (".." ascii) + "running".
	scenRunningWord = "running"
	scenPendingDot  = "…"
	scenASCIIDot    = ".."

	// scenMinListWidth is the List's floor width before the first size
	// msg; scenListFraction sizes the master column at split widths.
	scenMinListWidth = 24
	scenListFraction = 3 // list column = content width / 3, clamped

	// scenSectionGap is the blank row/column between the panes.
	scenSectionGap = 1
)

// View renders the §F body for the frame's content area.
func (s *Scenarios) View() tea.View {
	w, h := frame.ContentSize(s.width, s.height)

	return tea.NewView(s.render(w, h))
}

// render lays out title row + panes + banner line, clipped to exactly h
// lines of at most w cells.
func (s *Scenarios) render(w, h int) string {
	s.sections = s.sections[:0] // redraw the section rects alongside the ink

	title := s.titleRow(w)

	if len(s.state.Scenarios) == 0 {
		return clipBlockStyled(s.th, title+"\n"+s.emptyStateBody(), h, w)
	}

	footer := s.bannerLine()
	errStrip := s.errorStrip(w)
	head := title + "\n"
	errLines := 0
	if errStrip != "" {
		errLines = strings.Count(errStrip, "\n") + 1
		head += errStrip + "\n"
	}
	paneH := max(h-2-errLines, 4) // title + error strip + banner rows

	if w >= frame.FullWidth {
		listW := min(max(w/scenListFraction, scenMinListWidth+2), 44)
		stepsW := w - listW - scenSectionGap
		panesY := 1 + errLines // the title row, plus the error strip if any

		// The border-only list pane draws two cells narrower than its
		// nominal w, so the STEPS origin advances by the DRAWN list
		// width, not the nominal column width.
		listSec := s.listBox(listW, paneH)
		stepsSec := s.stepsBox(lipgloss.Width(listSec)+scenSectionGap, panesY, stepsW, paneH)

		body := lipgloss.JoinHorizontal(lipgloss.Top,
			listSec, strings.Repeat(" ", scenSectionGap), stepsSec)

		return clipBlockStyled(s.th, head+body+"\n"+footer, h, w)
	}

	lowerH := max(paneH/2, 3)
	upperH := paneH - lowerH
	panesY := 1 + errLines

	// Stacked: the list section's "\n" terminator costs no line of its
	// own; STEPS starts where the drawn list lines end.
	listSec := s.listBox(w, upperH)
	stepsSec := s.stepsBox(0, panesY+lipgloss.Height(listSec), w, lowerH)

	return clipBlockStyled(s.th, head+listSec+"\n"+stepsSec+"\n"+footer, h, w)
}

// scenErrMaxLines caps the dedicated error strip: two wrapped lines
// carry a full engine error at typical widths without eating the STEPS
// pane's height.
const scenErrMaxLines = 2

// errorStrip is the dedicated error area (UAT round 5): the FIRST
// failed step's full error, word-wrapped into at most scenErrMaxLines
// lines with the error status style. Before it existed, the only place
// the error appeared was the clipped sub-line under the step, where a
// long pack error read as an unreadable fragment. Empty (and
// zero-height) when no step failed, so passing frames render
// byte-identical to before.
func (s *Scenarios) errorStrip(w int) string {
	text := ""
	for _, st := range s.state.SelectedSteps {
		if st.Status == StepFail && st.Note != "" {
			text = plainDecor(s.th, st.Note)

			break
		}
	}
	if text == "" {
		return ""
	}

	return s.wrapErrorLines("error: "+text, max(w-1, 8))
}

// wrapErrorLines word-wraps text into at most maxLines lines of w
// cells, error-styled (symbol+word on the first line only, never color
// alone); the last kept line clips with the ellipsis so overflow is
// honest.
func (s *Scenarios) wrapErrorLines(text string, w int) string {
	words := strings.Fields(text)
	var lines []string
	cur := ""
	for _, word := range words {
		switch {
		case cur == "":
			cur = word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= w:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) > scenErrMaxLines {
		last := clipCells(lines[scenErrMaxLines-1], w, clipTail(s.th))
		lines = append(lines[:scenErrMaxLines-1], last)
	}

	styled := make([]string, 0, len(lines))
	for i, l := range lines {
		if i == 0 {
			styled = append(styled, s.th.Status(theme.KindError, l))

			continue
		}
		styled = append(styled, s.th.StatusError.Render(l))
	}

	return strings.Join(styled, "\n")
}

// titleRow renders "SCENARIOS (4)  filter: ▏  report: scenario-report.json"
// clipped to the content width (the report segment shows the export
// destination root pushed; dash = not derived yet).
func (s *Scenarios) titleRow(w int) string {
	filter := dashIf(s.th, s.filter)
	if s.filtering {
		filter += cursorGlyph(s.th)
	}

	line := strings.Join([]string{
		titleLine(s.th, titleScenarios+" ("+strconv.Itoa(len(s.state.Scenarios))+")"),
		s.th.Deemphasized.Render("filter:") + " " + s.th.TextPrimary.Render(filter),
		s.th.Deemphasized.Render("report:") + " " + s.th.TextPrimary.Render(dashIf(s.th, s.state.ReportPath)),
	}, "  ")

	return clipCells(line, w, clipTail(s.th))
}

// bannerLine is the bottom line: the final banner (Summary) and, once
// `e` ran, the export status line beside it (toast-less feedback —
// TUI-406b will add real toasts); with no banner yet, the live running
// hint. One line, symbol+word throughout.
func (s *Scenarios) bannerLine() string {
	base, tail := plainDecor(s.th, s.state.Summary), plainDecor(s.th, s.state.StatusLine)
	w := s.bannerWidth()

	switch {
	case base != "" && tail != "":
		return clipCells(s.th.TextPrimary.Render(base)+"  "+s.th.TextMuted.Render(tail), w, clipTail(s.th))
	case base != "":
		return clipCells(s.th.TextPrimary.Render(base), w, clipTail(s.th))
	case tail != "":
		return clipCells(s.th.TextMuted.Render(tail), w, clipTail(s.th))
	case s.state.Running:
		return clipCells(s.th.TextMuted.Render(s.pendingGlyph()+" "+scenRunningWord), w, clipTail(s.th))
	default:
		return ""
	}
}

// bannerWidth is the content width for clipping the banner (falls back
// to the pre-first-resize floor).
func (s *Scenarios) bannerWidth() int {
	w, _ := frame.ContentSize(s.width, s.height)

	return w
}

// listBox renders the master pane: the widgets.List sized into the box.
// The pane carries no own title (the page title row is its title), so the
// box fills the full pane height; its frame comes from the shared
// widgets.Border accessor (a title-less section).
func (s *Scenarios) listBox(w, h int) string {
	inner := max(h-2, 1)
	// The box's total width is w-2 (one gap column), so its content is
	// w-4; the widget must render to that, not past the border.
	s.list.SetSize(max(w-4, 2), inner)

	return widgets.Border(s.th, false).Width(max(w-2, 1)).Height(inner).Render(s.list.View())
}

// stepsBox renders the detail pane: step rows plus sub-lines, or the
// "select a scenario" hint when root pushed no rows, drawn through the
// one shared widgets.Section (ModeServer: the box is two cells narrower
// than w). The step rows arrive pre-clipped to the box's CONTENT width,
// so the section's own clip is a no-op and the bytes cannot move; the
// section's Rect is recorded at its content-relative origin.
func (s *Scenarios) stepsBox(x, y, w, h int) string {
	// Content width: the box is w-2 wide including its two border
	// columns, so the body gets w-4.
	inner := s.stepsBody(max(w-4, 1), max(h-3, 1))

	sec := widgets.NewSection(s.th, titleSteps)
	sec.Mode = widgets.ModeServer
	out, _ := sec.Render(inner, x, y, w, h)
	s.sections = append(s.sections, sectionRect(x, y, out))

	return out
}

// stepsBody renders one line per step with an optional indented sub-line
// (extract/validate notes, validation diff). Rows beyond the visible
// window are dropped (truncate-never-wrap; the list owns scrolling, the
// step pane shows what fits).
func (s *Scenarios) stepsBody(w, h int) string {
	if len(s.state.SelectedSteps) == 0 {
		return s.th.TextMuted.Render("select a scenario - enter runs it")
	}

	lines := make([]string, 0, h)

	for _, st := range s.state.SelectedSteps {
		if len(lines) >= h {
			break
		}
		lines = append(lines, clipCells(s.stepLine(st, w), w, clipTail(s.th)))
		if st.Note == "" {
			continue
		}
		if len(lines) >= h {
			break
		}
		lines = append(lines, clipCells(s.stepNoteLine(st, w), w, clipTail(s.th)))
	}

	for len(lines) < h {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// stepLine renders " 1  Purchase Authorization  0200 → RC 00 ✓ 3ms" (or
// the running/pending variants; the arrow + dash keep unknowns honest).
func (s *Scenarios) stepLine(st StepRow, w int) string {
	idx := padLeft(strconv.Itoa(st.Index), 2)
	budget := stepNameBudget(w)
	name := padTo(clipCells(st.Name, budget, clipTail(s.th)), budget)
	mti := padTo(dashIf(s.th, st.MTI), 4)

	tail := s.stepTail(st)

	return s.th.TextMuted.Render(" "+idx+"  ") + name + " " +
		s.th.Deemphasized.Render(mti) + " " +
		s.th.Deemphasized.Render(scenArrow(s.th)) + " " + tail
}

// scenArrow is the step-row separator for th's glyph mode (the wireframe
// arrow; ASCII keeps goldens 7-bit).
func scenArrow(th *theme.Theme) string {
	if th.ASCII {
		return "->"
	}

	return "→"
}

// plainDecor degrades the decorative separators root builds into state
// strings (the "·" segment join, the "→" in export feedback, "…"
// ellipses, em dashes, the "±" latency jitter) to ASCII equivalents, so
// ascii-mode goldens stay 7-bit while root keeps emitting the wireframe
// glyphs. Shared by the §F banner/notes and the §G latency cells.
func plainDecor(th *theme.Theme, s string) string {
	if !th.ASCII {
		return s
	}
	s = strings.ReplaceAll(s, "·", "-")
	s = strings.ReplaceAll(s, "→", "->")
	s = strings.ReplaceAll(s, "—", "-")
	s = strings.ReplaceAll(s, "±", "~")

	return s
}

// stepTail renders the "RC 00 ✓ 3ms" / ".. running" / dash segment: the
// status symbol trails the values (wireframe order); the symbol+word
// pairing lives in the glyphs and the running word, never color alone.
func (s *Scenarios) stepTail(st StepRow) string {
	switch st.Status {
	case StepRunning:
		return s.th.TextPrimary.Render(s.pendingGlyph() + " " + scenRunningWord + clipTail(s.th))
	case StepPending:
		return s.th.Deemphasized.Render(dashIf(s.th, ""))
	case StepPass:
		tail := "RC " + dashIf(s.th, st.RC)
		if d := formatStepDuration(st.Latency); d != "" {
			tail += " " + d
		}

		return s.th.TextPrimary.Render(tail) + " " + s.th.StatusOK.Render(s.th.Symbol(theme.KindOK))
	default: // StepFail
		tail := dashIf(s.th, st.RC)
		if d := formatStepDuration(st.Latency); d != "" {
			tail += " " + d
		}

		return s.th.TextPrimary.Render(tail) + " " + s.th.StatusError.Render(s.th.Symbol(theme.KindError))
	}
}

// stepNoteLine renders the indented sub-line: pass notes stay muted
// with the ok symbol appended (extract/validate summary), fail notes get
// the error status style (validation diff `39 expect "00" got "96"`).
func (s *Scenarios) stepNoteLine(st StepRow, w int) string {
	const indent = "      "

	note := plainDecor(s.th, st.Note)
	switch st.Status {
	case StepFail:
		return clipCells(indent+s.th.Status(theme.KindError, note), w, clipTail(s.th))
	case StepPass:
		return clipCells(indent+s.th.TextMuted.Render(note+" ")+
			s.th.StatusOK.Render(s.th.Symbol(theme.KindOK)), w, clipTail(s.th))
	default:
		return clipCells(indent+s.th.TextMuted.Render(note), w, clipTail(s.th))
	}
}

// pendingGlyph is the running marker for th's glyph mode (the wireframe
// ⏳ is an emoji; the contract's symbol+word pair uses the neutral dot).
func (s *Scenarios) pendingGlyph() string {
	if s.th.ASCII {
		return scenASCIIDot
	}

	return scenPendingDot
}

// emptyStateBody renders "no scenarios — load via :" with the palette
// key accent-styled (transactions empty-state pattern; the dash follows
// the theme's ASCII mode). The CLI prints "No scenarios defined in the
// configuration file"; the TUI follows the wireframe wording.
func (s *Scenarios) emptyStateBody() string {
	return s.th.TextMuted.Render("no scenarios "+dashIf(s.th, "")+" load via ") + s.th.Accent.Render(":")
}

// stepNameBudget is the step-row name column width for a pane of total
// width w (the " 1  " prefix, the MTI cell, the arrow, and the
// "RC 00 ✓ 3ms" tail keep a fixed reserve).
func stepNameBudget(w int) int {
	return max(w-29, 8)
}

// padLeft left-pads s with spaces to width n (already-wide s is kept).
func padLeft(s string, n int) string {
	if len(s) >= n {
		return s
	}

	return strings.Repeat(" ", n-len(s)) + s
}

// padTo right-pads s with spaces to width n (already-wide s is kept —
// unlike connect_view.go's padRight, which always adds one space).
func padTo(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}

	return s
}

// formatStepDuration is the step-row time cell ("3ms", "1.2s"; "" for
// the not-yet-run zero so unknowns render as nothing, not zeros).
func formatStepDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < time.Second:
		return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
	case d < time.Minute:
		return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + "s"
	default:
		return strconv.FormatInt(int64(d.Minutes()), 10) + "m"
	}
}
