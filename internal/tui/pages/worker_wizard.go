// worker_wizard.go is the §H worker start wizard: a three-step modal —
// "1 tx ▸ 2 rate/params ▸ 3 run" — with a scrollable 5-row candidate
// list (space multi-select or Enter single-select, "/" filter), labeled
// inline-edit param rows, and a run summary. It is presentation + input
// routing only: every leg runs root-side, and the wizard never touches
// internal/app nor reads the clock.
//
// The tx step claims the keyboard while its "/" filter is open and the
// param step only while a row is being typed into; navigate mode claims
// nothing, so "?" stays a help key there. Ctrl+C stays global.
package pages

import (
	"strings"

	key "charm.land/bubbles/v2/key"

	"jiso/internal/tui/theme"
)

// WorkerWizard is the modal; a reference type owned by the root while
// open (the SendWizard ownership pattern).
type WorkerWizard struct {
	th    *theme.Theme
	mode  string
	nav   workerWizardNav
	state WorkerWizardState

	width, height int
	step          int

	// step 1 (tx list): filter line, cursor, scroll window top, and
	// selections; checked aligns to state.TxItems so filtering and
	// refreshes never lose a toggle.
	filtering bool
	draft     string
	sel       int
	top       int
	checked   []bool
	picked    string

	// step 2 (params): focused row, its inline validation line, and the
	// two-mode flag: a printable enters edit mode; esc leaves the row first.
	focus    int
	editing  bool
	params   map[string]string
	paramErr string

	// errLine is the step-1 inline error (Enter with nothing selected).
	errLine string
}

// workerWizardNav is the wizard keymap. Enter/Esc semantics are wizard
// transitions, so they route through updateKey; the arrow pair stays
// code-only on the param step because duration values type j/k.
type workerWizardNav struct {
	Cancel    key.Binding
	Enter     key.Binding
	Backspace key.Binding
	Space     key.Binding
	Browse    key.Binding
	Filter    key.Binding
	Down      key.Binding
	Up        key.Binding
	FieldDown key.Binding
	FieldUp   key.Binding
}

func newWorkerWizardNav() workerWizardNav {
	return workerWizardNav{
		Cancel:    key.NewBinding(key.WithKeys(theme.KeyEsc)),
		Enter:     key.NewBinding(key.WithKeys(theme.KeyEnter)),
		Backspace: key.NewBinding(key.WithKeys("backspace")),
		Space:     key.NewBinding(key.WithKeys("space")),
		Browse:    key.NewBinding(key.WithKeys("f")),
		Filter:    key.NewBinding(key.WithKeys("/")),
		Down:      key.NewBinding(key.WithKeys("down", "j")),
		Up:        key.NewBinding(key.WithKeys("up", "k")),
		FieldDown: key.NewBinding(key.WithKeys("down")),
		FieldUp:   key.NewBinding(key.WithKeys("up")),
	}
}

// NewWorkerWizard builds the wizard for one mode ("stress"/"bgsend")
// with the mode's prefilled parameter rows (see the WorkerDefault*
// source contract). A nil theme selects theme.Default.
func NewWorkerWizard(th *theme.Theme, mode string) *WorkerWizard {
	if th == nil {
		th = theme.Default()
	}
	w := &WorkerWizard{th: th, mode: mode, nav: newWorkerWizardNav(), params: map[string]string{}}
	switch mode {
	case WorkerModeStress:
		w.params[WorkerParamTps] = WorkerDefaultTps
		w.params[WorkerParamRamp] = WorkerDefaultRamp
		w.params[WorkerParamDuration] = WorkerDefaultDuration
		w.params[WorkerParamWorkers] = WorkerDefaultWorkers
	default:
		w.params[WorkerParamCount] = WorkerDefaultCount
		w.params[WorkerParamInterval] = WorkerDefaultInterval
	}

	return w
}

// Mode reports the wizard's mode (root tests).
func (w *WorkerWizard) Mode() string { return w.mode }

// Theme exposes the resolved theme (golden tests inject a profile).
func (w *WorkerWizard) Theme() *theme.Theme { return w.th }

// Size reports the last tea.WindowSizeMsg.
func (w *WorkerWizard) Size() (width, height int) { return w.width, w.height }

// Step reports the current step (tests).
func (w *WorkerWizard) Step() int { return w.step }

// State returns the rendered snapshot (root stamps the leg lines on it).
func (w *WorkerWizard) State() WorkerWizardState { return w.state }

// Param reads one step-2 row's live text ("" when absent; tests).
func (w *WorkerWizard) Param(key string) string { return w.params[key] }

// SelectedNames returns the checked transaction names in list order
// (stress) or the picked single name (bgsend).
func (w *WorkerWizard) SelectedNames() []string {
	if w.mode != WorkerModeStress {
		if w.picked == "" {
			return nil
		}

		return []string{w.picked}
	}
	var out []string
	for i, it := range w.state.TxItems {
		if i < len(w.checked) && w.checked[i] {
			out = append(out, it.Path)
		}
	}

	return out
}

