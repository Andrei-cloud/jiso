package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/theme"
)

// The §M help overlay is a frame-level modal above the page stack (same
// mechanism as the palette and the confirm dialogs, §N1 stacking): it
// replaces the content body, owns the keyboard while open (`?` toggles,
// Esc closes first), and is NOT a page in PageIDs.

const (
	helpBoxMaxWidth  = 60 // §M wireframe modal box width
	helpBoxMinWidth  = 40 // below the terminal's width the frame clips
	helpCompactInner = 36 // narrower content switches to compact lines
)

// helpOverlay renders the context-sensitive keymap box for one page.
type helpOverlay struct {
	th      *theme.Theme
	context string
	groups  []helpGroup
	width   int
}

// newHelpOverlay snapshots the CURRENT page's registry plus the global
// group; the overlay is pure display state (no page mutation, no cmds).
func newHelpOverlay(th *theme.Theme, p Page, km *globalKeyMap, width int) *helpOverlay {
	return &helpOverlay{
		th:      th,
		context: helpContextName(p.ID()),
		groups:  helpGroupsFor(p, km),
		width:   width,
	}
}

// openHelp opens the §M overlay over the CURRENT page (context label and
// page-actions block follow the stack top). The palette's "show help"
// action lands here too; opening while open never duplicates.
func (m *RootModel) openHelp() {
	if m.help != nil {
		return
	}
	m.help = newHelpOverlay(m.themeOrNil(), m.Current(), &m.keys, m.innerWS().Width)
	m.debug.logf("help open context=%s", m.Current().ID())
}

// boxWidth sizes the box for the terminal width: margined at full size,
// near-edge-to-edge when the terminal itself is narrow.
func (h *helpOverlay) boxWidth() int {
	switch {
	case h.width < helpBoxMinWidth+4:
		return max(h.width-2, 12)
	case h.width-4 > helpBoxMaxWidth:
		return helpBoxMaxWidth
	default:
		return h.width - 4
	}
}

// View renders the box: title rule, one block per group, closing rule.
// Lines are packed at segment boundaries and truncated, never wrapped.
func (h *helpOverlay) View() string {
	boxW := h.boxWidth()
	innerW := max(boxW-4, 8)
	compact := innerW < helpCompactInner

	labelW := 0
	for _, g := range h.groups {
		if len(g.Title) > labelW {
			labelW = len(g.Title)
		}
	}

	dash, vbar := "─", "│"
	// four corners: the bottom rule must use boxBL (└) at the left and
	// boxBR (┘) at the right (UAT round 6 QA: it used boxBR then boxTR,
	// so the bottom-left drew a ┘ and the bottom-right a ┐).
	boxTL, boxTR, boxBL, boxBR := "┌", "┐", "└", "┘"
	if h.th.ASCII {
		dash, vbar = "-", "|"
		boxTL, boxTR, boxBL, boxBR = "+", "+", "+", "+"
	}

	box := lipgloss.NewStyle().Foreground(h.th.Border.GetBorderTopForeground())
	rule := lipgloss.NewStyle().Foreground(h.th.SubtleBorder.GetBorderTopForeground())

	lines := []string{h.titleLine(box, rule, dash, boxW, boxTL, boxTR)}

	for _, g := range h.groups {
		for _, l := range h.groupLines(g, innerW, labelW, compact) {
			pad := strings.Repeat(" ", max(innerW-lipgloss.Width(l), 0))
			lines = append(lines, box.Render(vbar)+" "+l+pad+" "+box.Render(vbar))
		}
	}

	lines = append(lines, box.Render(boxBL+strings.Repeat(dash, boxW-2)+boxBR))

	return strings.Join(lines, "\n")
}

// titleLine renders the "┌─ HELP ── context: <page> ────┐" rule, clipped
// (never wrapped) when the context label is too long for the box.
func (h *helpOverlay) titleLine(box, rule lipgloss.Style, dash string, boxW int, tl, tr string) string {
	avail := boxW - 2

	title := dash + " " + h.th.Accent.Render("HELP") + " "
	// The plain width of that styled title, counted in cells: the box glyph is
	// three bytes in UTF-8, so len() here makes the unicode profile's rule two
	// cells short of the box it belongs to while ascii looks correct. This is the
	// bug the truecolor golden caught and the ascii one could not.
	titleW := lipgloss.Width(dash + " HELP ")

	// The plain and styled forms are the same width here, so one counter covers
	// the decision and the fill.
	context := dash + dash + " context: " + h.context + " "
	if titleW+lipgloss.Width(context) <= avail {
		title += rule.Render(context)
		titleW += lipgloss.Width(context)
	}

	// When the context does not fit, the title leaves it out rather than cutting
	// it short. The old render truncated the page name and then appended a fill
	// dash, so the ellipsis, a dash and the box corner landed in three adjacent
	// cells: "+- HELP -- context: Sessi~-+" is three dash-shaped marks in a row and
	// not one readable word. A title that says only HELP is still a title; one
	// that says "Sessi" is nothing.
	title += rule.Render(strings.Repeat(dash, max(avail-titleW, 0)))

	return box.Render(tl) + title + box.Render(tr)
}

// groupLines lays one group out: styled "keys note" segments joined by
// dim separators, packed into lines of inner width. Full mode aligns the
// first line behind the group-label column (continuation lines indent to
// the same column); compact mode (narrow) drops the label column and puts
// the label on its own line.
func (h *helpOverlay) groupLines(g helpGroup, innerW, labelW int, compact bool) []string {
	sep := h.th.Deemphasized.Render(h.th.Separator())

	labelPrefix, contPrefix := "", ""
	if !compact {
		labelPrefix = h.th.Deemphasized.Render(padHelp(g.Title, labelW)) + " "
		contPrefix = strings.Repeat(" ", labelW+1)
	}

	var lines []string

	if compact {
		lines = append(lines, h.th.Deemphasized.Render(g.Title))
	}

	row := make([]string, 0, len(g.Entries))

	var rowPlain string

	first := true

	flush := func() {
		prefix := contPrefix
		if first {
			prefix = labelPrefix
			first = false
		}

		lines = append(lines, h.fitLine(prefix+strings.Join(row, sep), innerW))
		row, rowPlain = nil, ""
	}

	for _, e := range g.Entries {
		seg := h.th.Accent.Render(e.Keys) + " " + h.th.TextPrimary.Render(e.Note)
		segPlain := e.Keys + " " + e.Note

		nextPlain := segPlain
		if rowPlain != "" {
			nextPlain = rowPlain + h.th.Separator() + segPlain
		}

		if len(row) > 0 && lipgloss.Width(nextPlain)+lipgloss.Width(labelPrefix) > innerW {
			flush()
			nextPlain = segPlain
		}

		row = append(row, seg)
		rowPlain = nextPlain
	}

	if len(row) > 0 {
		flush()
	}

	return lines
}

// fitLine clamps one content line to the inner width (truncate, no wrap).
func (h *helpOverlay) fitLine(line string, w int) string {
	return h.th.Truncate(line, w)
}

// padHelp right-pads a group label to the aligned label column.
func padHelp(s string, w int) string {
	if len(s) >= w {
		return s
	}

	return s + strings.Repeat(" ", w-len(s))
}
