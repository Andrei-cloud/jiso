// connect_view.go renders the §E dialog: an accent title over a bordered
// box, one line per field (fields wrap onto continuation lines; disabled
// rows dim), and an in-flight progress line that replaces the whole form.
package pages

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

const (
	// connectBoxWidth is the dialog box width.
	connectBoxWidth = 62
	// connectLabelCol is the label column inside the box: the longest
	// form label ("Station ID") plus the two-cell gap.
	connectLabelCol = 12
	// connectValueW is the column a noted value is padded to, so the notes that
	// explain a field line up. It is a floor, never a clip: see fieldLine.
	connectValueW = 16
	// connectBoxMin clamps the box for absurdly narrow terminals; the
	// frame's too-small floor takes over below its own minimum anyway.
	connectBoxMin = 44
)

// View renders the overlay body the root composes over the content area.
func (d *ConnectDialog) View() string {
	w := d.width
	if w <= 0 {
		w = frame.FallbackWidth
	}

	bw := max(min(connectBoxWidth, w-2), connectBoxMin)
	// In lipgloss v2 Width is the total box width (borders included),
	// so the content column is the box width minus the two border columns.
	inner := bw - 2

	var body string
	switch {
	case d.state.InFlight:
		body = d.progressBody(inner)
	default:
		body = d.formBody(inner)
	}

	title := d.state.Title
	if title == "" {
		title = "CONNECT"
	}

	return titleLine(d.th, title) + "\n" +
		widgets.Border(d.th, false).Width(bw).Render(body)
}

// FieldRowHits reports the drawn field rows relative to the dialog's own
// View origin: every line the field's fieldLine drew belongs to its rect
// (hit = drawn ink), while the picker overlay and error lines belong to
// no field and the in-flight body publishes nothing. The root translates
// these into the absolute cells its hit map resolves.
func (d *ConnectDialog) FieldRowHits() []widgets.RowHit {
	if d.state.InFlight || len(d.state.Fields) == 0 {
		return nil
	}
	w := d.width
	if w <= 0 {
		w = frame.FallbackWidth
	}
	bw := max(min(connectBoxWidth, w-2), connectBoxMin)
	inner := bw - 2

	out := make([]widgets.RowHit, 0, len(d.state.Fields))
	y := 2 // the title line and the box's top border sit above the body
	for i, f := range d.state.Fields {
		h := lipgloss.Height(d.fieldLine(i == d.focus, f, inner))
		out = append(out, widgets.RowHit{
			Rect:  geom.Rect{X: 1, Y: y, W: inner, H: h},
			Index: i,
		})
		y += h
		if d.pickerOpen && f.Kind == FieldPicker {
			// The overlay under the picker row is the overlay's own ink
			// and occupies exactly Height(pickerBox) lines — the '\n'
			// written before it adds no blank line — so the fields below
			// it keep their rects on their own drawn rows.
			y += lipgloss.Height(d.pickerBox(inner))
		}
	}

	return out
}

// formBody renders the editable form, optional error line, and the
// [Enter]/[Esc] footer.
// formFields renders the form's fields, an open picker, and the
// root-stamped error — with no key line. Callers own their footer; the
// wizard step that embeds these fields must not inherit a second key line.
func (d *ConnectDialog) formFields(inner int) string {
	var b strings.Builder
	for i, f := range d.state.Fields {
		if i > 0 {
			b.WriteByte('\n')
		}

		b.WriteString(d.fieldLine(i == d.focus, f, inner))
		if d.pickerOpen && f.Kind == FieldPicker {
			b.WriteByte('\n')
			b.WriteString(d.pickerBox(inner))
		}
	}
	if d.state.Error != "" {
		b.WriteByte('\n')
		b.WriteString(clipCells(d.th.Status(theme.KindError, d.state.Error), inner, clipTail(d.th)))
	}
	return b.String()
}

// formBody is the dialog's own view: the fields over the key line.
func (d *ConnectDialog) formBody(inner int) string {
	return d.formFields(inner) + "\n" + d.footer(inner, true)
}

// progressBody renders the shrunken in-flight box: the root-stamped
// attempt/backoff line and the cancel footer.
func (d *ConnectDialog) progressBody(inner int) string {
	return d.progressLines(inner) + "\n" + d.footer(inner, false)
}

// progressLines is the in-flight attempt line with no key line, for the wizard step
// that draws its own (see formFields).
func (d *ConnectDialog) progressLines(inner int) string {
	line := d.state.Progress
	if d.state.Backoff != "" {
		line += d.sep() + d.state.Backoff
	}

	return clipCells(line, inner, clipTail(d.th))
}

