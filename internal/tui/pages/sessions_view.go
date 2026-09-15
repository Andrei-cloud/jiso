// sessions_view.go renders the §I body (wireframe §I): the header line
// ("SESSIONS (db: ./sessions.db)  filter: ▏"), then at ≥ frame.FullWidth
// the three panes side by side — SESSIONS list, STATS card for the
// selected session, TX HISTORY table — and below it the responsive
// fallback: the sessions list alone, Enter drilling into the stacked
// stats+history view (the wireframe's 80×24 row). The tx review overlay
// replaces the body while open (§C-style reconstructed hex + parsed
// fields; Esc closes first). Everything is pre-derived display data;
// the only math here is pane sizing.
package pages

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

const (
	titleSessions      = "SESSIONS"
	titleSessionsStats = "STATS"
	titleHistory       = "TX HISTORY"

	// Pane/table floors.
	sessionsMinListWidth    = 20
	sessionsMinHistoryWidth = 40
	// The list and stats panes are ratios of the content width at split
	// widths (UAT round 8 finding 5: the old fixed 30-cell caps froze the
	// split on wide terminals); TX HISTORY absorbs the remainder.
	sessionsListFraction  = 4
	sessionsStatsFraction = 4
	sessionsMinStatsWidth = 20
	sessionsSectionGap    = 1
	// sessionsStatsLabelCol is the stats card's label column.
	sessionsStatsLabelCol = 10
)

// View renders the §I body for the frame's content area.
func (s *Sessions) View() tea.View {
	w, h := frame.ContentSize(s.width, s.height)

	return tea.NewView(s.render(w, h))
}

// render lays out header (+optional note) + panes (or the drill /
// review body), clipped to exactly h lines of at most w cells.
func (s *Sessions) render(w, h int) string {
	s.sections = s.sections[:0] // redraw the section rects alongside the ink

	head := s.headerLine(w)
	headH := 1
	note := s.state.Note
	if line := s.noteLine(w); line != "" {
		head += "\n" + line
		headH++
	}
	// The empty-state hint repeats the note verbatim when the whole
	// database is missing (both name "database not configured - pass
	// --db…"); showing it twice is clutter, so render it only when it
	// adds something the root note did not already say (UAT round 6 QA).
	if line := s.emptyHintLine(); line != "" && line != note {
		head += "\n" + clipCells(s.th.TextMuted.Render(line), w, clipTail(s.th))
		headH++
	}

	if s.reviewOpen && s.state.Review != nil {
		return s.renderReview(head, headH, h, w)
	}

	paneH := max(h-headH, 4)

	if w >= frame.FullWidth {
		listW := max(w/sessionsListFraction, sessionsMinListWidth)
		statsW := max(w/sessionsStatsFraction, sessionsMinStatsWidth)
		// TX HISTORY is the remainder: the three panes plus the two gaps
		// sum exactly to the content width (the floor cannot bite at
		// w >= frame.FullWidth, so it never opens a trailing gap).
		histW := max(w-listW-statsW-2*sessionsSectionGap, sessionsMinHistoryWidth)
		gap := strings.Repeat(" ", sessionsSectionGap)

		// Origins advance by the DRAWN widths the join actually consumes
		// (measured strings), never by nominal pane widths.
		listSec := s.listBox(0, headH, listW, paneH)
		statsX := lipgloss.Width(listSec) + sessionsSectionGap
		statsSec := s.statsBox(statsX, headH, statsW, paneH)
		histSec := s.historyBox(statsX+lipgloss.Width(statsSec)+sessionsSectionGap, headH, histW, paneH)

		body := lipgloss.JoinHorizontal(lipgloss.Top,
			listSec, gap, statsSec, gap, histSec)

		return clipBlockStyled(s.th, head+"\n"+body, h, w)
	}

	if !s.drill {
		return clipBlockStyled(s.th, head+"\n"+s.listBox(0, headH, w, paneH), h, w)
	}

	statsH := max(paneH/3, 6)
	histH := max(paneH-statsH-sessionsSectionGap, 4)

	statsSec := s.statsBox(0, headH, w, statsH)
	// The stacked "\n" is a line terminator, not a gap row: history's
	// top line is exactly where the stats section's drawn lines end.
	histSec := s.historyBox(0, headH+lipgloss.Height(statsSec), w, histH)

	return clipBlockStyled(s.th, head+"\n"+statsSec+"\n"+histSec, h, w)
}

