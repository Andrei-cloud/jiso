// ctf_view.go renders the §K body (wireframe §K): the title line
// ("VISA BASE II — CTF EXPORT"), then at ≥ frame.FullWidth the two
// panes side by side — SESSIONS list (short id, relative time,
// approved counts) and the PARAMETERS form (four label+input rows with
// the focus ring on the active field) — and below it the stacked
// narrow fallback (parameters under the list). The SUMMARY line and
// the [Enter] hint close the body; the preview overlay replaces the
// body while open (first/last record + counts; Esc closes first).
// Everything is pre-derived display data; the only math here is pane
// sizing.
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
	titleCtf       = "VISA BASE II"
	titleCtfSub    = "CTF EXPORT"
	titleSessionsK = "SESSIONS (Visa tx eligible)"
	titleParams    = "PARAMETERS"

	// Pane/table floors and fixed widths.
	ctfMinListWidth  = 20
	ctfMinParamsBoxW = 40
	ctfParamsPaneW   = 48
	ctfSectionGap    = 1
	// ctfParamLabelCol is the PARAMETERS label column.
	ctfParamLabelCol = 22
)

// View renders the §K body for the frame's content area.
func (c *Ctf) View() tea.View {
	w, h := frame.ContentSize(c.width, c.height)

	return tea.NewView(c.render(w, h))
}

// render lays out header (+optional note) + panes + summary/hint lines
// (or the preview overlay body), clipped to exactly h lines of at most
// w cells.
func (c *Ctf) render(w, h int) string {
	head := c.headerLine(w)
	headH := 1
	note := c.state.Note
	if line := c.noteLine(w); line != "" {
		head += "\n" + line
		headH++
	}
	// The empty-state hint repeats the note verbatim when the whole
	// database is missing (both name "database not configured - pass
	// --db…"); showing it twice is clutter, so render it only when it
	// adds something the root note did not already say (UAT round 6 QA).
	if line := c.emptyHintLine(); line != "" && line != note {
		head += "\n" + clipCells(c.th.TextMuted.Render(line), w, clipTail(c.th))
		headH++
	}

	if c.previewOpen && c.state.Preview != nil {
		return clipBlockStyled(c.th, head+"\n"+c.previewBody(w), h, w)
	}

	footer := c.summaryLine(w) + "\n" + c.hintLine(w)
	footerH := 2
	paneH := max(h-headH-footerH, 4)

	if w >= frame.FullWidth {
		listW := max(w-ctfParamsPaneW-ctfSectionGap, ctfMinListWidth)
		paramsW := max(min(ctfParamsPaneW, w-listW-ctfSectionGap), ctfMinParamsBoxW)
		gap := strings.Repeat(" ", ctfSectionGap)

		body := lipgloss.JoinHorizontal(lipgloss.Top,
			c.listBox(listW, paneH), gap,
			c.paramsBox(paramsW, paneH))

		return clipBlockStyled(c.th, head+"\n"+body+"\n"+footer, h, w)
	}

	listH := max(paneH/2, 4)
	paramsH := max(paneH-listH-ctfSectionGap, 4)

	return clipBlockStyled(c.th, head+"\n"+
		c.listBox(w, listH)+"\n"+
		c.paramsBox(w, paramsH)+"\n"+footer, h, w)
}

// headerLine is the wireframe title row: the accent title with its
// em/ASCII dash, and the configured db path (dash when unset).
func (c *Ctf) headerLine(w int) string {
	dash := "\u2014"
	if c.th.ASCII {
		dash = "-"
	}
	title := titleCtf + " " + dash + " " + titleCtfSub
	// three sections at most: the title, the body and the footer
	parts := make([]string, 0, 3)
	parts = append(parts, titleLine(c.th, title))
	db := "db: " + dashIf(c.th, c.state.DBPath)
	parts = append(parts, c.th.Deemphasized.Render(db))

	return clipCells(strings.Join(parts, "  "), w, clipTail(c.th))
}

// noteLine is the root-stamped status line — a typed façade error
// rendered as text (missing DB etc.), never a crash.
func (c *Ctf) noteLine(w int) string {
	if c.state.Note == "" {
		return ""
	}

	return clipCells(c.th.Status(theme.KindWarn, c.state.Note), w, clipTail(c.th))
}

// summaryLine is the wireframe SUMMARY line: the root-derived
// "148 tx · $ 12,450.00 total · header/trailer dates auto" text (dash
// until the first preview). While the cursor-following dry leg is in
// flight it reads "… computing" (UAT round 6: the summary follows the
// cursor, and the operator sees the recalculation happen).
func (c *Ctf) summaryLine(w int) string {
	label := "SUMMARY: "
	sep := c.th.Separator()
	if c.state.SummaryWait {
		return clipCells(c.th.Deemphasized.Render(label)+
			c.th.Dim.Render(c.th.Ellipsis()+" computing"), w, clipTail(c.th))
	}
	value := c.state.SummaryLine
	if value == "" {
		value = dashIf(c.th, "") + " tx" + sep + "total " + dashIf(c.th, "") + sep + "header/trailer dates auto"
	}
	kind := theme.KindOK
	if c.state.WriteLine != "" {
		kind = map[bool]theme.Kind{true: theme.KindOK, false: theme.KindError}[c.state.WriteOK]
	}

	return clipCells(c.th.Deemphasized.Render(label)+c.th.Status(kind, value), w, clipTail(c.th))
}

