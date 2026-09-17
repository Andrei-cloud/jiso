// analyze_select.go answers questions about the cursor and draft: filtering a
// list against the draft, resolving the selection to a capture or spec path,
// and keeping the cursor inside a list that just changed size.
package pages

import (
	"strings"
)

// filteredCapture returns the capture candidates matching the draft.
func (a *Analyze) filteredCapture() []WizardItem {
	return filterWizardItems(a.state.CaptureItems, a.draft)
}

// filteredSpec returns the spec candidates matching the draft.
func (a *Analyze) filteredSpec() []WizardItem { return filterWizardItems(a.state.SpecItems, a.draft) }

// filterWizardItems keeps items whose label or path contains the filter
// (case-insensitive).
func filterWizardItems(items []WizardItem, filter string) []WizardItem {
	f := strings.ToLower(strings.TrimSpace(filter))
	if f == "" {
		return items
	}
	out := make([]WizardItem, 0, len(items))
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.Label+" "+it.Path), f) {
			out = append(out, it)
		}
	}

	return out
}

// looksLikeCapture reports whether the draft should be treated as a
// typed capture path instead of a list filter.
func looksLikeCapture(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))

	return looksLikePath(s) || strings.HasSuffix(s, ".pcap") || strings.HasSuffix(s, ".pcapng")
}

// pickedCapture returns the Enter value of the capture step: the typed path
// when the draft looks like one, else the candidate under the cursor; an
// empty list with an empty draft returns "" so root opens the file picker.
func (a *Analyze) pickedCapture() (string, bool) {
	if looksLikeCapture(a.draft) {
		return strings.TrimSpace(a.draft), true
	}
	idx := a.filteredCapture()
	if len(idx) == 0 {
		return "", a.draft == ""
	}
	if a.sel >= len(idx) {
		return "", false
	}

	return idx[a.sel].Path, true
}

// pickedSpec returns the Enter value of the spec step; an empty draft
// with no candidates commits "" (the engine default spec).
func (a *Analyze) pickedSpec() (string, bool) {
	if looksLikePath(a.draft) {
		return strings.TrimSpace(a.draft), true
	}
	idx := a.filteredSpec()
	if len(idx) == 0 {
		return "", a.draft == ""
	}
	if a.sel >= len(idx) {
		return "", false
	}

	return idx[a.sel].Path, true
}

// visibleFlowRows lists the flow rows the filter shows, capped to the rendered
// window — the flow cursor's domain, spanning dst AND src rows.
func (a *Analyze) visibleFlowRows() []AnalyzeFlowRow {
	rows := make([]AnalyzeFlowRow, 0, len(a.state.Flows))
	for _, f := range a.state.Flows {
		if FlowMatchesFilter(f, a.draft) {
			rows = append(rows, f)
			if len(rows) == analyzeListMaxRows {
				break
			}
		}
	}

	return rows
}

// clampSel keeps the cursor inside the current step's filtered list.
func (a *Analyze) clampSel() {
	var n int
	switch a.state.Step {
	case StepCapture:
		n = len(a.filteredCapture())
	case StepSpec:
		n = len(a.filteredSpec())
	case StepHeader:
		n = len(a.state.Headers)
	case StepRun:
		n = len(a.visibleFlowRows())
	}
	if n == 0 {
		a.sel = 0

		return
	}
	if a.sel >= n {
		a.sel = n - 1
	}
}

// wizardItemsIndex finds the first tagged item (clamped, never negative).
func wizardItemsIndex(items []WizardItem, tagged func(WizardItem) bool) int {
	for i, it := range items {
		if tagged(it) {
			return i
		}
	}

	return 0
}

// headerIndex finds the selected header row.
func headerIndex(hs []AnalyzeHeaderItem, sel func(AnalyzeHeaderItem) bool) int {
	for i, h := range hs {
		if sel(h) {
			return i
		}
	}

	return 0
}