// footer right-aligns the dialog's own key line inside the box; in
// flight only Esc remains.
func (d *ConnectDialog) footer(inner int, enter bool) string {
	base := d.th.Deemphasized
	keys := keySpan(d.th, base, "Esc", "cancel")
	if enter {
		label := d.state.EnterLabel
		if label == "" {
			label = "connect"
		}
		keys = keySpan(d.th, base, "Enter", label) + base.Render("   ") + keys
	}
	if f := d.focused(); enter && f != nil && f.Browsable {
		keys = keySpan(d.th, base, "f", "browse") + base.Render("   ") + keys
	}
	return keyLine(keys, inner)
}

// keyLine right-aligns a dialog's key line inside a box of inner cells,
// inset from the border so the last cell never reads as part of the frame.
func keyLine(keys string, inner int) string {
	return strings.Repeat(" ", max(inner-footerInset-lipgloss.Width(keys), 0)) + keys
}

// footerInset is the breathing room between a right-aligned key line and the box
// border it sits in.
const footerInset = 2

// fieldLine renders one row: focus marker, label, and the kind-specific
// input part; disabled rows are fully dim (Enabled is root-stamped data).
func (d *ConnectDialog) fieldLine(focused bool, f FormField, inner int) string {
	marker := "  "
	if focused {
		marker = d.selectMarker() + " "
	}
	label := padRight(f.Label, connectLabelCol)
	valueCol := max(inner-connectLabelCol-2, 8)

	var part string
	switch f.Kind {
	case FieldRadio:
		part = d.radioPart(f, valueCol)
	case FieldChecklist:
		part = d.checklistPart(f, valueCol)
	case FieldPicker:
		part = d.pickerPart(f, focused)
	default:
		part = d.textPart(f, focused, valueCol)
		if f.Note != "" {
			// The note is the row's second column. The column pads rather
			// than clips: a long value keeps every cell and pushes its
			// note along — hiding which TLS file is picked is worse than
			// one note sitting right.
			part = padRight(part, connectValueW)
		}
	}
	part = d.appendNote(part, f)

	head := marker + label + part
	if f.Enabled {
		return head
	}

	return clipCells(d.th.Deemphasized.Render(head), inner, clipTail(d.th))
}

// radioPart renders "● Caller   ○ Listener", wrapping onto continuation
// lines indented to the value column when the row overflows.
func (d *ConnectDialog) radioPart(f FormField, valueCol int) string {
	on, off := d.radioOn(), d.radioOff()
	parts := make([]string, 0, len(f.Options))
	for i, o := range f.Options {
		glyph := off
		if i == f.Selected {
			glyph = on
		}
		parts = append(parts, glyph+" "+o)
	}
	if len(parts) == 0 {
		return ""
	}

	indent := strings.Repeat(" ", connectLabelCol+2)
	lines := []string{parts[0]}
	cur := lipgloss.Width(parts[0])
	for _, p := range parts[1:] {
		if cur+3+lipgloss.Width(p) > valueCol {
			lines = append(lines, p)
			cur = lipgloss.Width(p)

			continue
		}
		lines[len(lines)-1] += "   " + p
		cur += 3 + lipgloss.Width(p)
	}
	for i := 1; i < len(lines); i++ {
		lines[i] = indent + lines[i]
	}

	return strings.Join(lines, "\n")
}

// checklistPart renders the multi-select grid (▸ marks the cursor row,
// boxes wrap onto continuation lines like the radio part), followed by a
// dim "N selected" line.
func (d *ConnectDialog) checklistPart(f FormField, valueCol int) string {
	if len(f.Options) == 0 {
		return d.th.Deemphasized.Render("no transactions loaded")
	}

	indent := strings.Repeat(" ", connectLabelCol+2)
	lines := []string{}
	cur := 0
	selected := 0
	for i, o := range f.Options {
		ticked := f.checkedAt(i)
		if ticked {
			selected++
		}
		box := theme.BoxChecked
		if !ticked {
			box = theme.BoxUnchecked
		}
		marker := "  "
		if i == f.Selected {
			marker = d.selectMarker() + " "
		}
		item := marker + box + o
		if cur+lipgloss.Width(item) > valueCol && len(lines) > 0 {
			lines = append(lines, indent+item)
			cur = lipgloss.Width(indent) + lipgloss.Width(item)

			continue
		}
		if len(lines) == 0 {
			lines = append(lines, item)
		} else {
			lines[len(lines)-1] += "  " + strings.TrimLeft(item, " ")
		}
		cur += lipgloss.Width(item) + 2
	}
	lines = append(lines, indent+d.th.Deemphasized.Render(
		strconv.Itoa(selected)+" selected"))

	return strings.Join(lines, "\n")
}

