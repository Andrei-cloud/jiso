// analyze_view.go renders the §J body as a wizard in the send wizard's
// visual vocabulary: a "PCAP ANALYZE  1 capture ▸ 2 spec ▸ 3 header ▸
// 4 run" rail (current step accented, the rest dim), the step body
// (▸-cursor candidate lists with a filter line that doubles as a typed
// path, the radio-style header list, and the run step's summary with
// the folded inline option rows), and a right-aligned key footer. The
// run step folds the small choices (goal t/r/s, the security toggle,
// the "/" flow filter) into inline rows and renders the status line and
// the results preview under them. EVERY line is clipped to the content
// inner width (clipCells + clipTail): no fragment may leak past the
// frame at any width ≥ the frame minimum.
package pages

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"jiso/internal/tui/frame"
	"jiso/internal/tui/theme"
	"jiso/internal/tui/widgets"
)

const (
	titleAnalyze = "PCAP ANALYZE"

	// analyzeListLabelWidth is the label column of the run step's
	// summary/inline rows; analyzeListMaxRows caps a candidate list and
	// the flows block so the footer stays on screen.
	analyzeListLabelWidth = 9
	analyzeListMaxRows    = 12
)

// View renders the §J body for the frame's content area.
func (a *Analyze) View() tea.View {
	w, h := frame.ContentSize(a.width, a.height)

	return tea.NewView(a.render(w, h))
}

// render lays out rail + step body + footer line, clipped to exactly h
// lines of at most w cells.
func (a *Analyze) render(w, h int) string {
	head := a.railLine(w)
	foot := a.footerLine(w)

	bodyH := max(h-2, 3)
	body := clipBlockStyled(a.th, a.stepBody(w), bodyH, w)
	if a.itemsOpen && len(a.state.Items) > 0 {
		// The generated-item picker overlays the step body (UAT round 6)
		// and owns the keyboard; its own hint line replaces the footer.
		body = clipBlockStyled(a.th, a.itemsOverlay(w, h), bodyH, w)
		foot = a.pickerFooterLine(w)
	} else if a.unparsableOpen && len(a.state.UnparsableRows) > 0 {
		// The unparsable-message hexdump reviewer (UAT round 6): opened
		// with [u], read-only, own key line.
		body = clipBlockStyled(a.th, a.unparsableOverlay(w, h), bodyH, w)
		foot = a.unparsableFooterLine(w)
	}

	return clipBlockStyled(a.th, head+"\n"+body+"\n"+foot, h, w)
}

// unparsableFooterLine is the viewer's key line (replaces the step
// footer while it is open).
func (a *Analyze) unparsableFooterLine(w int) string {
	sep := a.th.Separator()
	line := "[j/k] sample" + sep + "[pgup/pgdn] page" + sep + "[esc] close"

	return clipCells(a.th.Dim.Render(line), w, clipTail(a.th))
}

// pickerFooterLine is the picker's key line (replaces the step footer
// while the overlay is open).
func (a *Analyze) pickerFooterLine(w int) string {
	sep := a.th.Separator()
	line := "[" + "space" + "] include" + sep + "[a] all/none" + sep +
		"[enter] apply" + sep + "[esc] close (discard)"

	return clipCells(a.th.Dim.Render(line), w, clipTail(a.th))
}

// railLine is the wizard rail: the accent title plus the numbered step
// labels joined by ▸, the current step accented and the rest dim.
func (a *Analyze) railLine(w int) string {
	// three sections at most: the title, the step body and the footer
	parts := make([]string, 0, 3)
	parts = append(parts, titleLine(a.th, titleAnalyze))
	labels := make([]string, 0, StepCount)
	for i := 0; i < StepCount; i++ {
		label := strconv.Itoa(i+1) + " " + StepNames[i]
		if i == a.state.Step {
			labels = append(labels, a.th.Accent.Render(label))
		} else {
			labels = append(labels, a.th.Dim.Render(label))
		}
	}
	parts = append(parts, strings.Join(labels, a.pick(" \u25b8 ", " > ")))

	return clipCells(strings.Join(parts, "  "), w, clipTail(a.th))
}

