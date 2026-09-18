// analyze_unparsable_view.go renders the §J unparsable-message reviewer: a
// read-only browser opened with [u] — the failure roster on the left; the
// sample's parsed fields plus a hexdump with the unparsed region marked on
// the right. Below frame.FullWidth the panes stack.
package pages

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
)

const (
	unparsableListW      = 52 // roster column width at full width
	unparsableOffsetW    = 9  // stream-offset column
	unparsableLenW       = 6  // byte-length column
	unparsableHexPerLine = 16 // bytes per hexdump line (the classic mainframe dump)
)

// unparsableWindow is the viewer geometry shared by the renderer and
// the scroll keys: the roster column width and the visible roster rows.
func (a *Analyze) unparsableWindow() (listW, rows int) {
	_, h := frame.ContentSize(a.width, a.height)

	return unparsableListW, max(h-6, 4)
}

// unparsableOverlay composes the viewer body for the content area.
func (a *Analyze) unparsableOverlay(w, h int) string {
	total := len(a.state.UnparsableRows)
	head := titleLine(a.th, "UNPARSABLE MESSAGES  showing first "+strconv.Itoa(total)+
		" of "+strconv.Itoa(a.state.Unparsable)) + "\n" +
		hintSpans(a.th, a.th.Dim, "j/k", "sample", "esc", "close")

	// Same pane budget as the generated-item picker: render clips the
	// overlay to h-2 lines and the two-line head consumes two, so the
	// panes get itemsPaneH rows.
	bodyH := itemsPaneH(h)
	var body string
	if w >= frame.FullWidth {
		lw := min(unparsableListW, max(w/2, 28))
		gap := " "
		pw := max(w-lw-len([]rune(gap)), 40)
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			a.unparsableRoster(lw, bodyH), gap,
			a.unparsableHexPane(pw, bodyH))
	} else {
		lh := max(bodyH/2, 4)
		ph := max(bodyH-lh, 3)
		body = a.unparsableRoster(w, lh) + "\n" + a.unparsableHexPane(w, ph)
	}

	return head + "\n" + body
}

// unparsableRoster draws the failure roster: cursor, offset, length and
// a truncated reason.
func (a *Analyze) unparsableRoster(w, h int) string {
	reasonW := max(w-unparsableOffsetW-unparsableLenW-4, 8)
	lines := make([]string, 0, h+1)
	lines = append(lines, a.th.Deemphasized.Render(
		padRight("  OFFSET", unparsableOffsetW)+padRight("LEN", unparsableLenW)+"REASON"))
	hidden := 0
	for i, r := range a.state.UnparsableRows {
		if i < a.unparsableOff || i >= a.unparsableOff+max(h-1, 1) {
			if i >= a.unparsableOff+h {
				hidden++
			}

			continue
		}
		cell := "  "
		if i == a.unparsableCursor {
			cell = a.pick("▸", ">") + " "
		}
		reason := truncateCells(r.Reason, reasonW, clipTail(a.th))
		line := cell + padRight(r.Offset, unparsableOffsetW) + padRight(r.Length, unparsableLenW) + reason
		if i == a.unparsableCursor {
			lines = append(lines, a.th.Accent.Render(clipCells(line, w, clipTail(a.th))))
		} else {
			lines = append(lines, a.th.TextPrimary.Render(clipCells(line, w, clipTail(a.th))))
		}
	}
	if hidden > 0 {
		lines = append(lines, a.th.Dim.Render("  +"+strconv.Itoa(hidden)+" more"))
	}

	return strings.Join(lines, "\n")
}

// unparsableHexPane draws the sample under the cursor: reason, the fields
// that unpacked before the failure, then a hexdump with unparsed bytes marked.
func (a *Analyze) unparsableHexPane(w, h int) string {
	if a.unparsableCursor >= len(a.state.UnparsableRows) {
		return a.th.Dim.Render("sample")
	}
	r := a.state.UnparsableRows[a.unparsableCursor]
	head := titleLine(a.th, "SAMPLE AT "+r.Offset+"  ("+r.Length+" bytes)") + "\n" +
		a.th.Dim.Render("reason: "+r.Reason)

	body := a.unparsableDescribe(r, w) + "\n\n" + a.unparsableHexdump(r, w)
	clipped := clipBlockStyled(a.th, plainBlock(body), max(h-2, 1), w)

	return head + "\n" + clipped
}

