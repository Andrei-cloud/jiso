// ctf_records_view.go renders the §K record viewer (UAT round 6
// wireframe): EVERY record the write emits, in a bordered box with a
// position ruler above and below. Records are fixed-width Base II lines
// wider than any terminal, so the box is a two-dimensional window:
// records scroll vertically (↑↓/j/k, PgUp/PgDn) and the column window
// scrolls horizontally (←→/h/l) — the rulers re-anchor to the TRUE
// record positions of the visible window, so a character position is
// readable anywhere in the 168-column record.
package pages

import (
	"strconv"
	"strings"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

// Records-window geometry. The gutter holds the record number, the
// cursor cell the ▸, and both ruler rows share the text column so a
// digit always sits exactly above/below its own column.
const (
	ctfRecGutter  = 4 // "  45"
	ctfRecCursorC = 2 // "▸ "
	ctfRecMinCols = 8
)

// viewerSize is the records box's outer size: the overlay body minus
// the headline, status, and write lines.
func (c *Ctf) viewerSize() (w, h int) {
	cw, ch := frame.ContentSize(c.width, c.height)

	return cw, max(ch-3, 8)
}

// viewerGeom is the window geometry the renderer and the scroll keys
// share: text columns and visible record rows of the box.
func (c *Ctf) viewerGeom() (cols, rows int) {
	w, h := c.viewerSize()

	return max(w-4-ctfRecGutter-ctfRecCursorC, ctfRecMinCols), max(h-4, 1)
}

// recordLen is the width of the widest record (168 for Base II; the
// page does not import base2, it measures what root sent).
func (c *Ctf) recordLen() int {
	n := 0
	if c.state.Preview != nil {
		for _, r := range c.state.Preview.Records {
			if l := len([]rune(r)); l > n {
				n = l
			}
		}
	}

	return n
}

// maxColOff clamps the column window so its right edge never passes the
// record end.
func (c *Ctf) maxColOff() int {
	cols, _ := c.viewerGeom()

	return max(c.recordLen()-cols, 0)
}

// colStep is the ←→ jump: half a window, never zero.
func (c *Ctf) colStep() int {
	cols, _ := c.viewerGeom()

	return max(cols/2, ctfRecMinCols)
}

// scrollRecordsIntoView keeps the record cursor inside the vertical
// window after any navigation.
func (c *Ctf) scrollRecordsIntoView(rows int) {
	if rows <= 0 {
		return
	}
	if c.recCursor < c.recOff {
		c.recOff = c.recCursor
	}
	if c.recCursor >= c.recOff+rows {
		c.recOff = c.recCursor - rows + 1
	}
}

// ctfRuler builds the position ruler for the column window
// [off, off+cols) of 1-based record positions: a digit marker every 10
// positions, a + tick at each half-decade between, and - fill (the
// classic mainframe column ruler; pure ASCII so it survives theme.ASCII
// and colorless profiles unchanged).
func ctfRuler(off, cols int) string {
	b := make([]byte, 0, cols)
	for i := 0; i < cols; i++ {
		p := off + i // 0-based record position
		switch {
		case p%10 == 0:
			b = append(b, byte('0'+(p/10)%10))
		case p%5 == 0:
			b = append(b, '+')
		default:
			b = append(b, '-')
		}
	}

	return string(b)
}

// recordsBox renders the titled bordered box: top ruler, the visible
// record rows with their numbers and the cursor cell, bottom ruler.
// Both rulers and every record row share the gutter width, so the
// digit markers align with the record text columns exactly.
func (c *Ctf) recordsBox(w int) string {
	recs := c.state.Preview.Records
	total := len(recs)
	cols, rows := c.viewerGeom()

	first, last := 1, total
	if total > 0 {
		first = min(c.recOff+1, total)
		last = min(c.recOff+rows, total)
	}
	arrows := pickGlyph(c.th, "\u2190\u2192", "left/right")
	title := "RECORDS  " + strconv.Itoa(first) + "-" + strconv.Itoa(last) +
		joinSep(c.th) + pickGlyph(c.th, "\u2191\u2193", "up/down") + " record" +
		joinSep(c.th) + "PgUp/PgDn page" + joinSep(c.th) + arrows + " columns"

	// The rulers sit BEHIND the record gutter + cursor cell, so every
	// digit lands exactly above/below its own record column.
	rulerGutter := strings.Repeat(" ", ctfRecGutter+ctfRecCursorC)
	inner := rulerGutter + ctfRuler(c.colOff, cols)
	body := make([]string, 0, rows+2)
	body = append(body, c.th.Dim.Render(inner))
	for i := 0; i < rows; i++ {
		n := c.recOff + i
		if n >= total {
			break
		}
		body = append(body, c.recordRow(n, recs[n], cols))
	}
	body = append(body, c.th.Dim.Render(inner))

	return titleLine(c.th, title) + "\n" +
		widgets.Border(c.th, false).Width(max(w, 8)).Height(max(len(body), 1)+2).
			Render(clipBlockStyled(c.th, strings.Join(body, "\n"), max(len(body), 1), max(w-4, ctfRecGutter+ctfRecCursorC+ctfRecMinCols)))
}

// recordRow renders one record line: number gutter, cursor cell, and
// the column window of the record.
func (c *Ctf) recordRow(n int, rec string, cols int) string {
	gutter := padLeft(strconv.Itoa(n+1), ctfRecGutter)
	cell := strings.Repeat(" ", ctfRecCursorC)
	if n == c.recCursor {
		cell = pickGlyph(c.th, "\u25b8", ">") + " "
	}
	runes := []rune(rec)
	if c.colOff >= len(runes) {
		return c.th.Dim.Render(gutter) + cell
	}
	win := string(runes[c.colOff:min(len(runes), c.colOff+cols)])

	return c.th.Dim.Render(gutter) + cell + c.th.TextPrimary.Render(win)
}

// ctfRecStatus is the viewer status line: cursor position, the visible
// column range of the record, and the scroll hint (glyph-honest: the
// arrow pair degrades to words under theme.ASCII).
func (c *Ctf) ctfRecStatus() string {
	total := len(c.state.Preview.Records)
	cols, _ := c.viewerGeom()
	recN := min(c.recCursor+1, max(total, 1))
	colEnd := min(c.colOff+cols, max(c.recordLen(), 1))
	arrows := pickGlyph(c.th, "\u2190\u2192", "left/right")
	line := "rec " + strconv.Itoa(recN) + "/" + strconv.Itoa(total) +
		joinSep(c.th) + "cols " + strconv.Itoa(min(c.colOff+1, colEnd)) + "-" + strconv.Itoa(colEnd) +
		" of " + strconv.Itoa(c.recordLen()) +
		joinSep(c.th) + arrows + " shifts the column window (ruler re-anchors)"

	return clipCells(c.th.Dim.Render(line), c.recordBoxWidth(), clipTail(c.th))
}

// ctfRecWriteLine is the write/close line under the box; the §N3
// overwrite warning keeps its warn styling (root stamped the flag).
func (c *Ctf) ctfRecWriteLine() string {
	p := c.state.Preview
	line := "w write" + joinSep(c.th) + pickGlyph(c.th, "\u2192", "->") +
		" " + dashIf(c.th, p.OutPath) +
		" (" + strconv.Itoa(len(p.Records)) + " records)" + joinSep(c.th) + "esc close"
	if p.Overwrite {
		line = "target exists - w overwrites " + dashIf(c.th, p.OutPath) + joinSep(c.th) + "esc closes first"

		return clipCells(c.th.Status(theme.KindWarn, line), c.recordBoxWidth(), clipTail(c.th))
	}

	return clipCells(c.th.Deemphasized.Render(line), c.recordBoxWidth(), clipTail(c.th))
}

// recordBoxWidth is the content width the overlay lines clip to.
func (c *Ctf) recordBoxWidth() int {
	w, _ := frame.ContentSize(c.width, c.height)

	return w
}
