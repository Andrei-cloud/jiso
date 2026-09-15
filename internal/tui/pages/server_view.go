// server_view.go renders the §G body (wireframe §G): the status header
// row ("MOCK SERVER ● running :9999 (binary2) uptime 00:42:11" /
// "MOCK SERVER ○ stopped") above a STATS box and a ROUTES table — side
// by side at ≥ frame.FullWidth, stacked below it (the dashboard/scenarios
// responsive contract). While the server has never been snapshotted the
// stats box becomes the start-form hint (wireframe's bottom line); the
// Enter-on-route detail replaces the whole body. Sizing comes from
// frame.ContentSize; the frame owns the surrounding chrome.
package pages

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

const (
	titleServer = "MOCK SERVER"
	titleStats  = "STATS"
	titleRoutes = "ROUTES"

	// serverMinTableWidth is the routes Table's floor width.
	serverMinTableWidth = 40
	titleLog            = "SERVER LOG"

	// serverStatsBoxH is the natural height of the STATS/START box
	// (title + border + 5 body lines); the LOG box takes the rest of
	// the left column once the server has emitted anything.
	serverStatsBoxH  = 10
	serverLogMinBoxH = 4
	serverNarrowLogH = 6

	// serverStatsFraction sizes the STATS column at the two-column
	// (no-log) split: one third of the content width, floored only (UAT
	// round 8 finding 5: the old 26..40 clamp froze the split and left a
	// trailing gap). The three-column split sizes STATS at one fifth
	// (server3ColWidths).
	serverStatsFraction = 3
	serverStatsMin      = 26
	serverSectionGap    = 1
	// serverStatsLabelCol is the stats card's label column.
	serverStatsLabelCol = 10
)

// Running/stopped header dots (symbol+word, never color alone; ASCII
// fallbacks keep the ascii goldens 7-bit).
const (
	glyphDotOn  = "●"
	glyphDotOff = "○"
	asciiDotOn  = "*"
	asciiDotOff = "o"
)

// startHintLines is the wireframe's start-form line, shown in place of
// the stats card until a server has ever been snapshotted. The lines are
// pre-styled (body copy muted, the c/Enter hotkeys bold-accent per the
// UAT round-4 convention), so startHintBody must join them without
// re-styling the block.
func startHintLines(th *theme.Theme) []string {
	sep := th.Separator()

	return []string{
		th.TextMuted.Render("start form (when stopped):"),
		th.TextMuted.Render(plainDecor(th, "port · header · spec · routes file")),
		th.TextMuted.Render("remembered from your last start"),
		"",
		keyGlyph(th, "c") + th.TextMuted.Render(" configure"+sep) +
			keyGlyph(th, "Enter") + th.TextMuted.Render(" starts"),
	}
}

// serverColumns are the ROUTES columns; MATCH is the flex column that
// gives first on narrow terminals (the Table's fit clamp keeps every
// line inside the pane).
func serverColumns() []widgets.Column {
	return []widgets.Column{
		{Title: "MATCH", Width: 30, Flex: true},
		{Title: "RESP", Width: 6},
		// Hit counts: "812" over "6" only reads as a decrease when the units line up.
		{Title: "HITS", Width: 8, AlignRight: true},
	}
}

// View renders the §G body for the frame's content area.
func (s *Server) View() tea.View {
	w, h := frame.ContentSize(s.width, s.height)

	return tea.NewView(s.render(w, h))
}