// unparsableDescribe lists the fields that unpacked before the failure in
// describe form; with nothing parsed it says so explicitly.
func (a *Analyze) unparsableDescribe(r AnalyzeUnparsableRow, w int) string {
	sep := a.th.Separator()
	if len(r.Fields) == 0 {
		return titleLine(a.th, "PARSED BEFORE FAILURE") + "\n" +
			a.th.Dim.Render("no field unpacked before the failure")
	}
	const idW, nameW = 3, 30
	valW := max(w-idW-nameW-3, 10)
	rows := make([]string, 0, len(r.Fields))
	for _, f := range r.Fields {
		id := padLeft(f.ID, idW)
		name := truncateCells(f.Name, nameW, clipTail(a.th))
		val := truncateCells(f.Value, valW, clipTail(a.th))
		rows = append(rows, a.th.TextMuted.Render(id+"  ")+
			a.th.TextPrimary.Render(padRight(name, nameW)+"  ")+
			a.th.Accent.Render(val))
	}

	return titleLine(a.th, "PARSED BEFORE FAILURE"+sep+strconv.Itoa(len(r.Fields))+" fields") + "\n" +
		strings.Join(rows, "\n")
}

// unparsableHexdump renders the captured head as classic hexdump lines
// with a one-line legend naming where the unparsed (marked) region
// begins.
func (a *Analyze) unparsableHexdump(r AnalyzeUnparsableRow, w int) string {
	return titleLine(a.th, "HEXDUMP") + "\n" +
		a.th.Dim.Render(a.unparsableStopNote(r)) + "\n" +
		strings.Join(a.unparsableHexLines(r, w), "\n")
}

// unparsableStopNote explains the marked region: the byte where parsing
// stopped, or why nothing is marked (stop unknown, or the whole shown
// window parsed and the failure is past it).
func (a *Analyze) unparsableStopNote(r AnalyzeUnparsableRow) string {
	switch {
	case r.FailedAt < 0:
		return "no marked region: stop offset unknown"
	case r.FailedAt >= len(r.Head):
		return "no marked region: all " + strconv.Itoa(len(r.Head)) + " shown bytes parsed (stop at byte " + strconv.Itoa(r.FailedAt) + ")"
	default:
		return "marked = unparsed from byte " + strconv.Itoa(r.FailedAt)
	}
}

// unparsableHexLines builds 16-bytes-per-line hexdump lines addressed from the
// sample's stream offset; bytes at or after FailedAt get the error style. The
// ASCII column is clipped to the pane.
func (a *Analyze) unparsableHexLines(r AnalyzeUnparsableRow, w int) []string {
	head := r.Head
	if len(head) == 0 {
		return nil
	}
	base, _ := strconv.ParseInt(r.Offset, 10, 64)
	lines := make([]string, 0, (len(head)+unparsableHexPerLine-1)/unparsableHexPerLine)
	for off := 0; off < len(head); off += unparsableHexPerLine {
		end := min(off+unparsableHexPerLine, len(head))
		chunk := head[off:end]

		var hexb, asciib strings.Builder
		for i := range unparsableHexPerLine {
			if i >= len(chunk) {
				hexb.WriteString("   ") // pad missing bytes for alignment
				continue
			}
			b := chunk[i]
			glyph := asciiByte(b)
			if r.FailedAt >= 0 && off+i >= r.FailedAt {
				hexb.WriteString(a.th.StatusError.Render(fmt.Sprintf("%02x", b)) + " ")
				asciib.WriteString(a.th.StatusError.Render(string(rune(glyph))))
			} else {
				fmt.Fprintf(&hexb, "%02x ", b)
				asciib.WriteByte(glyph)
			}
		}

		addr := fmt.Sprintf("%08x  ", base+int64(off))
		ascii := clipCells(asciib.String(), max(w-lipgloss.Width(addr)-unparsableHexPerLine*3-2, 1), clipTail(a.th))
		lines = append(lines, addr+hexb.String()+" |"+ascii+"|")
	}

	return lines
}

// asciiByte maps a byte to its ASCII glyph, '.' for non-printables.
func asciiByte(b byte) byte {
	if b >= 0x20 && b < 0x7f {
		return b
	}

	return '.'
}
