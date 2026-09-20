// send_view.go renders the §D body: title row (tx, target, segmented
// stage indicator, hex marker), the request/response panes (side by side
// at >= frame.FullWidth, stacked below), the elapsed/validated/correlation
// status line (or the TIMEOUT banner), and the RC badge. Sizing delegates
// to frame.ContentSize (dashboard layout.go pattern); every block is
// clipped to the content area and 7-bit clean under theme.ASCII.
package pages

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// titleSend is the §D section title.
const titleSend = "SEND"

// sendWaiting is the in-flight response-pane header text (the ellipsis is
// the theme's clipTail so ascii stays 7-bit).
const sendWaiting = "waiting for response"

// sendPendingGlyph marks not-yet-resolved stages; ascii uses "..".
const (
	sendPendingGlyph = "…"
	asciiPending     = ".."
)

// View renders the §D body for the frame's content area.
func (s *Send) View() tea.View {
	w, h := frame.ContentSize(s.width, s.height)

	return tea.NewView(s.render(w, h))
}

// render lays out title + indicator + panes + status + badge, clipped to
// exactly h lines of at most w cells.
func (s *Send) render(w, h int) string {
	s.sections = s.sections[:0] // redraw the section rects alongside the ink

	st := s.state

	inline := w >= frame.FullWidth
	stage := s.stageIndicator()
	hexMark := ""
	if st.HexOn {
		hexMark = "  " + s.th.Dim.Render("[hex on]")
	}
	titleText := titleLine(s.th, titleSend) + s.th.Dim.Render(" "+longDash(s.th)+" ") +
		s.th.TextPrimary.Render(dashIf(s.th, st.TxName)) +
		s.th.Dim.Render(" "+longDash(s.th)+" target ") + s.th.TextPrimary.Render(dashIf(s.th, st.Target)) +
		hexMark
	title := clipCells(titleText, w, clipTail(s.th))
	if inline {
		title = clipCells(titleText+"  "+stage, w, clipTail(s.th))
	} else {
		title += "\n" + clipCells(stage, w, clipTail(s.th))
	}

	status := s.statusLine()
	badge := s.rcBadge()

	below := 3 // title + status + badge
	if !inline {
		below++ // stage indicator owns its line
	}
	panesH := max(h-below, 5)

	var panes string
	reqTitle := s.paneTitle(true)
	respTitle := s.paneTitle(false)
	panesY := 1 // the title line above the panes is one line inline…
	if !inline {
		panesY = 2 // …and two once the stage indicator owns its own line
	}
	if w >= frame.FullWidth && !s.hexStacks() {
		colW := (w - 1) / 2
		panes = s.panesSideBySide(colW, panesY, panesH, reqTitle, respTitle)
	} else {
		panes = s.panesStacked(w, panesY, panesH, reqTitle, respTitle)
	}

	return clipBlockStyled(s.th, strings.Join([]string{title, panes, status, badge}, "\n"), h, w)
}

// hexStacks reports whether the h toggle shows at least one hex pane
// with content. The standard hexdump needs the full content width for
// its ASCII gutter, so those panes stack at any width instead of going
// side by side.
func (s *Send) hexStacks() bool {
	return s.state.HexOn && (len(s.state.RequestHex) > 0 || len(s.state.ResponseHex) > 0)
}

// stageIndicator is the segmented Connect ▸ Send ▸ Receive ▸ Parse ▸
// Validate indicator: ✓/✗ per resolved stage (symbol+text, never color
// alone), a lit pending marker on the running stage, dim ".." for stages
// that never fired.
func (s *Send) stageIndicator() string {
	sep := s.th.Dim.Render(" " + stageSep(s.th) + " ")

	parts := make([]string, 0, SendStageCount)
	for i, name := range SendStageNames {
		switch {
		case i < len(s.state.StageOK):
			st := s.th.StatusOK
			glyph := s.th.Symbol(theme.KindOK)
			if !s.state.StageOK[i] {
				st = s.th.StatusError
				glyph = s.th.Symbol(theme.KindError)
			}
			parts = append(parts, s.th.TextPrimary.Render(name)+" "+st.Render(glyph))
		case i == len(s.state.StageOK) && !s.state.Done:
			parts = append(parts, s.th.TextPrimary.Render(name)+" "+
				s.th.Accent.Render(sendPending(s.th)))
		default:
			parts = append(parts, s.th.Dim.Render(name+" "+sendPending(s.th)))
		}
	}

	return strings.Join(parts, sep)
}

