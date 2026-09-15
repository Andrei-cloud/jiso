// workers_view.go renders the §H body (wireframe §H): the WORKERS table
// (ID TYPE TRANSACTION STATUS THR INTERVAL/TPS RUNTIME OK/FAIL
// CIRCUIT — the Table truncates, never wraps), the TPS sparkline strip
// ("TPS w-2 ▁▂▃▅▆▇█ inst 118.4 · avg 96.2" plus the derived net line),
// and one progress Bar row per active worker (eighth-block fill, ascii
// # — the progress package's own degradation ladder sizes each row). At
// ≥ frame.FullWidth the TPS strip is its own line; below it the strip
// folds into the status line under the title (the wireframe's 80×24
// fallback). The stress summary overlay replaces the whole body while
// open (percentiles + RC breakdown + histogram sections). Everything is
// pre-derived display data; the only math here is bar sizing.
package pages

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/progress"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

const (
	titleWorkers = "WORKERS & STRESS"
	titleTps     = "TPS"
	titleProg    = "PROGRESS"

	// workersMinTableWidth is the worker Table's floor width.
	workersMinTableWidth = 40

	// summaryBarMax is the histogram bar's widest form in cells.
	summaryBarMax = 24
)

// workersEmptyHint names the next actions (wireframe empty state) with
// the b/t key glyphs pre-rendered in the HotKey style (UAT round 4:
// inline hotkeys must read as hotkeys, matching the footer's accented
// keys). The escape-stripped text is exactly "no workers - b
// background-send · t stress test" ("|" separator in ascii mode, keeping
// ascii goldens 7-bit). The Table re-renders the whole message in
// TextMuted; lipgloss v2 splices the outer style around pre-styled
// segments, so the glyphs keep their bold-accent.
func workersEmptyHint(th *theme.Theme) string {
	sep := th.Separator()

	return "no workers - " + th.Key("b") +
		th.TextMuted.Render(" background-send"+sep) +
		th.Key("t") + th.TextMuted.Render(" stress test")
}

// workersColumns are the §H columns; TRANSACTION is the flex column that
// gives first on narrow terminals (the Table's fit clamp keeps every
// line inside the pane and never wraps).
func workersColumns() []widgets.Column {
	return []widgets.Column{
		{Title: "ID", Width: 6},
		{Title: "TYPE", Width: 10},
		{Title: colTransaction, Width: 13, Flex: true},
		{Title: colStatus, Width: 17}, // ascii "[x] circuit-broke"
		// Counts of different digit counts, so they are laid out on their units.
		{Title: "THR", Width: 3, AlignRight: true},
		{Title: "INTERVAL/TPS", Width: 12},
		{Title: "RUNTIME", Width: 9},
		{Title: "OK/FAIL", Width: 10, AlignRight: true},
		// An empty circuit cell means the breaker has nothing to report; the cell
		// takes the same dash every other no-value cell in this table takes, so a
		// quiet column does not read as a table that failed to fill in.
		{Title: "CIRCUIT", Width: 15},
	}
}

// View renders the §H body for the frame's content area.
func (w *Workers) View() tea.View {
	wt, h := frame.ContentSize(w.width, w.height)

	return tea.NewView(w.render(wt, h))
}

// render lays out title + status line + table + TPS strip + progress rows
// (or the summary overlay), clipped to exactly h lines of at most wt-2 cells:
// every block gets the same width budget, so nothing draws wider than the
// table it is aligned under.
func (w *Workers) render(wt, h int) string {
	// inner is the single width budget every block on this page shares. The
	// table used to size itself at wt-2 while the status line, the TPS strip and
	// the PROGRESS rows clipped to the full content width, so a progress row
	// could run a cell past the right edge of the table it sits under.
	inner := max(wt-2, 4)
	w.tableRect = geom.Rect{} // the table re-publishes below, or not at all
	w.selRows = w.selRows[:0] // and so do its click rows

	head := titleLine(w.th, titleWorkers)
	if line := w.statusLine(inner); line != "" {
		head += "\n" + line
	}

	if w.summaryOpen && w.state.Summary != nil {
		return clipBlockStyled(w.th, head+"\n"+w.summaryBody(inner), h, inner)
	}

	// The TPS strip and PROGRESS rows are pure functions of the state,
	// so they are measured BEFORE the table: whatever vertical budget the
	// head, the strip, and the progress rows leave is the table's real
	// pane (Task 8.2c: the wheel window fills the pane instead of the
	// table drawing unbounded and being clipped mid-row).
	strip := w.tpsStrip(inner)
	rows := w.progressBlock(inner)
	paneH := max(h-(strings.Count(head, "\n")+1)-strHeight(strip)-strHeight(rows), tableGridChrome+1)

	w.table.SetWidth(inner)
	w.table.SetHeight(max(paneH-tableGridChrome, 1))
	body := w.table.View()
	// Publish the DRAWN table box for the wheel hit map (Task 8.2c):
	// measured from the composed string like every recorded section rect.
	headH := strings.Count(head, "\n") + 1
	w.tableRect = sectionRect(0, headH, body)
	// And the drawn rows for the click hit map (Task 8.3): the table
	// body starts under the head lines, so the widget's row rects shift
	// down by the head height into content coords.
	w.selRows = selectRows(w.selRows, RegionWorkersTable, w.table.RowHits(), 0, headH)

	if strip != "" {
		body += "\n" + strip
	}
	if rows != "" {
		body += "\n" + rows
	}

	return clipBlockStyled(w.th, head+"\n"+body, h, inner)
}