// footerLine is the step's key line (the send wizard's footer
// vocabulary), right-aligned and clipped.
func (a *Analyze) footerLine(w int) string {
	base := a.th.Deemphasized
	var keys string
	switch a.state.Step {
	case StepCapture:
		keys = keySpan(a.th, base, "Enter", "next") + base.Render("   ") +
			keySpan(a.th, base, "f", "browse") + base.Render("   ") + a.escHint()
	case StepSpec, StepHeader:
		keys = keySpan(a.th, base, "Enter", "next") + base.Render("   ") + a.escHint()
	case StepRun:
		if a.filtering {
			keys = keySpan(a.th, base, "Enter", "run") + base.Render("   ") +
				keySpan(a.th, base, "Esc", "clear filter")
		} else {
			keys = keySpan(a.th, base, "Enter", "run") + base.Render("   ") +
				keySpan(a.th, base, "t/r/s", "goal") + base.Render("   ") +
				keySpan(a.th, base, "m", "security") + base.Render("   ") +
				keySpan(a.th, base, "/", "filter") + base.Render("   ") +
				keySpan(a.th, base, "w", "write") + base.Render("   ") + a.escHint()
		}
	}

	return clipCells(keys, w, clipTail(a.th))
}

// escHint names Esc for the current step: cancel on the first step,
// back everywhere else.
func (a *Analyze) escHint() string {
	if a.state.Step == StepCapture {
		return keySpan(a.th, a.th.Deemphasized, "Esc", "cancel")
	}

	return keySpan(a.th, a.th.Deemphasized, "Esc", "back")
}

// stepBody renders the current step's body into the content area.
func (a *Analyze) stepBody(w int) string {
	switch a.state.Step {
	case StepCapture:
		return a.captureBody(w)
	case StepSpec:
		return a.specBody(w)
	case StepHeader:
		return a.headersBody(w)
	case StepRun:
		return a.runBody(w)
	}

	return ""
}

// captureBody renders step 1: the filter/path line, the ▸-cursor .pcap
// candidate list, and the inline field error.
func (a *Analyze) captureBody(w int) string {
	return a.listBody(w, a.state.CaptureItems,
		a.th.Deemphasized.Render(pickGlyph(a.th,
			"no .pcap files found \u2014 [", "no .pcap files found -- ["))+
			a.th.Key("f")+
			a.th.Deemphasized.Render("] browse or type a path"),
		a.state.CaptureError)
}

// specBody renders step 2: the same list shape over the spec
// candidates; an empty commit is the engine default spec.
func (a *Analyze) specBody(w int) string {
	return a.listBody(w, a.state.SpecItems,
		a.th.Deemphasized.Render(pickGlyph(a.th,
			"no .json spec files found \u2014 ", "no .json spec files found -- "))+
			a.th.Key("Enter")+
			a.th.Deemphasized.Render(" uses the engine default"),
		a.state.SpecError)
}

// listBody renders one wizard candidate list (the send wizard's
// listBody idiom): filter line, ▸ rows with the "current" tag, the
// de-emphasized empty state (emptyText arrives pre-styled, so it is
// only clipped here), and the inline field error.
func (a *Analyze) listBody(w int, items []WizardItem, emptyText, fieldErr string) string {
	shown := filterWizardItems(items, a.draft)
	lines := []string{a.filterLine(w)}
	switch {
	case len(items) == 0:
		lines = append(lines, clipCells(emptyText, w, clipTail(a.th)))
	case len(shown) == 0:
		lines = append(lines, a.th.Deemphasized.Render(clipCells("nothing to pick \u2014 keep typing a path", w, clipTail(a.th))))
	default:
		rows := make([]string, 0, min(len(shown), analyzeListMaxRows))
		for i, it := range shown {
			if i >= analyzeListMaxRows {
				rows = append(rows, a.th.Dim.Render("  +"+strconv.Itoa(len(shown)-i)+" more"))
				break
			}
			rows = append(rows, a.itemRow(it, i))
		}
		lines = append(lines, widgets.Border(a.th, false).Width(max(w-2, 4)).Render(strings.Join(rows, "\n")))
	}
	if fieldErr != "" {
		lines = append(lines, clipCells(a.th.Status(theme.KindError, fieldErr), w, clipTail(a.th)))
	}

	return strings.Join(lines, "\n")
}

// itemRow renders one ▸-cursor candidate row: label, dim hint (the
// directory), and the "← current" tag.
func (a *Analyze) itemRow(it WizardItem, i int) string {
	marker := "  "
	if i == a.sel {
		marker = a.pick("\u25b8", ">") + " "
	}
	label := clipCells(it.Label, max((a.listInnerWidth()-2)/2, 12), clipTail(a.th))
	line := marker + label
	if hint := a.itemHint(it); hint != "" {
		line += "  " + a.th.Deemphasized.Render(clipCells(hint, max(a.listInnerWidth()-lipgloss.Width(line)-4, 8), clipTail(a.th)))
	}
	if it.Current {
		line += "  " + a.th.Deemphasized.Render(a.pick("\u2190 current", "<- current"))
	}

	return clipCells(line, a.listInnerWidth(), clipTail(a.th))
}