// headerLine is the wireframe title row: accent title, the configured
// db path (dash when unset), and the live filter segment with the caret
// while filter mode owns the keyboard.
func (s *Sessions) headerLine(w int) string {
	// four sections at most: the title, the note, the list and the stats box
	parts := make([]string, 0, 4)
	parts = append(parts, titleLine(s.th, titleSessions))
	db := "db: " + dashIf(s.th, s.state.DBPath)
	parts = append(parts, s.th.Deemphasized.Render(db))
	filter := "filter: " + dashIf(s.th, s.filter)
	if s.filtering {
		filter = "filter: " + s.filter + cursorGlyph(s.th)
	}
	parts = append(parts, s.th.Deemphasized.Render(filter))

	return clipCells(strings.Join(parts, "  "), w, clipTail(s.th))
}

// noteLine is the root-stamped status line — a typed façade error
// rendered as empty-state text (missing DB etc.), never a crash.
func (s *Sessions) noteLine(w int) string {
	if s.state.Note == "" {
		return ""
	}

	return clipCells(s.th.Status(theme.KindWarn, s.state.Note), w, clipTail(s.th))
}

// listBox renders the SESSIONS pane: the filtered list table in a
// titled box; the title is accented and the border lit while the pane
// holds focus (UAT round 5: two accented titles and two cursor markers
// made the active pane ambiguous). The table is sized to the box's
// inner content width (lipgloss v2 Width includes the border, so a
// table padded to w-2 would word-wrap). (x,y) is the section's
// content-relative origin, recorded with its Rect.
func (s *Sessions) listBox(x, y, w, h int) string {
	focused := s.pane == paneSessions
	s.list.SetFocused(focused)
	s.list.SetWidth(max(w-4, sessionsMinListWidth))

	return s.sectionW(s.paneTitle(titleSessions, focused), s.list.View(), x, y, w, h, focused)
}

// statsBox renders the STATS card (root-derived label/value lines).
// STATS is display-only and never takes focus, so its title stays
// muted — accenting it made two panes look active at once (UAT round 5).
func (s *Sessions) statsBox(x, y, w, h int) string {
	return s.sectionW(paneTitle(s.th, titleSessionsStats, false), s.statsBody(), x, y, w, h, false)
}

// historyBox renders the TX HISTORY pane, titled with the selected
// session's short id (wireframe: "TX HISTORY (9f3c…a1)"). The focused
// pane accents its title, lights its border, and is the only table
// showing a cursor marker (UAT round 5).
func (s *Sessions) historyBox(x, y, w, h int) string {
	focused := s.pane == paneHistory || s.drill // the drill drives history directly
	s.history.SetFocused(focused)
	s.history.SetWidth(max(w-4, sessionsMinHistoryWidth))
	title := titleHistory
	if s.state.SelectedID != "" {
		title += " (" + shortDisplayID(s.th, s.state.SelectedID) + ")"
	}

	return s.sectionW(s.paneTitle(title, focused), s.history.View(), x, y, w, h, focused)
}

// paneTitle accents a focused pane's title and mutes the rest (focus
// is symbol-of-position too: the cursor marker lives in the table).
func (s *Sessions) paneTitle(title string, focused bool) string {
	return paneTitle(s.th, title, focused)
}

// statsBody renders the root-derived stats lines (total/ok/fail/avg/RC
// dist) as label/value rows; empty renders the next-action hint.
func (s *Sessions) statsBody() string {
	if len(s.state.Stats) == 0 {
		return s.th.TextMuted.Render(s.statsEmptyText())
	}
	lines := make([]string, 0, len(s.state.Stats))
	for _, kv := range s.state.Stats {
		lines = append(lines,
			s.th.Deemphasized.Render(padRight(kv.Label, sessionsStatsLabelCol))+
				s.th.TextPrimary.Render(dashIf(s.th, kv.Value)))
	}

	return strings.Join(lines, "\n")
}