// strHeight is the drawn line count of an optional body block: an empty
// block draws nothing here (lipgloss.Height("") would count one phantom
// line and steal a table row from the pane budget).
func strHeight(s string) int {
	if s == "" {
		return 0
	}

	return lipgloss.Height(s)
}

// statusLine is the root-stamped action line (no-op notices, stop
// errors), plus the TPS strip folded in when the width has no room for
// its own line (the wireframe's "table only, TPS in status line").
func (w *Workers) statusLine(wt int) string {
	parts := make([]string, 0, 2)
	if w.state.StatusLine != "" {
		parts = append(parts, w.th.Status(theme.KindWarn, w.state.StatusLine))
	}
	if wt < frame.FullWidth {
		if strip := w.tpsText(); strip != "" {
			parts = append(parts, w.th.Deemphasized.Render(strip))
		}
	}
	if len(parts) == 0 {
		return ""
	}

	return clipCells(strings.Join(parts, "  "), wt, clipTail(w.th))
}

// tpsStrip is the standalone TPS line at ≥ frame.FullWidth.
func (w *Workers) tpsStrip(wt int) string {
	if wt < frame.FullWidth {
		return ""
	}
	text := w.tpsText()
	if text == "" {
		return ""
	}

	return clipCells(w.th.Deemphasized.Render(text), wt, clipTail(w.th))
}

// tpsText builds "TPS w-2 ▁▂▃ inst 118.4 · avg 96.2 net: …" from the
// root-derived levels/label/net ("" when there are no samples).
func (w *Workers) tpsText() string {
	st := w.state
	if len(st.Sparkline) == 0 && st.Net == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(titleTps)
	if st.SparkLabel != "" {
		b.WriteString(" " + st.SparkLabel)
	}
	if len(st.Sparkline) > 0 {
		b.WriteString(" " + RenderSparkline(w.th, st.Sparkline))
	}
	if st.Net != "" {
		if b.Len() > 0 {
			b.WriteString(w.sep())
		}
		b.WriteString(st.Net)
	}

	return b.String()
}

// progressBlock renders the PROGRESS section: one Bar row per active
// worker (superfile Processes pattern). Unknown totals (Pct < 0) render
// the degraded count line — never a spinner, which would need a clock
// the page does not own.
func (w *Workers) progressBlock(wt int) string {
	if len(w.state.Progress) == 0 {
		return ""
	}
	lines := make([]string, 0, len(w.state.Progress)+1)
	lines = append(lines, titleLine(w.th, titleProg))
	inner := max(wt-2, 8)
	for _, p := range w.state.Progress {
		lines = append(lines, clipCells(w.progressLine(p, inner), wt, clipTail(w.th)))
	}

	return strings.Join(lines, "\n")
}

// progressLine renders one row via the shared progress Bar: label +
// eighth-block track + percent, with the root-derived counts/ETA as the
// note the Bar drops first when the width gives out.
func (w *Workers) progressLine(p ProgressRow, width int) string {
	if p.Pct < 0 {
		return w.th.Deemphasized.Render(p.ID) + " " +
			w.th.TextPrimary.Render(dashIf(w.th, p.Counts))
	}

	bar := progress.NewBar(w.th, p.ID, 100)
	bar.Eighths = true
	bar.SetProgress(p.Pct, 100)
	note := strings.TrimSpace(p.Counts + w.sep() + p.ETA)
	bar.Note = note

	return bar.View(width)
}