// render lays out header row + error line + panes (or the detail
// overlay), clipped to exactly h lines of at most w cells.
func (s *Server) render(w, h int) string {
	s.sections = s.sections[:0] // redraw the section rects alongside the ink

	head := s.headerRow(w)
	if errLine := s.errorLine(w); errLine != "" {
		head += "\n" + errLine
	}
	headH := strings.Count(head, "\n") + 1

	if s.detailOpen && s.detailIdx >= 0 && s.detailIdx < len(s.state.Routes) {
		return clipBlockStyled(s.th, head+"\n"+s.detailBody(), h, w)
	}

	if s.state.Starting {
		return clipBlockStyled(s.th, head+"\n"+s.th.TextMuted.Render(s.dotOn()+" starting "+plainDecor(s.th, "…")), h, w)
	}

	paneH := max(h-1, 4)

	if w >= frame.FullWidth {
		if len(s.state.Log) == 0 {
			// Two columns by ratio: STATS keeps its third (floor only)
			// and ROUTES absorbs the remainder, so the join sums exactly
			// to the content width (UAT round 8 finding 5).
			statsW := max(w/serverStatsFraction, serverStatsMin)
			routesW := w - statsW - serverSectionGap

			// A sectionW box draws at its layout width, so the next
			// column's origin advances by the DRAWN width of the segment
			// the join consumes (measured, not nominal).
			leftSec := s.leftBox(headH, statsW, paneH)
			routesSec := s.routesBox(lipgloss.Width(leftSec)+serverSectionGap, headH, routesW, paneH)

			body := lipgloss.JoinHorizontal(lipgloss.Top,
				leftSec, strings.Repeat(" ", serverSectionGap), routesSec)

			return clipBlockStyled(s.th, head+"\n"+body, h, w)
		}

		// Proposal 05 §1: the LOG is the live signal and owns the big
		// right pane at full height; STATS and ROUTES keep their
		// natural (short) heights, top-aligned (JoinHorizontal pads).
		statsW, logW, routesW := server3ColWidths(w)
		statsH := min(serverStatsBoxH, paneH)
		routesH := min(paneH, max(len(s.state.Routes)+5, 6))

		leftSec := s.leftBox(headH, statsW, statsH)
		routesX := lipgloss.Width(leftSec) + serverSectionGap
		routesSec := s.routesBox(routesX, headH, routesW, routesH)
		logSec := s.logBox(routesX+lipgloss.Width(routesSec)+serverSectionGap, headH, logW, paneH)

		body := lipgloss.JoinHorizontal(lipgloss.Top,
			leftSec, strings.Repeat(" ", serverSectionGap),
			routesSec, strings.Repeat(" ", serverSectionGap),
			logSec)

		return clipBlockStyled(s.th, head+"\n"+body, h, w)
	}

	if len(s.state.Log) > 0 {
		return s.narrowStackedLogs(head, headH, w, h, paneH)
	}

	statsH := max(paneH/2, 6)
	routesH := max(paneH-statsH-serverSectionGap, 4)

	leftSec := s.leftBox(headH, w, statsH)
	routesSec := s.routesBox(0, headH+lipgloss.Height(leftSec), w, routesH)

	return clipBlockStyled(s.th, head+"\n"+leftSec+"\n"+routesSec, h, w)
}

// narrowStackedLogs renders the narrow with-log stack: STATS over LOG over
// ROUTES. Each section's "\n" terminator costs no line of its own, and a
// ModeServer box draws short of its nominal height, so the stacked origins
// chain off the measured section strings.
func (s *Server) narrowStackedLogs(head string, headH, w, h, paneH int) string {
	statsH := min(serverStatsBoxH, max(paneH-serverNarrowLogH-serverSectionGap*2, 6))
	logH := min(serverNarrowLogH+4, max(paneH-statsH-serverSectionGap*2, serverLogMinBoxH))
	routesH := max(paneH-statsH-logH-serverSectionGap*2, 4)

	leftSec := s.leftBox(headH, w, statsH)
	logSec := s.logBox(0, headH+lipgloss.Height(leftSec), w, logH)
	routesSec := s.routesBox(0, headH+lipgloss.Height(leftSec)+lipgloss.Height(logSec), w, routesH)

	return clipBlockStyled(s.th, head+"\n"+leftSec+"\n"+logSec+"\n"+routesSec, h, w)
}

// headerRow renders the §G status header: accent title plus the
// symbol+word state ("● running :9999 (binary2)" in the theme ok kind /
// "○ stopped" muted), with the root-stamped uptime while running.
func (s *Server) headerRow(w int) string {
	parts := []string{titleLine(s.th, titleServer)}

	switch {
	case s.state.Running:
		state := s.th.StatusOK.Render(s.dotOn() + " running")
		if p := dashIf(s.th, s.state.Port); p != "" {
			state += s.th.TextPrimary.Render(" :" + p)
		}
		if hd := dashIf(s.th, s.state.Header); hd != "" {
			state += s.th.Deemphasized.Render(" (" + hd + ")")
		}
		parts = append(parts, state)
		up := s.state.Uptime
		parts = append(parts, s.th.Deemphasized.Render("uptime "+FormatUptime(&up)))
	case s.state.Starting:
		parts = append(parts, s.th.Deemphasized.Render(s.dotOn()+" starting"))
	default:
		parts = append(parts, s.th.Deemphasized.Render(s.dotOff()+" stopped"))
	}

	return clipCells(strings.Join(parts, "  "), w, clipTail(s.th))
}