// longDash is the title's name/target separator ("——", "--" under
// theme.ASCII; ascii goldens must stay 7-bit).
func longDash(th *theme.Theme) string {
	if th.ASCII {
		return "--"
	}

	return "——"
}

// stageSep is the indicator separator ("▸", ">" under theme.ASCII).
func stageSep(th *theme.Theme) string {
	if th.ASCII {
		return ">"
	}

	return "▸"
}

// sendPending is the pending-stage glyph for th's glyph mode.
func sendPending(th *theme.Theme) string {
	if th.ASCII {
		return asciiPending
	}

	return sendPendingGlyph
}

// paneTitle is one pane's title line: "REQUEST 0200" / "RESPONSE 0210",
// the in-flight waiting line in the response header ("waiting for
// response… 2.1s / budget 5s"), or the pane name alone while empty.
func (s *Send) paneTitle(request bool) string {
	rows := s.state.Response
	name := "RESPONSE"
	if request {
		rows, name = s.state.Request, "REQUEST"
	}
	mti := ""
	for _, r := range rows {
		if r.Num == "0" {
			mti = r.Display

			break
		}
	}

	if !request && !s.state.Done {
		wait := sendWaiting + sendPending(s.th) + " " + formatSendElapsed(s.state.Elapsed) +
			" / budget " + formatSendBudget(s.state.Budget)

		return titleLine(s.th, name) + "  " + s.th.Dim.Render(wait)
	}
	if mti == "" {
		mti = dashIf(s.th, "")
	}

	return titleLine(s.th, name) + "  " + s.th.Deemphasized.Render(mti)
}

// panesSideBySide joins the two panes horizontally with a one-column gap.
// The RESPONSE pane's origin advances by the REQUEST pane's drawn width
// (a ModeServer pane draws two cells narrower than its nominal w), not
// by the nominal column width.
func (s *Send) panesSideBySide(colW, y, h int, reqTitle, respTitle string) string {
	reqSec := s.pane(reqTitle, s.state.Request, 0, y, colW, h, true)
	respSec := s.pane(respTitle, s.state.Response, lipgloss.Width(reqSec)+1, y, colW, h, false)

	return lipgloss.JoinHorizontal(lipgloss.Top, reqSec, " ", respSec)
}

// panesStacked splits the vertical budget between the two panes; the
// second pane's origin chains off the first pane's measured height.
func (s *Send) panesStacked(w, y, h int, reqTitle, respTitle string) string {
	top := (h - 1) / 2
	reqSec := s.pane(reqTitle, s.state.Request, 0, y, w, top, true)
	respSec := s.pane(respTitle, s.state.Response, 0, y+lipgloss.Height(reqSec), w, h-top, false)

	return strings.Join([]string{reqSec, respSec}, "\n")
}

// pane renders title + a bordered box totaling exactly h lines (title 1 +
// inner h-3 + borders 2) through the one shared widgets.Section in
// ModeServer, with one line per row in the Display or Hex column (the
// `h` toggle is pure display over identical rows). The rows/dump bodies
// arrive pre-clipped to the box's CONTENT width, so the section's own
// clip is a no-op and the bytes cannot move.
func (s *Send) pane(title string, rows []ExchangeRow, x, y, w, h int, request bool) string {
	inner := max(h-3, 1)
	contentW := max(max(w-2, 1)-2, 1)
	// Record the truthful inner height (scroll clamps and page steps
	// derive from it) and the pane's own scroll offset.
	if request {
		s.paneInnerReq = inner
	} else {
		s.paneInnerResp = inner
	}
	off := min(s.curScroll(request), max(s.contentLen(request)-inner, 0))
	// UAT: the h toggle switches the WHOLE pane from the Describe rows
	// to the standard hexdump of the packed message (and back).
	if s.state.HexOn {
		dump := s.state.ResponseHex
		if request {
			dump = s.state.RequestHex
		}
		if len(dump) > 0 {
			return s.sectionBox(title+s.scrollMark(off, len(dump), inner, request), s.dumpBody(dump, inner, contentW, off), x, y, w, h)
		}
	}

	return s.sectionBox(title+s.scrollMark(off, len(rows), inner, request), s.rowsBody(rows, inner, contentW, request, off), x, y, w, h)
}