// summaryBody renders the stress-summary overlay (UAT round 4: as
// informative as the legacy CLI table, in the page's boxed
// vocabulary): a title with worker id + status + success ratio, the
// RUN plan, LATENCY MS (percentiles + budget), RESPONSE CODES beside
// HISTOGRAM at wide widths, the PER TRANSACTION breakdown, and the
// esc affordance. Every value is a root-derived display string.
func (w *Workers) summaryBody(wt int) string {
	s := w.state.Summary
	var b strings.Builder

	title := titleLine(w.th, "STRESS SUMMARY")
	if s.WorkerID != "" {
		title += "  " + w.th.Deemphasized.Render(s.WorkerID)
	}
	if s.Status != "" {
		title += "  " + w.th.Deemphasized.Render(summarySep(w.th)) + "  " + w.statusCell(s.Status)
	}
	if s.Success != "" {
		kind := theme.KindWarn
		if s.Status == StatusCircuitBroke {
			kind = theme.KindError
		} else if strings.HasSuffix(s.Success, "100.0%") {
			kind = theme.KindOK
		}

		title += "  " + w.th.Deemphasized.Render(summarySep(w.th)) + "  " +
			w.th.Status(kind, s.Success+" ok")
	}
	b.WriteString(clipCells(title, wt, clipTail(w.th)) + "\n")

	if len(s.Run) > 0 {
		b.WriteString(w.summaryBox("RUN", w.kvRows(s.Run, wt-4), wt) + "\n")
	}
	for _, line := range s.Headline {
		b.WriteString(w.th.TextPrimary.Render(line) + "\n")
	}

	latency := w.kvGrid(s.Percentiles, wt-4)
	if len(s.Budget) > 0 {
		latency += "\n" + w.budgetLine(s.Budget, wt)
	}
	b.WriteString(w.summaryBox("LATENCY MS", latency, wt) + "\n")

	if len(s.RCs) > 0 && len(s.Histogram) > 0 && wt >= 96 {
		rcW := (wt - 1) / 2
		rcBox := w.summaryBox("RESPONSE CODES", w.kvRows(s.RCs, rcW-4), rcW)
		histBox := w.summaryBox("HISTOGRAM", w.histogramLines(wt-rcW-5), wt-rcW-1)
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, rcBox, " ", histBox) + "\n")
	} else {
		if len(s.RCs) > 0 {
			b.WriteString(w.summaryBox("RESPONSE CODES", w.kvRows(s.RCs, wt-4), wt) + "\n")
		}
		if len(s.Histogram) > 0 {
			b.WriteString(w.summaryBox("HISTOGRAM", w.histogramLines(wt-4), wt) + "\n")
		}
	}

	if len(s.TxRows) > 0 {
		b.WriteString(w.summaryBox("PER TRANSACTION", w.txLines(s.TxRows, wt-4), wt) + "\n")
	}

	b.WriteString(w.th.Deemphasized.Render("esc close"))

	return b.String()
}

// summarySep is the title's section separator.
func summarySep(th *theme.Theme) string {
	if th.ASCII {
		return "|"
	}

	return "\u25b8"
}

// summaryBox draws one titled rounded box around pre-styled body lines
// (the server page's sectionW idiom; body lines are clipped to the box
// interior). The frame comes from the shared widgets.Border accessor:
// the summary boxes vary in height with their pre-styled bodies, so
// they compose the one shared border with their own Width maths rather
// than a fixed-size Section.
func (w *Workers) summaryBox(title, body string, wt int) string {
	box := widgets.Border(w.th, false).
		Width(max(wt-2, 1)).
		Render(strings.TrimRight(body, "\n"))

	return titleLine(w.th, title) + "\n" + box
}

// kvRows lays label/value pairs one per line, the label column padded
// to the widest label (used for the RUN section's long labels).
func (w *Workers) kvRows(kvs []SummaryKV, wt int) string {
	labelW := 0
	for _, kv := range kvs {
		if n := lipgloss.Width(kv.Label); n > labelW {
			labelW = n
		}
	}
	labelW = min(labelW+1, 12)

	lines := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		lines = append(lines, clipCells(
			w.th.Deemphasized.Render(padRight(kv.Label, labelW))+w.th.TextPrimary.Render(kv.Value),
			max(wt, 8), clipTail(w.th)))
	}

	return strings.Join(lines, "\n")
}