// errorLine is the last failed-operation line (stop failure), rendered
// with the theme error kind; "" when there is no error to show.
func (s *Server) errorLine(w int) string {
	if s.state.Error == "" {
		return ""
	}

	return clipCells(s.th.Status(theme.KindError, s.state.Error), w, clipTail(s.th))
}

// server3ColWidths sizes the proposal-05 three-column body by ratio of
// the content width (UAT round 8 finding 5: the old fixed 26-cell STATS
// column and the 36..64 ROUTES clamp left a trailing gap on every wide
// terminal): STATS keeps a fifth (floor 26), ROUTES keeps 28% (floor 36
// so match expressions stay readable) and the LOG — the live signal and
// always the widest column at full width — absorbs the remainder, so the
// three columns plus the two gaps sum exactly to w.
func server3ColWidths(w int) (statsW, logW, routesW int) {
	statsW = max(w/5, serverStatsMin)
	routesW = max(w*28/100, 36)
	logW = w - statsW - routesW - serverSectionGap*2

	return statsW, logW, routesW
}

// logFooterHint is the dim last line inside the LOG box (proposal 05
// §1: the pane self-documents its scroll keys).
func logFooterHint(th *theme.Theme) string {
	return pickGlyph(th,
		"newest at bottom · j/k scroll · end follows",
		"newest at bottom - j/k scroll - end follows")
}

// logBox renders the SERVER LOG window: the raw ring lines compacted by
// CompactServerLog, oldest at the top, honoring the page's logScroll
// offset (0 = following the newest) and ending with the scroll hint
// line at the bottom of the box.
func (s *Server) logBox(x, y, w, h int) string {
	inner := max(h-3, 1)
	total := len(s.state.Log)

	// The newest line sits just above the hint; logScroll counts lines
	// back from the newest and the window never scrolls past the oldest.
	avail := max(inner-1, 1)
	end := min(max(total-s.logScroll, avail), total)
	start := max(end-avail, 0)

	body := make([]string, 0, inner)
	for _, l := range s.state.Log[start:end] {
		body = append(body, clipCells(s.th.TextMuted.Render(CompactServerLog(s.th, l)), max(w-2, 8), clipTail(s.th)))
	}
	for len(body) < avail {
		body = append(body, "")
	}
	body = append(body, clipCells(s.th.Deemphasized.Render(logFooterHint(s.th)), max(w-2, 8), clipTail(s.th)))

	// The log pane is the default focus target (j/k scroll it until r
	// or Tab moves to ROUTES); UAT round 5 makes that visible.
	return s.sectionW(paneTitle(s.th, titleLog, !s.routesFocused), strings.Join(body, "\n"), x, y, w, h, !s.routesFocused)
}

// leftBox renders the STATS card (or the start-form hint before the
// first snapshot) as a titled bordered box through the shared Section;
// it is always the leftmost column, so only its top line varies.
func (s *Server) leftBox(y, w, h int) string {
	title := titleStats
	body := s.statsBody()
	if !s.state.StatsKnown {
		title = "START"
		body = s.startHintBody()
	}

	return s.sectionW(paneTitle(s.th, title, false), body, 0, y, w, h, false)
}

// statsBody renders the five wireframe lines; thousands separators via
// formatCount, the match percent root-derived (dash when unknown).
func (s *Server) statsBody() string {
	st := s.state.Stats
	// Two cells, not three: the suffix this took was retired when the compact card
	// form moved percent and drop_conn onto their own continuation lines.
	line := func(label, value string) string {
		return s.th.Deemphasized.Render(padRight(label, serverStatsLabelCol)) +
			s.th.TextPrimary.Render(value)
	}
	// Proposal 05 §1: the compact card form — percent and drop_conn get
	// their own indented continuation lines so the card fits the narrow
	// first column without wrapping.
	sub := func(text string) string {
		return "  " + s.th.Deemphasized.Render(text)
	}

	return strings.Join([]string{
		line("served", formatCount(int64(st.Served))),
		line("matched", formatCount(int64(st.Matched))),
		sub(dashIf(s.th, st.MatchPct)),
		line("fallback", formatCount(int64(st.Fallback))),
		sub("drop_conn " + formatCount(int64(st.Dropped))),
		line("req err", formatCount(int64(st.ReqErr))),
		line("live conns", formatCount(int64(st.LiveConns))),
	}, "\n")
}

