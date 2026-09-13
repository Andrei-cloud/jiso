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

	// serverWideCols is the width at which ROUTES keeps a wider (relative,
	// clamped) column and the LOG owns the rest (proposal 05 §1); below it
	// (but still >= frame.FullWidth) the LOG takes ~45% and ROUTES the
	// middle.
	serverWideCols = 140

	// serverStatsFraction sizes the STATS column at split widths.
	serverStatsFraction = 3
	serverStatsMin      = 26
	serverStatsMax      = 40
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
	head := s.headerRow(w)
	if errLine := s.errorLine(w); errLine != "" {
		head += "\n" + errLine
	}

	if s.detailOpen && s.detailIdx >= 0 && s.detailIdx < len(s.state.Routes) {
		return clipBlockStyled(s.th, head+"\n"+s.detailBody(), h, w)
	}

	if s.state.Starting {
		return clipBlockStyled(s.th, head+"\n"+s.th.TextMuted.Render(s.dotOn()+" starting "+plainDecor(s.th, "…")), h, w)
	}

	paneH := max(h-1, 4)

	if w >= frame.FullWidth {
		if len(s.state.Log) == 0 {
			// No server output yet: the pre-proposal-05 two-column
			// layout stays byte-identical (goldens).
			statsW := min(max(w/serverStatsFraction, serverStatsMin), serverStatsMax)
			routesW := max(w-statsW-serverSectionGap, 20)

			body := lipgloss.JoinHorizontal(lipgloss.Top,
				s.leftBox(statsW, paneH), strings.Repeat(" ", serverSectionGap),
				s.routesBox(routesW, paneH))

			return clipBlockStyled(s.th, head+"\n"+body, h, w)
		}

		// Proposal 05 §1: the LOG is the live signal and owns the big
		// right pane at full height; STATS and ROUTES keep their
		// natural (short) heights, top-aligned (JoinHorizontal pads).
		statsW := serverStatsMin
		logW, routesW := server3ColWidths(w)
		statsH := min(serverStatsBoxH, paneH)
		routesH := min(paneH, max(len(s.state.Routes)+5, 6))

		body := lipgloss.JoinHorizontal(lipgloss.Top,
			s.leftBox(statsW, statsH), strings.Repeat(" ", serverSectionGap),
			s.routesBox(routesW, routesH), strings.Repeat(" ", serverSectionGap),
			s.logBox(logW, paneH))

		return clipBlockStyled(s.th, head+"\n"+body, h, w)
	}

	if len(s.state.Log) > 0 {
		statsH := min(serverStatsBoxH, max(paneH-serverNarrowLogH-serverSectionGap*2, 6))
		logH := min(serverNarrowLogH+4, max(paneH-statsH-serverSectionGap*2, serverLogMinBoxH))
		routesH := max(paneH-statsH-logH-serverSectionGap*2, 4)

		return clipBlockStyled(s.th, head+"\n"+
			s.leftBox(w, statsH)+"\n"+
			s.logBox(w, logH)+"\n"+
			s.routesBox(w, routesH), h, w)
	}

	statsH := max(paneH/2, 6)
	routesH := max(paneH-statsH-serverSectionGap, 4)

	return clipBlockStyled(s.th, head+"\n"+
		s.leftBox(w, statsH)+"\n"+
		s.routesBox(w, routesH), h, w)
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

// server3ColWidths sizes the proposal-05 three-column body: the LOG
// owns the rest at >= serverWideCols and takes ~45% between
// frame.FullWidth and it. The ROUTES column is relative to the terminal
// (UAT round 5: panes adopt to the size), clamped 36..64 so the match
// expressions stay readable without starving the live log.
func server3ColWidths(w int) (logW, routesW int) {
	if w >= serverWideCols {
		routesW = min(max(w*28/100, 36), 64)
		logW = max(w-serverStatsMin-routesW-serverSectionGap*2, 24)

		return logW, routesW
	}

	logW = max(w*45/100, 30)
	routesW = max(w-serverStatsMin-logW-serverSectionGap*2, 20)
	logW = max(w-serverStatsMin-routesW-serverSectionGap*2, 24)

	return logW, routesW
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
func (s *Server) logBox(w, h int) string {
	inner := max(h-3, 1)
	total := len(s.state.Log)

	// The newest line sits just above the hint; logScroll counts lines
	// back from the newest and the window never scrolls past the oldest.
	avail := max(inner-1, 1)
	end := min(max(total-s.logScroll, avail), total)
	start := max(end-avail, 0)

	body := make([]string, 0, inner)
	for _, l := range s.state.Log[start:end] {
		body = append(body, clipCells(s.th.TextMuted.Render(CompactServerLog(s.th, l)), max(w-4, 8), clipTail(s.th)))
	}
	for len(body) < avail {
		body = append(body, "")
	}
	body = append(body, clipCells(s.th.Deemphasized.Render(logFooterHint(s.th)), max(w-4, 8), clipTail(s.th)))

	// The log pane is the default focus target (j/k scroll it until r
	// or Tab moves to ROUTES); UAT round 5 makes that visible.
	return s.sectionW(paneTitle(s.th, titleLog, !s.routesFocused), strings.Join(body, "\n"), w, h, !s.routesFocused)
}

// leftBox renders the STATS card (or the start-form hint before the
// first snapshot) as a titled bordered box, dashboard sectionW idiom.
func (s *Server) leftBox(w, h int) string {
	title := titleStats
	body := s.statsBody()
	if !s.state.StatsKnown {
		title = "START"
		body = s.startHintBody()
	}

	return s.sectionW(paneTitle(s.th, title, false), body, w, h, false)
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
// The table is sized to the box's inner content width (lipgloss v2
// Width includes the border, so a table padded to w-2 word-wrapped the
// right-aligned HITS column — UAT round 5; sessions listBox idiom).
func (s *Server) routesBox(w, h int) string {
	s.table.SetFocused(s.routesFocused)
	s.table.SetWidth(max(w-4, 4))

	return s.sectionW(paneTitle(s.th, titleRoutes, s.routesFocused), s.table.View(), w, h, s.routesFocused)
}

// sectionW renders "TITLE" + the body clipped into a bordered box of
// total size w×h (h includes the title line), so joins stay aligned
// (dashboard layout.go idiom). The body clips to the box's CONTENT
// width (w-4), not its outer width: UAT round 5, the ROUTES table's
// full-width lines wrapped two cells past the right border. A focused
// pane's border takes the accent colour (UAT round 5: the server page
// had no focus indication at all); the rest keep the neutral border
// token.
func (s *Server) sectionW(title, body string, w, h int, focused bool) string {
	inner := max(h-3, 1)
	box := clipBlockStyled(s.th, body, inner, max(w-4, 1))
	style := s.boxStyle()
	if focused {
		style = style.BorderForeground(s.th.Accent.GetForeground())
	}

	return titleLine(s.th, title) + "\n" +
		style.Width(max(w-2, 1)).Height(inner).Render(box)
}

// boxStyle is the pane border: rounded normally, ASCII under
// theme.ASCII (dashboard boxStyle idiom).
func (s *Server) boxStyle() lipgloss.Style {
	b := lipgloss.RoundedBorder()
	if s.th.ASCII {
		b = lipgloss.ASCIIBorder()
	}

	return lipgloss.NewStyle().
		Border(b).
		BorderForeground(s.th.Border.GetBorderTopForeground())
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
