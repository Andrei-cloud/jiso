// transactions_view.go renders the §B body: a title row (file label,
// live filter text, sort indicator) above the widgets.Table. Sizing is
// delegated to frame.ContentSize so page and chrome never disagree
// (dashboard layout.go pattern).
package pages

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// titleTransactions is the §B section title (wireframe shows it uppercase;
// it doubles as the slot's frame-visible title).
const titleTransactions = "TRANSACTIONS"

// txEmptyHint is the §B empty state (wireframe line; UAT round 8 D3: the
// key is the universal `f`, not `t`).
const txEmptyHint = "no tx file loaded"

// txPickFileSuffix completes the empty hint: "<dash> <f> to pick file".
const txPickFileSuffix = "to pick file"

// GlyphCursor marks the live-filter caret ("▏"); ASCII fallback "|".
const (
	GlyphCursor = "▏"
	ASCIICursor = "|"
)

// txMinTableWidth is the Table's floor width before the first size msg.
const txMinTableWidth = 40

// txColumns are the §B column widths (NAME · MTI · DESCRIPTION · DATASET ·
// SPEC). DESCRIPTION is the flex column: on narrow terminals it gives
// first, and the Table's fit() clamp keeps every line inside the frame at
// any width — cells truncate and never wrap.
func txColumns() []widgets.Column {
	return []widgets.Column{
		{Title: "NAME", Width: 16},
		{Title: "MTI", Width: 6},
		{Title: "DESCRIPTION", Width: 34, Flex: true},
		{Title: "DATASET", Width: 9},
		{Title: "SPEC", Width: 8},
	}
}

// View renders the §B body for the frame's content area.
func (t *Transactions) View() tea.View {
	w, h := frame.ContentSize(t.width, t.height)

	return tea.NewView(t.render(w, h))
}

// tableGridChrome is the grid renderer's non-data line count (top rule,
// header row, header rule, bottom rule): the table's row budget is the
// pane under the title line minus this, so the box fills the pane
// exactly (UAT round 8 finding 5 fill + Task 8.2c wheel windowing).
const tableGridChrome = 4

// render lays out title row + table, clipped to exactly h lines of at
// most w cells (never wraps, never overflows the content area). A failed
// tx-file load shows the reason instead of the table or the empty state
// (UAT round 7: silence hid the real error).
func (t *Transactions) render(w, h int) string {
	t.txRect = geom.Rect{} // the table re-publishes below, or not at all
	if t.state.Error != "" {
		return clipBlockStyled(t.th, t.titleRow(w)+"\n"+t.errorBody(), h, w)
	}
	if t.state.FileName == "" && len(t.state.Rows) == 0 {
		return clipBlockStyled(t.th, t.titleRow(w)+"\n"+t.emptyStateBody(), h, w)
	}
	t.table.SetWidth(w)
	t.table.SetHeight(max(h-1-tableGridChrome, 1))
	body := t.table.View()
	// Publish the DRAWN table box for the wheel hit map (Task 8.2c):
	// measured from the composed string like every recorded section
	// rect, so the registered region is the ink the user sees.
	t.txRect = sectionRect(0, 1, body)

	return clipBlockStyled(t.th, t.titleRow(w)+"\n"+body, h, w)
}

// titleRow renders "TRANSACTIONS  pool.json (12)  filter: …  sort: name ▲"
// clipped to the content width.
func (t *Transactions) titleRow(w int) string {
	file := dashIf(t.th, t.state.FileName) + " (" + strconv.Itoa(t.state.TxCount) + ")"
	filter := t.filter
	switch {
	case t.filtering:
		filter += cursorGlyph(t.th)
	case filter == "":
		filter = cursorGlyph(t.th)
	}
	sort := sortColumnNames[sortCycle[t.sortStep%len(sortCycle)].col] + " " +
		txSortGlyph(t.th, sortCycle[t.sortStep%len(sortCycle)].asc)

	line := strings.Join([]string{
		titleLine(t.th, titleTransactions),
		t.th.Deemphasized.Render(file),
		t.th.Deemphasized.Render("filter:") + " " + t.th.TextPrimary.Render(filter),
		t.th.Deemphasized.Render("sort:") + " " + t.th.TextPrimary.Render(sort),
	}, "  ")

	return clipCells(line, w, clipTail(t.th))
}

// emptyStateBody renders the wireframe empty line: "no tx file loaded —
// f to pick file" (dash per glyph mode; the key itself is accent-styled,
// the suffix muted — no duplicated key token). The key is read from the
// page's own PickFile binding, so the hint can never drift from the key
// the page matches on (the §M drift-pin rule, applied to the empty state).
func (t *Transactions) emptyStateBody() string {
	return t.th.TextMuted.Render(txEmptyHint+" "+dashIf(t.th, "")+" ") +
		t.th.Accent.Render(t.nav.PickFile.Keys()[0]) + " " + t.th.TextMuted.Render(txPickFileSuffix)
}

// errorBody renders why the last tx-file pick did not load (UAT round 7):
// the error glyph + the reason, so a rejected file names its problem
// instead of leaving the page silent. The line is clipped to the frame.
func (t *Transactions) errorBody() string {
	return t.th.StatusError.Render(theme.GlyphError+" ") + t.th.TextPrimary.Render(t.state.Error)
}

// txSortGlyph is the Table widget's sort indicator for the page's own
// title row (same glyphs the table header shows).
func txSortGlyph(th *theme.Theme, asc bool) string {
	switch {
	case th.ASCII && asc:
		return widgets.ASCIISortAsc
	case th.ASCII:
		return widgets.ASCIISortDesc
	case asc:
		return widgets.GlyphSortAsc
	default:
		return widgets.GlyphSortDesc
	}
}

// cursorGlyph is the filter caret for th's glyph mode.
func cursorGlyph(th *theme.Theme) string {
	if th.ASCII {
		return ASCIICursor
	}

	return GlyphCursor
}

// clipTail is the truncation ellipsis for th's glyph mode.
func clipTail(th *theme.Theme) string { return th.Ellipsis() }

// clipBlockStyled flattens a body to exactly h lines of at most maxW
// cells (truncate, never wrap; short bodies pad with empty lines so
// joins and the frame stay aligned). Shared by the dashboard cards and
// the transactions page.
func clipBlockStyled(th *theme.Theme, body string, h, maxW int) string {
	src := strings.Split(strings.TrimRight(body, "\n"), "\n")

	lines := make([]string, 0, h)
	for i := 0; i < h; i++ {
		if i < len(src) {
			lines = append(lines, clipCells(src[i], maxW, clipTail(th)))
		} else {
			lines = append(lines, "")
		}
	}

	return strings.Join(lines, "\n")
}