// itemHint is the dim annotation of a candidate: its directory (the
// full path tail, so the list never echoes the label twice).
func (a *Analyze) itemHint(it WizardItem) string {
	if it.Path == "" || baseName(it.Path) == it.Label {
		return ""
	}

	return dirName(it.Path)
}

// listInnerWidth is the usable width inside the candidate box (the box
// itself is w-2, its borders take two more).
func (a *Analyze) listInnerWidth() int {
	w, _ := frame.ContentSize(a.width, a.height)

	return max(w-4, 8)
}

// filterLine shows the live filter/typed path with the accent caret
// (the send wizard's filterLine idiom).
func (a *Analyze) filterLine(w int) string {
	caret := ""
	if a.editingStep() {
		caret = cursorGlyph(a.th)
	}
	label := "filter"
	switch a.state.Step {
	case StepCapture:
		label = "capture"
	case StepSpec:
		label = "spec"
	}
	line := a.th.Deemphasized.Render(padRight(label, analyzeListLabelWidth)) +
		a.th.Accent.Render(a.draft+caret)

	return clipCells(line, w, clipTail(a.th))
}

// headersBody renders step 3: the radio-style length-header list, the
// effective framing first (root orders the snapshot), ▸ on the cursor.
func (a *Analyze) headersBody(w int) string {
	rows := make([]string, 0, len(a.state.Headers))
	for i, hh := range a.state.Headers {
		marker := "\u25cb"
		if hh.Selected {
			marker = "\u25cf"
		}
		if a.th.ASCII {
			marker = "[ ]"
			if hh.Selected {
				marker = "[*]"
			}
		}
		cursor := a.th.Selector(i == a.sel)
		text := cursor + " " + marker + " " + hh.Header
		if hh.Selected {
			rows = append(rows, clipCells(a.th.TextPrimary.Render(text), w, clipTail(a.th)))
		} else {
			rows = append(rows, clipCells(a.th.TextMuted.Render(text), w, clipTail(a.th)))
		}
	}
	rows = append(rows, a.th.Key("j/k")+
		a.th.Dim.Render(" move | ")+
		a.th.Key("space")+a.th.Dim.Render(" selects | ")+
		a.th.Key("Enter")+a.th.Dim.Render(" next"))

	return strings.Join(rows, "\n")
}

// runBody renders step 4: the summary block (capture/spec/header), the
// folded inline option rows (goal radios, the security toggle, the
// flow filter and the enumerated flows), and the status/results block.
func (a *Analyze) runBody(w int) string {
	var b strings.Builder
	b.WriteString(a.summaryRow("capture", dashIf(a.th, baseName(a.state.CapturePath)), w))
	b.WriteString(a.summaryRow("spec", dashIf(a.th, baseName(a.state.SpecPath)), w))
	b.WriteString(a.summaryRow("header", dashIf(a.th, a.state.Header), w))
	b.WriteString(a.outputRow(w))
	b.WriteString(a.goalRow(w) + "\n")
	b.WriteString(a.securityRow(w) + "\n")
	b.WriteString(a.flowFilterRow(w) + "\n")
	flows := a.flowsBlock(w)
	if flows != "" {
		b.WriteString(flows + "\n")
	}
	b.WriteString(a.statusBlock(w))

	return strings.TrimRight(b.String(), "\n")
}

// summaryRow renders one run-step "label  value" row.
func (a *Analyze) summaryRow(label, value string, w int) string {
	line := a.th.Deemphasized.Render(padRight(label, analyzeListLabelWidth)) + a.th.TextPrimary.Render(value)

	return clipCells(line, w, clipTail(a.th)) + "\n"
}