// statsEmptyText names the missing selection (wireframe empty state).
func (s *Sessions) statsEmptyText() string {
	if s.state.DBPath == "" {
		return EmptyTextNoSessionDB
	}

	return "select a session to see its stats"
}

// renderReview shows the head lines pinned plus a WINDOW of the review
// body starting at s.reviewScroll (UAT round 5: the old render
// flattened head+body to exactly h lines, so a review taller than the
// window silently clipped its RESPONSE sections — and even its own esc
// hint — out of reach). The scroll is clamped here against the live
// content, so a stale offset after a resize still renders sanely.
func (s *Sessions) renderReview(head string, headH, h, w int) string {
	body := strings.Split(strings.TrimRight(s.reviewBody(), "\n"), "\n")
	avail := max(h-headH, 1)
	top := min(max(s.reviewScroll, 0), max(len(body)-avail, 0))
	window := strings.Join(body[top:min(top+avail, len(body))], "\n")

	return clipBlockStyled(s.th, head+"\n"+window, h, w)
}

// reviewBody renders the §C-style reconstructed tx review: headline
// lines, then per message the packed HEX block and the parsed FIELDS
// (describe) block, closed by the esc hint (Esc owns the keyboard
// first). Raw fallbacks and parse errors are surfaced, never hidden.
func (s *Sessions) reviewBody() string {
	r := s.state.Review
	var b strings.Builder
	for _, line := range r.Headline {
		b.WriteString(s.th.TextPrimary.Render(line) + "\n")
	}
	b.WriteString(s.reviewMessage("REQUEST", r.Request))
	if r.Response != nil {
		b.WriteString(s.reviewMessage("RESPONSE", r.Response))
	}
	sep := s.th.Separator()
	b.WriteString(s.th.Deemphasized.Render("j" + sep + "k scroll" + sep + "esc close"))

	return b.String()
}

// reviewMessage renders one reconstructed message section.
func (s *Sessions) reviewMessage(title string, m *TxReviewMessage) string {
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

// plainBlock keeps a multi-line block verbatim (the outer
// clipBlockStyled still clamps every line to the pane width and pads
// the section to the exact height).
func plainBlock(block string) string {
	return strings.TrimRight(block, "\n")
}

// sectionW draws a titled bordered box of total size w×h (h includes
// the title line) through the one shared widgets.Section, so joins stay
// aligned (the dashboard layout.go convention). A focused pane's border
// takes the accent colour (UAT round 5: the active pane must be
// obvious); the rest keep the neutral border token. The section's Rect
// is recorded on the page at its content-relative origin.
func (s *Sessions) sectionW(title, body string, x, y, w, h int, focused bool) string {
	sec := widgets.NewSection(s.th, title)
	sec.Focused = focused
	out, _ := sec.Render(body, x, y, w, h)
	s.sections = append(s.sections, sectionRect(x, y, out))

	return out
}

// sessionsListColumns are the §I list columns (short id + relative
// time); WHEN is the flex column.
func sessionsListColumns() []widgets.Column {
	return []widgets.Column{
		{Title: "SESSION", Width: 11},
		{Title: "WHEN", Width: 11, Flex: true},
	}
}

// sessionsHistoryColumns are the §I history columns; TRANSACTION gives
// first on narrow panes (the Table's fit clamp never wraps).
func sessionsHistoryColumns() []widgets.Column {
	return []widgets.Column{
		{Title: "TIME", Width: 8},
		{Title: colTransaction, Width: 16, Flex: true},
		{Title: "MTI", Width: 5},
		{Title: "RC", Width: 4},
		{Title: colStatus, Width: 11},
		{Title: "LATENCY", Width: 9, AlignRight: true},
	}
}

// shortDisplayID shortens a display id for the pane title (the list
// already carries the root-derived short form).
func shortDisplayID(th *theme.Theme, id string) string {
	if len(id) <= 12 {
		return id
	}

	return th.ElideMiddle(id, 4, 2)
}