// kvGrid lays label/value pairs out several per row when the width
// fits (the old kvGrid packing; short labels only).
func (w *Workers) kvGrid(kvs []SummaryKV, wt int) string {
	colLabel := 6
	colW := colLabel + 12
	perLine := max(wt/(colW+2), 1)

	var b strings.Builder
	for i, kv := range kvs {
		if i > 0 {
			if i%perLine == 0 {
				b.WriteString("\n")
			} else {
				b.WriteString("   ")
			}
		}
		b.WriteString(w.th.Deemphasized.Render(padRight(kv.Label, colLabel)))
		b.WriteString(w.th.TextPrimary.Render(kv.Value))
	}

	return b.String()
}

// budgetLine renders the latency-budget classification as one
// symbol+word row (never colour alone).
func (w *Workers) budgetLine(kvs []SummaryKV, wt int) string {
	kinds := map[string]theme.Kind{
		"satisfactory": theme.KindOK,
		"tolerable":    theme.KindWarn,
		"exceeded":     theme.KindError,
	}
	parts := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		kind, ok := kinds[kv.Label]
		if !ok {
			kind = theme.KindWarn
		}
		parts = append(parts, w.th.Status(kind, kv.Label+" "+kv.Value))
	}

	return clipCells(w.th.Deemphasized.Render("budget ")+strings.Join(parts, "  "), wt, clipTail(w.th))
}

// txLines renders the per-transaction breakdown rows: name column
// padded to the widest name (capped), then ok/err, mean, p99 and the
// per-tx response codes.
func (w *Workers) txLines(rows []SummaryTxRow, wt int) string {
	nameW := 0
	for _, r := range rows {
		if n := lipgloss.Width(r.Name); n > nameW {
			nameW = n
		}
	}
	nameW = min(nameW, 22)

	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		line := w.th.TextPrimary.Render(padRight(r.Name, nameW)) +
			"  " + w.th.Deemphasized.Render(r.OKFail) +
			"  " + w.th.Deemphasized.Render(r.Mean) +
			"  " + w.th.Deemphasized.Render(r.P99)
		if r.RCs != "" {
			line += "  " + w.th.Accent.Render(r.RCs)
		}

		lines = append(lines, clipCells(line, max(wt-4, 8), clipTail(w.th)))
	}

	return strings.Join(lines, "\n")
}

// histogram draws one bar row per latency bucket, bars relative to the
// largest count (full blocks; # under ascii).
func (w *Workers) histogramLines(inner int) string {
	maxCount := 0
	for _, h := range w.state.Summary.Histogram {
		if h.Count > maxCount {
			maxCount = h.Count
		}
	}
	labelW := 8
	barMax := min(summaryBarMax, max(inner-labelW-8, 4))
	full, empty := "\u2588", "\u2591"
	if w.th.ASCII {
		full, empty = "#", "."
	}

	lines := make([]string, 0, len(w.state.Summary.Histogram))
	for _, h := range w.state.Summary.Histogram {
		n := 0
		if maxCount > 0 {
			n = min(h.Count*barMax/maxCount, barMax)
		}
		bar := w.th.Accent.Render(strings.Repeat(full, n)) +
			w.th.Dim.Render(strings.Repeat(empty, barMax-n))
		lines = append(lines, clipCells(
			w.th.Deemphasized.Render(padRight(h.Label, labelW))+bar+
				w.th.TextPrimary.Render(" "+strconv.Itoa(h.Count)),
			max(inner, 8), clipTail(w.th)))
	}

	return strings.Join(lines, "\n")
}

// statusCell maps a canonical status token to its symbol+word cell
// (never color alone): running/done get the ok kind, ramping the warn
// kind, circuit-broke the error kind, stopped the info glyph dim.
func (w *Workers) statusCell(status string) string {
	switch status {
	case StatusRunning, StatusDone:
		return w.th.Status(theme.KindOK, status)
	case StatusRamping:
		return w.th.Status(theme.KindWarn, status)
	case StatusCircuitBroke:
		return w.th.Status(theme.KindError, status)
	case StatusStopped:
		return w.th.Deemphasized.Render(w.infoGlyph() + " " + status)
	default:
		return dashIf(w.th, status)
	}
}

// infoGlyph/dot pick the severity glyph for the theme's glyph mode.
func (w *Workers) infoGlyph() string { return pickGlyph(w.th, GlyphInfo, ASCIIInfo) }

// sep is the section separator, ASCII-fied under theme.ASCII.
func (w *Workers) sep() string { return w.th.Separator() }

// pickGlyph is the shared ascii/truecolor glyph picker.
func pickGlyph(th *theme.Theme, truecolor, ascii string) string {
	if th.ASCII {
		return ascii
	}

	return truecolor
}