// scrollMark marks a pane that is scrolled or scrollable: ▴ once lines
// are above the window, ▾ while lines remain below (ascii ^/v); the
// keyboard-focused pane shows its line position, the other its arrows
// alone (dim), so both states read at a glance.
func (s *Send) scrollMark(off, content, inner int, request bool) string {
	if content <= inner {
		return ""
	}
	up, down := "▴", "▾"
	if s.th.ASCII {
		up, down = "^", "v"
	}
	marks := ""
	if off > 0 {
		marks += up
	}
	if off+inner < content {
		marks += down
	}
	if marks == "" {
		return ""
	}
	focused := (request && s.focusedIsRequest()) || (!request && !s.focusedIsRequest())
	if focused && off > 0 {
		return " " + s.th.Deemphasized.Render(fmt.Sprintf("%s %d/%d", marks, off+1, content))
	}

	return " " + s.th.Dim.Render(marks)
}

// sectionBox draws one pre-styled-title pane box with the shared
// Section (ModeServer: the box is two cells narrower than w) and
// records its Rect at the content-relative origin.
func (s *Send) sectionBox(title, body string, x, y, w, h int) string {
	sec := widgets.NewSection(s.th, title)
	sec.Mode = widgets.ModeServer
	sec.TitlePreStyled = true
	out, _ := sec.Render(body, x, y, w, h)
	s.sections = append(s.sections, sectionRect(x, y, out))

	return out
}

// dumpBody renders standard hexdump lines clipped to the pane: the
// 8-hex-digit offset deemphasized, bytes and ASCII gutter primary.
func (s *Send) dumpBody(dump []string, inner, maxW, off int) string {
	lines := make([]string, 0, len(dump))
	for _, ln := range dump {
		offset, rest, ok := strings.Cut(ln, "  ")
		styled := s.th.TextPrimary.Render(ln)
		if ok {
			styled = s.th.Deemphasized.Render(offset+"  ") + s.th.TextPrimary.Render(rest)
		}
		lines = append(lines, clipCells(styled, maxW, clipTail(s.th)))
	}
	if off > len(lines) {
		off = max(len(lines)-1, 0)
	}
	lines = lines[off:]
	for len(lines) < inner {
		lines = append(lines, "")
	}

	return strings.Join(lines[:inner], "\n")
}

// rowsBody renders the rows clipped to inner lines, one line per row.
// The correlation note is reserved its width before clipping so a
// clipped value never truncates mid-note.
func (s *Send) rowsBody(rows []ExchangeRow, inner, maxW int, request bool, off int) string {
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		note := s.noteSuffix(r)
		lines = append(lines, clipCells(s.rowBase(r), maxW-lipgloss.Width(note), clipTail(s.th))+note)
	}
	if len(rows) == 0 {
		placeholder := sendPending(s.th)
		if s.state.Done && !request {
			placeholder = "no response"
		}
		lines = append(lines, s.th.Dim.Render(placeholder))
	}
	if off > len(lines) {
		off = max(len(lines)-1, 0)
	}
	lines = lines[off:]
	for len(lines) < inner {
		lines = append(lines, "")
	}

	return strings.Join(lines[:inner], "\n")
}

