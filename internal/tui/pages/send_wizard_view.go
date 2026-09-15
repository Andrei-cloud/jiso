// send_wizard_view.go renders the send wizard (proposal 04 §B): a SEND
// title over the standard 60-col modal box, a step rail ("1 spec ▸ 2 file
// ▸ 3 send", the current step emphasized), the step body (embedded connect
// form / filtered list / template list + target line) and the right-aligned
// key footer. The connect step renders the embedded dialog's form body
// directly — no nested CONNECT title — so the wizard stays one box.
package pages

import (
	"strconv"
	"strings"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/geom"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

const (
	wizardBoxWidth = 60
	wizardBoxMin   = 44
)

// View renders the overlay body the root composes over the content area.
func (w *SendWizard) View() string {
	width := w.width
	if width <= 0 {
		width = frame.FallbackWidth
	}

	bw := max(min(wizardBoxWidth, width-2), wizardBoxMin)
	inner := bw - 2

	body := w.rail(inner) + "\n" + w.stepBody(inner)
	if w.state.Error != "" {
		body += "\n" + clipCells(w.th.Status(theme.KindError, w.state.Error), inner, clipTail(w.th))
	}
	body += "\n" + w.footer(inner)

	return titleLine(w.th, "SEND") + "\n" + widgets.Border(w.th, false).Width(bw).Render(body)
}

// rail renders the step line: numbered labels with the current step
// emphasized and the rest dim, joined by the ▸ (ascii ">") separator.
func (w *SendWizard) rail(inner int) string {
	parts := make([]string, 0, len(w.state.Steps))
	for i, s := range w.state.Steps {
		label := strconv.Itoa(i+1) + " " + s
		if i == w.step {
			parts = append(parts, w.th.Accent.Render(label))

			continue
		}
		parts = append(parts, w.th.Deemphasized.Render(label))
	}
	sep := w.pick(" ▸ ", " > ")

	return clipCells(strings.Join(parts, sep), inner, clipTail(w.th))
}

// RailRowHits reports the rail's drawn step-label spans relative to the
// wizard's own View origin (the FilePicker.RowHits doctrine, UAT round 8
// Task 8.5 click-to-focus): one cell tall on the rail line — the title
// line and the box's top border sit above it — clipped to the width rail
// clips to (a label past the clip drew no ink and publishes no hit). The
// root centers the composed View and translates these into the absolute
// cells its hit map resolves.
func (w *SendWizard) RailRowHits() []widgets.RowHit {
	width := w.width
	if width <= 0 {
		width = frame.FallbackWidth
	}
	bw := max(min(wizardBoxWidth, width-2), wizardBoxMin)
	inner := bw - 2

	spans := railSpans("", w.state.Steps, w.pick(" \u25b8 ", " > "), inner)
	out := make([]widgets.RowHit, 0, len(spans))
	for _, s := range spans {
		out = append(out, widgets.RowHit{
			Rect:  geom.Rect{X: 1 + s.X, Y: 2, W: s.W, H: 1},
			Index: s.Index,
		})
	}

	return out
}

// stepBody renders the current step's content.
func (w *SendWizard) stepBody(inner int) string {
	switch w.currentStep() {
	case WizardStepConnect:
		if w.dlg != nil {
			if w.dlg.state.InFlight {
				return w.dlg.progressLines(inner - 2)
			}

			return w.dlg.formFields(inner - 2)
		}
	case WizardStepSpec:
		return w.listBody(w.state.SpecItems, inner)
	case WizardStepFile:
		return w.listBody(w.state.FileItems, inner)
	case WizardStepSend:
		return w.templateBody(inner)
	}

	return ""
}

// listBody renders a filtered file list: filter line, bordered list with
// the cursor marker, dim hints and the "← current" tag.
func (w *SendWizard) listBody(items []WizardItem, inner int) string {
	idx := w.filteredItems(items)
	head := w.filterLine(inner)
	if len(idx) == 0 {
		base := w.th.Deemphasized

		return head + "\n" + clipCells(base.Render("nothing to pick \u2014 [")+
			w.th.Key("f")+base.Render("] browse or type a path"), inner, clipTail(w.th))
	}

	lines := make([]string, 0, len(idx))
	for sel, i := range idx {
		it := items[i]
		marker := "  "
		if sel == w.sel {
			marker = w.pick("▸", ">") + " "
		}
		line := marker + clipCells(it.Label, 24, clipTail(w.th))
		if it.Hint != "" {
			line += "  " + w.th.Deemphasized.Render(clipCells(it.Hint, 14, clipTail(w.th)))
		}
		if it.Current {
			line += "  " + w.th.Deemphasized.Render(w.pick("← current", "<- current"))
		}
		lines = append(lines, clipCells(line, inner-2, clipTail(w.th)))
	}

	return head + "\n" + widgets.Border(w.th, false).Width(inner).Render(strings.Join(lines, "\n"))
}

// templateBody renders the send step: the template list (name, MTI, masked
// PAN, amount — description as fallback) and the target/connection line.
func (w *SendWizard) templateBody(inner int) string {
	idx := w.filteredTemplates()
	head := w.filterLine(inner)
	if len(idx) == 0 {
		base := w.th.Deemphasized

		return head + "\n" + clipCells(base.Render("no templates \u2014 [")+
			w.th.Key("f")+base.Render("] browse for a tx file"), inner, clipTail(w.th))
	}

	lines := make([]string, 0, len(idx))
	for sel, i := range idx {
		t := w.state.Templates[i]
		marker := "  "
		if sel == w.sel {
			marker = w.pick("▸", ">") + " "
		}
		cells := []string{clipCells(t.Name, 20, clipTail(w.th))}
		if t.MTI != "" {
			cells = append(cells, clipCells(t.MTI, 4, clipTail(w.th)))
		}
		if t.PAN != "" {
			// The masked PAN arrives from root with a Unicode mid-elision
			// ("411111~1111" under the ASCII set); root masks it through the
			// root-baked glyph in state.
			cells = append(cells, clipCells(t.PAN, 16, clipTail(w.th)))
		}
		if t.Amount != "" {
			cells = append(cells, clipCells(t.Amount, 10, clipTail(w.th)))
		}
		if t.MTI+t.PAN+t.Amount == "" && t.Description != "" {
			cells = append(cells, w.th.Deemphasized.Render(clipCells(t.Description, 18, clipTail(w.th))))
		}
		lines = append(lines, clipCells(marker+strings.Join(cells, "  "), inner-2, clipTail(w.th)))
	}

	target := "target " + w.state.Target
	if w.state.TargetOK {
		// th.Status already stamps the ok symbol; the text stays bare
		// (a manual check would render "✓ ✓").
		target += "  " + w.th.Status(theme.KindOK, w.pick("connected", "connected"))
	} else {
		target += "  " + w.th.Status(theme.KindError, w.pick("✗ offline", "offline"))
	}

	return head + "\n" + widgets.Border(w.th, false).Width(inner).Render(strings.Join(lines, "\n")) +
		"\n" + clipCells(w.th.Deemphasized.Render(target), inner, clipTail(w.th))
}

// filterLine shows the live filter with the accent cursor and the match
// count (the palette idiom); empty filters render the bare caret.
func (w *SendWizard) filterLine(inner int) string {
	caret := w.pick("▏", "|")

	return clipCells("filter: "+w.th.Accent.Render(w.filter+caret), inner, clipTail(w.th))
}

// footer right-aligns the step's key line: [Enter] advances (connect /
// next / send), [Esc] steps back or cancels.
func (w *SendWizard) footer(inner int) string {
	base := w.th.Deemphasized
	var enter string
	switch w.currentStep() {
	case WizardStepConnect:
		enter = keySpan(w.th, base, "Enter", "connect")
	case WizardStepSend:
		enter = keySpan(w.th, base, "Enter", "send")
	default:
		enter = keySpan(w.th, base, "Enter", "next")
	}
	back := "cancel"
	if w.step != 0 {
		back = "back"
	}
	keys := enter + base.Render("   ")
	switch w.currentStep() {
	case WizardStepSpec, WizardStepFile:
		keys += keySpan(w.th, base, "f", "browse") + base.Render("   ")
	}
	keys += keySpan(w.th, base, "Esc", back)
	return keyLine(keys, inner)
}

// pick returns the ascii form under theme.ASCII, the truecolor form
// otherwise (same rule as the connect dialog).
func (w *SendWizard) pick(truecolor, ascii string) string {
	if w.th.ASCII {
		return ascii
	}

	return truecolor
}