// SetState replaces the root-owned data, preserving the page-owned step,
// cursor, scroll window, filter, and parameter edits. Checked/picked
// selections re-align to refreshed candidates BY NAME, so a tx-file
// reload keeps every selection whose name still exists.
func (w *WorkerWizard) SetState(st WorkerWizardState) {
	prev := w.state.TxItems
	w.state = st

	checked := make([]bool, len(st.TxItems))
	wasChecked := map[string]bool{}
	for i, it := range prev {
		if i < len(w.checked) && w.checked[i] {
			wasChecked[it.Label] = true
		}
	}
	picked := w.picked
	for i, it := range st.TxItems {
		checked[i] = wasChecked[it.Label]
		if it.Label == picked {
			picked = "" // the name survived only when re-tagged below
			w.picked = it.Label
		}
	}
	if picked != "" {
		w.picked = "" // the picked name vanished with the old list
	}
	w.checked = checked

	w.clampSel()
	w.clampTop()
}

// HomeSelection seeds the bgsend single-select on the first candidate;
// the stress multi-select opens empty. Root calls it once after loading.
func (w *WorkerWizard) HomeSelection() {
	if w.mode == WorkerModeBg && w.picked == "" && len(w.state.TxItems) > 0 {
		w.picked = w.state.TxItems[0].Label
	}
}

// ClaimsKeyboard implements KeyboardClaimer: the claim is the two-mode
// edit flag — the tx step claims while its "/" filter is open, the param
// step only while a row is being typed into. Navigate mode claims nothing.
func (w *WorkerWizard) ClaimsKeyboard() bool {
	return w.Editing()
}

// Editing reports the two-mode flag: true while the tx step's "/" filter
// is open or the param step has entered a row; "?" stays a help key only
// while this is false.
func (w *WorkerWizard) Editing() bool {
	switch w.step {
	case WorkerStepTx:
		return w.filtering
	case WorkerStepParams:
		return w.editing
	}

	return false
}

// resolveRun builds the start parameters from the current selections
// and step-2 values ("" err means the wizard may advance or start).
func (w *WorkerWizard) resolveRun() (WorkerRun, string) {
	if w.mode == WorkerModeStress {
		return ResolveStressRun(w.SelectedNames(),
			w.params[WorkerParamTps], w.params[WorkerParamRamp],
			w.params[WorkerParamDuration], w.params[WorkerParamWorkers])
	}

	return ResolveBgRun(w.picked, w.params[WorkerParamInterval], w.params[WorkerParamCount])
}

// paramKeys are the step-2 row keys in display order (stress: tps, ramp,
// duration, workers; bgsend: count, interval).
func (w *WorkerWizard) paramKeys() []string {
	if w.mode == WorkerModeStress {
		return []string{WorkerParamTps, WorkerParamRamp, WorkerParamDuration, WorkerParamWorkers}
	}

	return []string{WorkerParamCount, WorkerParamInterval}
}

// filteredTx returns the TxItems indices matching the draft
// (case-insensitive substring over label + path).
func (w *WorkerWizard) filteredTx() []int {
	f := strings.ToLower(strings.TrimSpace(w.draft))
	idx := make([]int, 0, len(w.state.TxItems))
	for i, it := range w.state.TxItems {
		if f == "" || strings.Contains(strings.ToLower(it.Label+" "+it.Path), f) {
			idx = append(idx, i)
		}
	}

	return idx
}

// moveSel walks the cursor inside the filtered list and scrolls the
// 5-row window to keep it visible.
func (w *WorkerWizard) moveSel(delta int) {
	n := len(w.filteredTx())
	if n == 0 {
		w.sel = 0

		return
	}
	w.sel = min(max(w.sel+delta, 0), n-1)
	w.clampTop()
}

// toggleSel flips the cursor row's stress checkbox.
func (w *WorkerWizard) toggleSel() {
	idx := w.filteredTx()
	if w.sel >= len(idx) {
		return
	}
	i := idx[w.sel]
	if i < len(w.checked) {
		w.checked[i] = !w.checked[i]
	}
}

// clampSel keeps the cursor inside the current filtered list.
func (w *WorkerWizard) clampSel() {
	n := len(w.filteredTx())
	if n == 0 {
		w.sel = 0

		return
	}
	if w.sel >= n {
		w.sel = n - 1
	}
}

// clampTop scrolls the window the MINIMUM distance that keeps the cursor
// inside the 5-row view (no page jumps).
func (w *WorkerWizard) clampTop() {
	n := len(w.filteredTx())
	if w.top > n-WorkerTxVisibleRows {
		w.top = max(n-WorkerTxVisibleRows, 0)
	}
	if w.sel < w.top {
		w.top = w.sel
	}
	if w.sel >= w.top+WorkerTxVisibleRows {
		w.top = w.sel - WorkerTxVisibleRows + 1
	}
}

// pick returns the ascii form under theme.ASCII, the truecolor otherwise.
func (w *WorkerWizard) pick(truecolor, ascii string) string {
	if w.th.ASCII {
		return ascii
	}

	return truecolor
}

var (
	_ Modal           = (*WorkerWizard)(nil)
	_ KeyboardClaimer = (*WorkerWizard)(nil)
)