// rowBase renders one pane line without the correlation note: the
// Describe text verbatim (label and dot padding deemphasized, value
// primary), or the legacy num/value pair for rows built without
// Describe text. The h toggle no longer rewrites rows: it switches the
// whole pane to dumpBody lines.
func (s *Send) rowBase(r ExchangeRow) string {
	if r.Text != "" {
		if head, value, ok := strings.Cut(r.Text, ": "); ok && r.Num != "" {
			return s.th.Deemphasized.Render(head+": ") + s.th.TextPrimary.Render(value)
		}

		return s.th.Deemphasized.Render(r.Text)
	}
	value := r.Display
	if s.state.HexOn {
		value = r.Hex
	}

	return s.th.Deemphasized.Render(fmt.Sprintf("%2s  ", r.Num)) + s.th.TextPrimary.Render(value)
}

// noteSuffix renders the correlation annotation: "(echo ✓)"/"(STAN ✗)"
// with the theme symbol, "(auth code)" muted.
func (s *Send) noteSuffix(r ExchangeRow) string {
	switch r.NoteKind {
	case NotePass:
		return s.th.Deemphasized.Render(" ("+r.Note+" ") +
			s.th.StatusOK.Render(s.th.Symbol(theme.KindOK)) + s.th.Deemphasized.Render(")")
	case NoteFail:
		return s.th.Deemphasized.Render(" ("+r.Note+" ") +
			s.th.StatusError.Render(s.th.Symbol(theme.KindError)) + s.th.Deemphasized.Render(")")
	case NoteInfo:
		return s.th.Dim.Render(" (" + r.Note + ")")
	}

	return ""
}

// statusLine is the completion line "elapsed 3.2ms · attempt 1 ·
// validated ✓ · correlation ✓", the "✗ TIMEOUT" banner (status.error),
// or the live elapsed/budget line while the op runs.
func (s *Send) statusLine() string {
	st := s.state
	switch {
	case st.Done && st.TimedOut:
		return s.th.Status(theme.KindError, "TIMEOUT") +
			s.th.Dim.Render(" "+midDot(s.th)+" elapsed "+formatSendElapsed(st.Elapsed))
	case st.Done:
		validated, corr := s.th.Symbol(theme.KindError), s.th.Symbol(theme.KindError)
		if st.Validated {
			validated = s.th.Symbol(theme.KindOK)
		}
		if st.CorrelationOK {
			corr = s.th.Symbol(theme.KindOK)
		}

		dot := midDot(s.th)
		attempt := ""
		if st.Attempt > 0 {
			attempt = "attempt " + strconv.Itoa(st.Attempt) + " " + dot + " "
		}

		return s.th.Dim.Render("elapsed "+formatSendElapsed(st.Elapsed)+" "+dot+" "+attempt+"validated ") +
			validated + s.th.Dim.Render(" "+dot+" correlation ") + corr
	default:
		return s.th.Dim.Render("elapsed " + formatSendElapsed(st.Elapsed) +
			" " + midDot(s.th) + " budget " + formatSendBudget(st.Budget))
	}
}

// rcBadge is the RC status line: "0210 · RC 00 APPROVED" on the ok style,
// "RC 96 DECLINED" on the error style, unknown codes code-only muted. It
// renders once the exchange is done and a response arrived.
func (s *Send) rcBadge() string {
	st := s.state
	if !st.Done || st.TimedOut || len(st.Response) == 0 {
		return ""
	}
	mti := ""
	for _, r := range st.Response {
		if r.Num == "0" {
			mti = r.Display

			break
		}
	}

	text := dashIf(s.th, mti) + " " + midDot(s.th) + " RC " + dashIf(s.th, st.RC)
	if st.RCLabel != "" {
		text += " " + st.RCLabel
	}
	switch {
	case st.RCok:
		return s.th.StatusOK.Render(text)
	case st.RCLabel != "":
		return s.th.StatusError.Render(text)
	default:
		return s.th.Deemphasized.Render(text)
	}
}

// formatSendElapsed renders the timer: "3.2ms" below a second, "2.1s"
// above (trailing ".0" trimmed, so a 5s budget reads "5s").
func formatSendElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}

	return formatSendBudget(d)
}

// formatSendBudget renders a whole-ish duration without trailing zeros
// ("5s", "2.5s").
func formatSendBudget(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := fmt.Sprintf("%.1f", d.Seconds())
	s = strings.TrimSuffix(s, ".0")

	return s + "s"
}