// hintLine is the wireframe action line under the SUMMARY. While the
// PARAMETERS pane holds focus it names the edit keys instead (UAT round
// 6: the form's editability must be visible, not folklore).
func (c *Ctf) hintLine(w int) string {
	if c.state.WriteLine != "" {
		return clipCells(c.th.Dim.Render(c.state.WriteLine), w, clipTail(c.th))
	}
	if c.pane == CtfPaneParams {
		line := "type edits" + joinSep(c.th) + "tab/↓ next field" + joinSep(c.th) + "esc back to list"

		return clipCells(c.th.Dim.Render(line), w, clipTail(c.th))
	}

	return clipCells(c.th.Dim.Render("[")+c.th.HotKey.Render("Enter")+
		c.th.Dim.Render("] preview records "+pickGlyph(c.th, "\u2192", "->")+
			" shows every record, writes nothing"),
		w, clipTail(c.th))
}

// listBox renders the SESSIONS pane: the filtered list table in a
// titled box; the title is accented while the pane holds focus.
func (c *Ctf) listBox(w, h int) string {
	c.list.SetWidth(max(w-4, ctfMinListWidth))

	return c.sectionW(c.paneTitle(titleSessionsK, c.pane == CtfPaneSessions), c.list.View(), w, h)
}

// paramsBox renders the PARAMETERS form: one label+value row per
// field, the focus ring (caret + accent label) on the active field
// while the pane holds focus.
func (c *Ctf) paramsBox(w, h int) string {
	return c.sectionW(c.paneTitle(titleParams, c.pane == CtfPaneParams), c.paramsBody(), w, h)
}

// paramsBody renders the four form rows. The unfocused BIN placeholder
// is plain dim text — it no longer fakes a caret (UAT round 6: the
// borrowed caret on an UNFOCUSED row read as "this is the editable
// one", which is exactly why the focused-and-editable CIB looked locked).
func (c *Ctf) paramsBody() string {
	lines := make([]string, 0, FormFieldCount)
	for i := 0; i < FormFieldCount; i++ {
		label := padRight(FieldLabels[i], ctfParamLabelCol)
		value := c.FieldValue(i)
		focused := c.pane == CtfPaneParams && c.fieldFocus == i
		labelStyle, valueStyle := c.th.Deemphasized, c.th.TextPrimary
		switch {
		case focused:
			labelStyle, valueStyle = c.th.Accent, c.th.TextPrimary
			value += cursorGlyph(c.th)
		case i == FieldBin && value == "":
			value, valueStyle = "(blank = all)", c.th.Dim
		}
		lines = append(lines, labelStyle.Render(label)+valueStyle.Render(value))
	}

	return strings.Join(lines, "\n")
}

// paneTitle accents a focused pane's title and mutes the rest (focus
// is symbol-of-position too: the caret marks the active field).
func (c *Ctf) paneTitle(title string, focused bool) string {
	if focused {
		return titleLine(c.th, title)
	}

	return c.th.TextMuted.Render(title)
}

// previewBody renders the record viewer overlay (UAT round 6
// wireframe): headline lines, EVERY record in a scrollable box with a
// position ruler above and below, the viewer status line, and the
// write/close line (the overlay owns the keyboard first).
func (c *Ctf) previewBody(w int) string {
	p := c.state.Preview
	var b strings.Builder
	for _, line := range p.Headline {
		b.WriteString(c.th.TextPrimary.Render(line) + "\n")
	}
	b.WriteString(c.recordsBox(w) + "\n")
	b.WriteString(c.ctfRecStatus() + "\n")
	b.WriteString(c.ctfRecWriteLine())

	return b.String()
}

// emptyText is the SESSIONS pane's short empty-state line.
func (c *Ctf) emptyText() string {
	switch {
	case c.filtering || c.filter != "":
		return "no sessions match filter"
	case c.state.DBPath == "":
		return "database not configured"
	default:
		return "no CTF-eligible sessions"
	}
}

// emptyHintLine is the full empty-state sentence naming the next
// action, rendered across the page when the list is empty.
func (c *Ctf) emptyHintLine() string {
	if c.filtering || c.filter != "" || len(c.state.Sessions) > 0 {
		return ""
	}
	if c.state.DBPath == "" {
		return EmptyTextNoSessionDB
	}

	return "no CTF-eligible sessions in " + c.state.DBPath + ". Record a Visa session first."
}

// sectionW renders "TITLE" + the body clipped into a bordered box of
// total size w×h (h includes the title line), so joins stay aligned
// (the §I sectionW idiom).
func (c *Ctf) sectionW(title, body string, w, h int) string {
	inner := max(h-3, 1)
	box := clipBlockStyled(c.th, body, inner, max(w-4, 1))

	return titleLine(c.th, title) + "\n" +
		c.boxStyle().Width(max(w, 4)).Height(inner+2).Render(box)
}

// boxStyle is the pane border: rounded normally, ASCII under
// theme.ASCII (the §I boxStyle idiom).
func (c *Ctf) boxStyle() lipgloss.Style {
	b := lipgloss.RoundedBorder()
	if c.th.ASCII {
		b = lipgloss.ASCIIBorder()
	}

	return lipgloss.NewStyle().
		Border(b).
		BorderForeground(c.th.Border.GetBorderTopForeground())
}

// ctfListColumns are the §K list columns (short id + relative time +
// approved count); WHEN is the flex column.
func ctfListColumns() []widgets.Column {
	return []widgets.Column{
		{Title: "SESSION", Width: 11},
		{Title: "WHEN", Width: 11, Flex: true},
		{Title: "APPROVED", Width: 12},
	}
}
