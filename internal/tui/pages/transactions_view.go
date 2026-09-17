// transactions_view.go renders the §B body: a title row (file label, live
// filter text, sort indicator) above the widgets.Table; sizing is
// delegated to frame.ContentSize so page and chrome never disagree.
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

// txEmptyHint is the §B empty state line (the picker key is the universal `f`).
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
// SPEC). DESCRIPTION is the flex column: it gives first on narrow terminals
// and cells truncate, never wrap.
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
// header row, header rule, bottom rule): the table's row budget is the pane
// under the title line minus this, so the box fills the pane exactly.
const tableGridChrome = 4

// render lays out title row + table, clipped to exactly h lines of at most
// w cells. A failed tx-file load shows the reason instead of the table or
// the empty state (silence must not hide the real error).
func (t *Transactions) render(w, h int) string {
	t.txRect = geom.Rect{}    // the table re-publishes below, or not at all
	t.selRows = t.selRows[:0] // and so do its click rows

	if t.state.Error != "" {
		return clipBlockStyled(t.th, t.titleRow(w)+"\n"+t.errorBody(), h, w)
	}
	if t.state.FileName == "" && len(t.state.Rows) == 0 {
		return clipBlockStyled(t.th, t.titleRow(w)+"\n"+t.emptyStateBody(), h, w)
	}
	t.table.SetWidth(w)
	t.table.SetHeight(max(h-1-tableGridChrome, 1))
	body := t.table.View()
	// Publish the DRAWN table box for the wheel hit map: measured from the
	// composed string, so the region is the ink the user sees.
	t.txRect = sectionRect(0, 1, body)
	// And the drawn rows for the click hit map: the table body starts
	// under the title row, so row rects shift down by one.
	t.selRows = selectRows(t.selRows, RegionTxTable, t.table.RowHits(), 0, 1)

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

// emptyStateBody renders "no tx file loaded — f to pick file" (key
// accent-styled, suffix muted). The key is read from the page's own
// PickFile binding, so the hint can never drift from the bound key.
func (t *Transactions) emptyStateBody() string {
	return t.th.TextMuted.Render(txEmptyHint+" "+dashIf(t.th, "")+" ") +
		t.th.Accent.Render(t.nav.PickFile.Keys()[0]) + " " + t.th.TextMuted.Render(txPickFileSuffix)
}

// errorBody renders why the last tx-file pick failed: error glyph + reason,
// so a rejected file names its problem instead of leaving the page silent.
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

// clipBlockStyled flattens a body to exactly h lines of at most maxW cells
// (truncate, never wrap; short bodies pad so joins stay aligned). Shared by
// the dashboard cards and the transactions page.
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