// startHintBody renders the wireframe's start-form hint block: the
// lines arrive pre-styled from startHintLines, so they are only joined
// (an outer Render here would re-tint every segment and flatten the
// HotKey glyphs).
func (s *Server) startHintBody() string {
	return strings.Join(startHintLines(s.th), "\n")
}

// routesBox renders the titled ROUTES table box sized into the pane.
// The table is sized to the box's inner content width (the drawn box is
// w wide with two border cells, so a wider table word-wraps the
// right-aligned HITS column — UAT round 5; sessions listBox idiom).
func (s *Server) routesBox(x, y, w, h int) string {
	s.table.SetFocused(s.routesFocused)
	s.table.SetWidth(max(w-2, 4))

	return s.sectionW(paneTitle(s.th, titleRoutes, s.routesFocused), s.table.View(), x, y, w, h, s.routesFocused)
}

// sectionW draws a titled bordered box occupying exactly w×h in the page
// layout (h includes the title line) through the one shared
// widgets.Section in ModeServer: the shared box draws two cells narrower
// than the nominal width it is handed (its pinned ModeServer contract),
// so sectionW hands it w+2 and the DRAWN box lands its right border on
// the layout edge — the joins then sum exactly to the content width
// (UAT round 8 finding 5). The body clips to the box's CONTENT width
// (w-2). A focused pane's border takes the accent colour (UAT round 5:
// the server page had no focus indication at all); the rest keep the
// neutral border token. The section's Rect is recorded on the page at
// its content-relative origin, measured from the drawn string.
func (s *Server) sectionW(title, body string, x, y, w, h int, focused bool) string {
	sec := widgets.NewSection(s.th, title)
	sec.Mode = widgets.ModeServer
	sec.Focused = focused
	out, _ := sec.Render(body, x, y, w+2, h)
	s.sections = append(s.sections, sectionRect(x, y, out))

	return out
}

// detailBody renders the Enter-on-route detail view: every line is a
// muted label plus the root-derived value (match/echo/latency per the
// route config; empty sections render the dash).
func (s *Server) detailBody() string {
	d := s.state.Routes[s.detailIdx].Detail
	row := func(label, value string) string {
		return s.th.Deemphasized.Render(padRight(label, 12)) +
			s.th.TextPrimary.Render(dashIf(s.th, value))
	}
	list := func(lines []string) string { return strings.Join(lines, " ") }

	lines := []string{
		titleLine(s.th, "ROUTE "+dashIf(s.th, d.Name)),
		row("description", d.Description),
		row("match", list(d.MatchLines)),
		row("required", list(d.RequiredLines)),
		row("echo", list(d.EchoLines)),
		row("resp mti", d.RespMTI),
		row("resp fields", list(d.RespLines)),
		row("latency", plainDecor(s.th, d.Latency)),
	}
	if d.DropConnection {
		lines = append(lines, row("behavior", "drops connection"))
	}
	lines = append(lines, s.th.Deemphasized.Render("esc back"))

	return strings.Join(lines, "\n")
}

// hitsCell is the HITS cell: the formatted count once a stats snapshot
// exists, the dash before (unknown ≠ zero).
func (s *Server) hitsCell(r RouteRow) string {
	if !s.state.StatsKnown {
		return dashIf(s.th, "")
	}

	return formatCount(r.Hits)
}

// formatCount renders n with thousands separators ("1,204"). Written
// inline on purpose: golang.org/x/humanize does not exist as a package
// and promoting the indirect github.com/dustin/go-humanize dependency
// for one six-line helper is not worth the go.mod change.
func formatCount(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}

		return s
	}
	rem := len(s) % 3
	out := s[:rem]
	for i := rem; i < len(s); i += 3 {
		if out != "" {
			out += ","
		}
		out += s[i : i+3]
	}
	if neg {
		out = "-" + out
	}

	return out
}

// dotOn/dotOff pick the running/stopped dot for the theme's glyph mode.
func (s *Server) dotOn() string  { return serverDot(s.th, true) }
func (s *Server) dotOff() string { return serverDot(s.th, false) }

func serverDot(th *theme.Theme, on bool) string {
	switch {
	case th.ASCII && on:
		return asciiDotOn
	case th.ASCII:
		return asciiDotOff
	case on:
		return glyphDotOn
	default:
		return glyphDotOff
	}
}
