// ctf_view.go renders the §K body: the title line
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
	"jiso/internal/tui/geom"
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
	c.sections = c.sections[:0] // redraw the section rects alongside the ink
	c.recRect = geom.Rect{}     // the RECORDS box re-publishes below, or not at all

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
	// adds something the root note did not already say (QA).
	if line := c.emptyHintLine(); line != "" && line != note {
		head += "\n" + clipCells(c.th.TextMuted.Render(line), w, clipTail(c.th))
		headH++
	}

	if c.previewOpen && c.state.Preview != nil {
		return clipBlockStyled(c.th, head+"\n"+c.previewBody(0, headH, w), h, w)
	}

	footer := c.summaryLine(w) + "\n" + c.hintLine(w)
	footerH := 2
	paneH := max(h-headH-footerH, 4)

	if w >= frame.FullWidth {
		listW := max(w-ctfParamsPaneW-ctfSectionGap, ctfMinListWidth)
		paramsW := max(min(ctfParamsPaneW, w-listW-ctfSectionGap), ctfMinParamsBoxW)
		gap := strings.Repeat(" ", ctfSectionGap)

		// Origins advance by the drawn widths the join consumes.
		listSec := c.listBox(0, headH, listW, paneH)
		paramsSec := c.paramsBox(lipgloss.Width(listSec)+ctfSectionGap, headH, paramsW, paneH)

		body := lipgloss.JoinHorizontal(lipgloss.Top, listSec, gap, paramsSec)

		return clipBlockStyled(c.th, head+"\n"+body+"\n"+footer, h, w)
	}

	listH := max(paneH/2, 4)
	paramsH := max(paneH-listH-ctfSectionGap, 4)

	// The stacked "\n" terminates the list section's last line; the
	// params section starts on the very next line (no phantom gap row).
	listSec := c.listBox(0, headH, w, listH)
	paramsSec := c.paramsBox(0, headH+lipgloss.Height(listSec), w, paramsH)

	return clipBlockStyled(c.th, head+"\n"+listSec+"\n"+paramsSec+"\n"+footer, h, w)
}

// headerLine is the title row: the accent title with its
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

// summaryLine is the SUMMARY line: the root-derived
// "148 tx · $ 12,450.00 total · header/trailer dates auto" text (dash
// until the first preview). While the cursor-following dry leg is in
// flight it reads "… computing" (the summary follows the
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

// hintLine is the action line under the SUMMARY. While the
// PARAMETERS pane holds focus it names the edit keys instead:
// the form's editability must be visible, not folklore).
func (c *Ctf) hintLine(w int) string {
	if c.state.WriteLine != "" {
		return clipCells(c.th.Dim.Render(c.state.WriteLine), w, clipTail(c.th))
	}
	if c.pane == CtfPaneParams {
		line := "type edits" + joinSep(c.th) + "tab/↓ next field" + joinSep(c.th) + "esc back to list"

		return clipCells(c.th.Dim.Render(line), w, clipTail(c.th))
	}

	return clipCells(c.th.Dim.Render("[")+c.th.Key("Enter")+
		c.th.Dim.Render("] preview records "+pickGlyph(c.th, "\u2192", "->")+
			" shows every record, writes nothing"),
		w, clipTail(c.th))
}

// listBox renders the SESSIONS pane: the filtered list table in a
// titled box; the title is accented while the pane holds focus.
func (c *Ctf) listBox(x, y, w, h int) string {
	c.list.SetWidth(max(w-4, ctfMinListWidth))

	return c.sectionW(c.paneTitle(titleSessionsK, c.pane == CtfPaneSessions), c.list.View(), x, y, w, h)
}

// paramsBox renders the PARAMETERS form: one label+value row per
// field, the focus ring (caret + accent label) on the active field
// while the pane holds focus.
func (c *Ctf) paramsBox(x, y, w, h int) string {
	return c.sectionW(c.paneTitle(titleParams, c.pane == CtfPaneParams), c.paramsBody(), x, y, w, h)
}

// paramsBody renders the four form rows. The unfocused BIN placeholder
// is plain dim text — it no longer fakes a caret (the
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

// previewBody renders the record viewer overlay:
// headline lines, EVERY record in a scrollable box with a
// position ruler above and below, the viewer status line, and the
// write/close line (the overlay owns the keyboard first). (x, y) is the
// content-relative origin of the overlay body under the page head; the
// wheel hit map records the DRAWN records box at its real offset.
func (c *Ctf) previewBody(x, y, w int) string {
	p := c.state.Preview
	var b strings.Builder
	for _, line := range p.Headline {
		b.WriteString(c.th.TextPrimary.Render(line) + "\n")
	}
	box := c.recordsBox(w)
	// Publish the DRAWN box for the wheel hit map: measured
	// from the composed string like every recorded section rect, below
	// the headline lines the overlay pins first.
	c.recRect = sectionRect(x, y+len(p.Headline), box)
	b.WriteString(box + "\n")
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

// sectionW draws a titled bordered box of total size w×h (h includes
// the title line) through the one shared widgets.Section, so joins stay
// aligned (the §I layout.go convention). The box border always keeps
// the neutral token (the pane focus reads through the title); the
// section's Rect is recorded on the page at its content-relative
// origin.
func (c *Ctf) sectionW(title, body string, x, y, w, h int) string {
	sec := widgets.NewSection(c.th, title)
	out, _ := sec.Render(body, x, y, w, h)
	c.sections = append(c.sections, sectionRect(x, y, out))

	return out
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
