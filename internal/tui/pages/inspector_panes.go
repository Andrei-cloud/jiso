// inspector_panes.go renders the non-fields views: the packed-hex pane
// (root's pre-wrapped hex lines regrouped 8/16 bytes per `h` — pure
// display), the bitmap bit grid (set bits derived from Fields[].Num by
// page-local string logic, no ISO library), and the raw-json passthrough.
package pages

import (
	"fmt"
	"strings"
)

// hexBytesWide is the packed pane's fallback regrouping (bytes per
// line) for states that carry raw hex digits but no standard dump.
const hexBytesWide = 16

// bitmapCols is the bit grid's cells per row.
const bitmapCols = 8

// packedBody renders the HeaderNote line and the standard hexdump of
// the packed message (offset, 16 byte pairs, ASCII gutter — UAT: no
// grouping toggle, hex is the right pane). PackedDump is the
// authoritative source; a state carrying only raw PackedHex digits
// falls back to the 16-bytes-per-line regrouping without the gutter.
func (in *Inspector) packedBody(w, h int) string {
	lines := make([]string, 0, h)
	lines = append(lines, in.th.Deemphasized.Render(dashIf(in.th, in.state.HeaderNote)))

	if dump := in.state.PackedDump; len(dump) > 0 {
		for _, ln := range dump {
			offset, rest, ok := strings.Cut(ln, "  ")
			styled := in.th.TextPrimary.Render(ln)
			if ok {
				styled = in.th.Deemphasized.Render(offset+"  ") + in.th.TextPrimary.Render(rest)
			}
			lines = append(lines, clipCells(styled, max(w, 1), clipTail(in.th)))
		}

		return strings.Join(lines, "\n")
	}

	digits := flattenHex(in.state.PackedHex)
	for i, ln := range hexLines(digits, hexBytesWide) {
		offset := in.th.Deemphasized.Render(fmt.Sprintf("%08x", i*hexBytesWide))
		lines = append(lines, offset+"  "+in.th.TextPrimary.Render(ln))
	}
	if len(digits) == 0 {
		lines = append(lines, dashIf(in.th, ""))
	}

	return strings.Join(lines, "\n")
}

// flattenHex concatenates root's pre-wrapped hex lines, dropping the
// grouping spaces root used.
func flattenHex(lines []string) string {
	var b strings.Builder
	for _, ln := range lines {
		for _, r := range ln {
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
				b.WriteRune(r)
			}
		}
	}

	return b.String()
}

// hexLines regroups a hex-digit string into lines of per bytes
// (2 hex digits each), 4-hex-digit groups separated by spaces.
func hexLines(digits string, per int) []string {
	per = max(per, 1)
	lineLen := per * 2
	out := make([]string, 0, (len(digits)+lineLen-1)/lineLen)
	for i := 0; i < len(digits); i += lineLen {
		chunk := digits[i:min(i+lineLen, len(digits))]
		groups := make([]string, 0, per)
		for j := 0; j < len(chunk); j += 4 {
			groups = append(groups, chunk[j:min(j+4, len(chunk))])
		}
		out = append(out, strings.Join(groups, " "))
	}

	return out
}

// bitmapBody renders the 64/128-cell bit grid: one line per row of
// cells with set bits marked [x] (accent) and clear bits [ ] (dim) —
// the mark, not the colour, carries the state. Cells per row adapt to
// the pane width (8 wide, 6, 4, or 2 narrow) so rows never clip.
func (in *Inspector) bitmapBody(w int) string {
	set := in.bitNums()
	total := 64
	for n := range set {
		if n > 64 {
			total = 128

			break
		}
	}

	cols := bitmapCols
	for cols > 2 && bitmapRowWidth(cols) > w {
		cols -= 2
	}

	sep := in.th.Deemphasized.Render(" " + midDot(in.th) + " ")
	lines := make([]string, 0, total/cols+2)
	lines = append(lines, in.th.Deemphasized.Render("bitmap: [x] set")+sep+
		in.th.Deemphasized.Render("[ ] clear")+sep+
		in.th.Deemphasized.Render("MTI is not a bit"))
	for base := 1; base <= total; base += cols {
		cells := make([]string, 0, cols)
		for b := base; b < base+cols; b++ {
			cell := fmt.Sprintf("%02d", b)
			if set[b] {
				cell += in.th.Accent.Render("[x]")
			} else {
				cell += in.th.Dim.Render("[ ]")
			}
			cells = append(cells, cell)
		}
		end := min(base+cols-1, total)
		label := in.th.Deemphasized.Render(fmt.Sprintf("%d-%d ", base, end))
		lines = append(lines, label+strings.Join(cells, " "))
	}

	return strings.Join(lines, "\n")
}

// bitmapRowWidth is the rendered width of a grid row with cols cells
// (worst-case three-digit range label).
func bitmapRowWidth(cols int) int {
	return 8 + cols*5 + (cols - 1)
}

// rawBody renders the root-serialized inspect JSON verbatim (dash when
// root provided none).
func (in *Inspector) rawBody() string {
	if strings.TrimSpace(in.state.RawJSON) == "" {
		return dashIf(in.th, "")
	}

	return strings.Join(strings.Split(in.state.RawJSON, "\n"), "\n")
}