// pickerPart renders the collapsed picker row: the selected value (accent
// cursor while focused) plus the dim ▸ open affordance.
func (d *ConnectDialog) pickerPart(f FormField, focused bool) string {
	v := f.Value
	if focused {
		v += d.cursor()
	}

	// The browse marker is the row's second column: padded, never clipped
	// (see fieldLine), so picker and text rows read as one table.
	return padRight(v, connectValueW) + d.th.Deemphasized.Render("  "+d.pick("▸", ">"))
}

// pickerBox renders the nested header-format overlay: a bordered list
// inset inside the dialog box, spliced under the picker row. The title
// carries the live filter and match count; Enter picks, Esc closes
// keeping the old value.
func (d *ConnectDialog) pickerBox(inner int) string {
	f := d.focused()
	if f == nil || f.Kind != FieldPicker {
		f = d.pickerField()
	}
	if f == nil {
		return ""
	}

	bw := max(inner-4, 24)
	boxInner := bw - 2

	title := strings.ToUpper(f.Label)
	if d.pickerFilter != "" {
		title += " " + d.pick("▏", "|") + d.pickerFilter +
			" (" + strconv.Itoa(len(d.pickerFiltered())) + ")"
	}

	nameCol := 10
	lines := make([]string, 0, len(f.Options)+1)
	idx := d.pickerFiltered()
	for sel, i := range idx {
		marker := "  "
		if sel == d.pickerSel {
			marker = d.selectMarker() + " "
		}
		line := marker + padRight(f.Options[i], nameCol)
		if h := HeaderHint(f.Options[i]); h != "" {
			line += d.th.Deemphasized.Render(h)
		}
		lines = append(lines, clipCells(line, boxInner, clipTail(d.th)))
	}
	if len(lines) == 0 {
		lines = append(lines, d.th.Deemphasized.Render(clipCells("no match", boxInner, clipTail(d.th))))
	}
	lines = append(lines, d.th.Deemphasized.Render(
		clipCells("j/k move  enter pick  esc back", boxInner, clipTail(d.th))))

	head := titleLine(d.th, title)

	return head + "\n" + widgets.Border(d.th, false).Width(bw).Render(strings.Join(lines, "\n"))
}

// pickerField returns the field backing the open overlay even while focus
// sits elsewhere (defensive; focus never leaves the picker row while open).
func (d *ConnectDialog) pickerField() *FormField {
	for i := range d.state.Fields {
		if d.state.Fields[i].Kind == FieldPicker {
			return &d.state.Fields[i]
		}
	}

	return nil
}

// textPart renders the value (accent cursor while focused) for text fields.
func (d *ConnectDialog) textPart(f FormField, focused bool, valueCol int) string {
	v := f.Value
	if focused {
		v += d.cursor()
	}

	return clipCells(v, valueCol, clipTail(d.th))
}

// appendNote adds the root-stamped note: status-symbol kinds (TLS loaded
// ✓/✗) via theme.Status, informational hints dim.
func (d *ConnectDialog) appendNote(part string, f FormField) string {
	switch {
	case f.Note == "":
		return part
	case f.NoteKind == NoteInfo:
		return part + d.th.Deemphasized.Render("  "+f.Note)
	case f.NoteKind == NotePass:
		return part + "  " + d.th.Status(theme.KindOK, f.Note)
	case f.NoteKind == NoteFail:
		return part + "  " + d.th.Status(theme.KindError, f.Note)
	default:
		return part + "  " + f.Note
	}
}

// Radio/selection glyphs, ASCII-fied under theme.ASCII so every ascii
// golden stays escape- and non-ASCII-free.
func (d *ConnectDialog) radioOn() string {
	return d.pick("●", "(*)")
}

func (d *ConnectDialog) radioOff() string {
	return d.pick("○", "( )")
}

func (d *ConnectDialog) selectMarker() string {
	return d.pick("▸", ">")
}

func (d *ConnectDialog) cursor() string {
	return d.pick("▏", "|")
}

func (d *ConnectDialog) sep() string {
	return d.th.Separator()
}

// pick returns the ascii form under theme.ASCII, the truecolor form
// otherwise (never color/glyph alone: every state pairs symbol + word).
func (d *ConnectDialog) pick(truecolor, ascii string) string {
	if d.th.ASCII {
		return ascii
	}

	return truecolor
}

// padRight pads s to width w with spaces (labels only; values clip).
func padRight(s string, w int) string {
	if lipgloss.Width(s) >= w {
		return s + " "
	}

	return s + strings.Repeat(" ", w-lipgloss.Width(s))
}