// outputRow renders the run step's output-file row (UAT round 5): the
// effective destination with the [o] affordance, or the live one-line
// editor while [o] is open. UAT round 8: the key tokens go through
// keySpan so they wear the Theme.Key badge like every other hint.
func (a *Analyze) outputRow(w int) string {
	if a.outEditing {
		// UAT round 8 finding 6: [f] browses the output location (the
		// shared picker) while the draft is still the seeded path.
		line := a.th.Deemphasized.Render(padRight("output>", analyzeListLabelWidth)) +
			a.th.TextPrimary.Render(a.outDraft+cursorGlyph(a.th)) + " " +
			keySpan(a.th, a.th.Deemphasized, "enter", "set") +
			a.th.Deemphasized.Render("  ") +
			keySpan(a.th, a.th.Deemphasized, "f", "browse") +
			a.th.Deemphasized.Render("  ") +
			keySpan(a.th, a.th.Deemphasized, "esc", "cancel")

		return clipCells(line, w, clipTail(a.th)) + "\n"
	}
	line := a.th.Deemphasized.Render(padRight("output", analyzeListLabelWidth)) +
		a.th.TextPrimary.Render(dashIf(a.th, a.state.OutputPath)) + "  " +
		keySpan(a.th, a.th.Deemphasized, "o", "change")

	return clipCells(line, w, clipTail(a.th)) + "\n"
}

// goalRow renders the folded goal radio row (t/r/s direct keys).
func (a *Analyze) goalRow(w int) string {
	parts := make([]string, 0, len(a.state.Goals))
	for _, r := range a.state.Goals {
		text := r.Key + " " + r.Label
		if r.Selected {
			parts = append(parts, a.th.Accent.Render(a.pick("\u25cf ", "* ")+text))
		} else {
			parts = append(parts, a.th.Deemphasized.Render(a.pick("\u25cb ", "[ ] ")+text))
		}
	}
	line := a.th.Deemphasized.Render(padRight("goal", analyzeListLabelWidth)) + strings.Join(parts, a.pick("  ", "  "))

	return clipCells(line, w, clipTail(a.th))
}

// securityRow renders the folded security toggle row: the old step-⑥
// mask/raw radios collapsed to one inline toggle (m).
func (a *Analyze) securityRow(w int) string {
	word, kind := "on (mask PAN/track)", theme.KindOK
	if a.state.MaskRaw {
		word, kind = "off (raw, unsecure)", theme.KindWarn
	}
	line := a.th.Deemphasized.Render(padRight("security", analyzeListLabelWidth)) +
		a.th.Status(kind, word) +
		a.th.Dim.Render("  [") + a.th.Key("m") + a.th.Dim.Render("] toggle")

	return clipCells(line, w, clipTail(a.th))
}

// statusBlock renders the run status line and the results/preview
// block (the old step-⑦ output, restyled): running/done/error with the
// root-stamped elapsed, the write toast line, and the preview text.
func (a *Analyze) statusBlock(w int) string {
	var b strings.Builder
	switch a.state.Status {
	case AnalyzeStatusRunning:
		b.WriteString(a.th.Deemphasized.Render("running analysis (engine)...") + "\n")
	case AnalyzeStatusError:
		b.WriteString(clipCells(a.th.Status(theme.KindError, dashIf(a.th, a.state.Note)), w, clipTail(a.th)) + "\n")
	case AnalyzeStatusDone:
		if a.state.WriteLine != "" {
			kind := theme.KindError
			if a.state.WriteOK {
				kind = theme.KindOK
			}
			b.WriteString(clipCells(a.th.Status(kind, a.state.WriteLine), w, clipTail(a.th)) + "\n")
		}
		if a.state.Elapsed != "" {
			b.WriteString(a.th.Dim.Render("done in "+a.state.Elapsed) + "\n")
		}
		if a.state.Preview != "" {
			for _, l := range strings.Split(strings.TrimRight(a.state.Preview, "\n"), "\n") {
				b.WriteString(clipCells(a.th.TextPrimary.Render(l), w, clipTail(a.th)) + "\n")
			}
		}
	default:
		if a.state.Note != "" {
			b.WriteString(clipCells(a.th.Status(theme.KindError, a.state.Note), w, clipTail(a.th)) + "\n")
		} else {
			b.WriteString(a.th.TextMuted.Render("ready - ") +
				a.th.Key("Enter") +
				a.th.TextMuted.Render(" starts the analysis") + "\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// baseName is the last path element of a path.
func baseName(path string) string {
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}

	return path
}

// dirName is the directory part of a path ("" when bare).
func dirName(path string) string {
	i := strings.LastIndexAny(path, `/\`)
	if i <= 0 {
		return ""
	}

	return path[:i]
}

// joinSep is the separator for a line built outside the view (fixtures and
// root-derived state). It delegates to the one policy in theme so a fixture
// cannot drift from what root builds for the same line.
func joinSep(th *theme.Theme) string {
	if th == nil {
		return theme.GlyphSeparator
	}

	return th.Separator()
}
