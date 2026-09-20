// send_wizard.go holds the send-wizard modal: ONE
// centered
// modal that walks spec ▸ tx file ▸ template, prefixed with a connect step
// when no connection is live ("send selected with connection settings
// undefined opens connection settings first"). Step 0 embeds the §E form
// machinery (a *ConnectDialog body, picker overlay and all); steps 1-2 are
// filtered lists (typing filters, and a filter that looks like a path is
// offered as the value on Enter); step 3 lists the tx file's transaction
// templates. The wizard is presentation + input routing only: every leg
// (connect attempt, spec/file commits, the send walk) runs in the root,
// which pushes snapshots via SetState and receives the Wizard*Msg values
// back through the Update command. The key routing lives in
// send_wizard_keys.go.
package pages

import (
	"strings"

	"jiso/internal/tui/theme"
)

// Wizard step ids (the rail labels, in walk order).
const (
	WizardStepConnect = "connect"
	WizardStepSpec    = "spec"
	WizardStepFile    = "file"
	WizardStepSend    = "send"
)

// WizardItem is one list entry in the spec/file steps. Path is the value
// the wizard emits, Hint the dim annotation (spec name, tx count),
// Current the "← current" tag of the live config value.
type WizardItem struct {
	Label   string
	Path    string
	Hint    string
	Current bool
}

// WizardTemplate is one transaction template row of the send step: the
// fields the §D walk will carry, pre-extracted by the root (MTI from
// field 0, masked PAN from field 2, amount from field 4; Description is
// the fallback when the file carries no PAN/amount).
type WizardTemplate struct {
	Name        string
	MTI         string
	PAN         string
	Amount      string
	Description string
}

// WizardState is the root-pushed snapshot. Steps drives the rail: four
// entries starting with connect while offline, three once connected.
// Target/TargetOK stamp the send step's connection line; Error is the
// inline failure line (bad path, unparsable file).
type WizardState struct {
	Steps     []string
	SpecItems []WizardItem
	FileItems []WizardItem
	Templates []WizardTemplate
	Target    string
	TargetOK  bool
	Error     string
}

// Wizard messages: the wizard expresses intent, the root runs the leg.
type (
	// WizardConnectAttemptMsg asks the root to validate the step-0 form
	// and arm the connect attempt loop (it stamps progress through the
	// embedded dialog).
	WizardConnectAttemptMsg struct{}
	// WizardCancelMsg asks the root to close the wizard (and cancel an
	// in-flight attempt when one runs).
	WizardCancelMsg struct{}
	// WizardChooseSpecMsg carries the spec file picked in step 1.
	WizardChooseSpecMsg struct{ Path string }
	// WizardChooseFileMsg carries the tx file picked in step 2.
	WizardChooseFileMsg struct{ Path string }
	// WizardSendMsg carries the template name to send (the root commits
	// the spec/tx-file picks first, then runs the §D walk and pops the
	// wizard).
	WizardSendMsg struct{ Name string }
	// WizardBrowseMsg asks the root to open the shared file picker over
	// the wizard ([f] on the spec or file step; IsSpec tells which).
	WizardBrowseMsg struct{ IsSpec bool }
)

// SendWizard is the modal; a reference type owned by the root while open.
type SendWizard struct {
	th  *theme.Theme
	dlg *ConnectDialog // step 0 form, nil once the wizard starts connected

	state  WizardState
	step   int // index into state.Steps
	sel    int // list cursor (spec/file/template lists)
	filter string

	preselect string // template the send step's cursor opens on (root-owned pick)

	width, height int
	nav           connectNav // focus/adjust bindings reused
}

// NewSendWizard builds the modal over an empty state; the root pushes the
// first snapshot before the first View. A nil theme selects theme.Default.
func NewSendWizard(th *theme.Theme) *SendWizard {
	if th == nil {
		th = theme.Default()
	}

	return &SendWizard{th: th, nav: newConnectNav()}
}

// Theme exposes the resolved theme (golden tests inject a profile).
func (w *SendWizard) Theme() *theme.Theme { return w.th }

// Size reports the last tea.WindowSizeMsg.
func (w *SendWizard) Size() (width, height int) { return w.width, w.height }

// State returns the rendered snapshot (tests).
func (w *SendWizard) State() WizardState { return w.state }

// Step reports the current step index.
func (w *SendWizard) Step() int { return w.step }

// CurrentStepID reports the id at the cursor ("spec", "file", "send").
func (w *SendWizard) CurrentStepID() string { return w.currentStep() }

// Editing reports the two-mode flag of the wizard:
// the list steps (spec/file/send) are in EDIT mode while a
// filter draft is in progress, and the connect step delegates to the
// embedded dialog. The [f] browse gate reads this predicate — navigate
// mode sends f to the root-owned file picker, edit mode types f
// literally into the filter (the §G server-form pattern made uniform).
func (w *SendWizard) Editing() bool {
	if w.currentStep() == WizardStepConnect {
		return w.dlg != nil && w.dlg.Editing()
	}

	return w.filter != ""
}

// ConnectForm returns the embedded step-0 dialog (nil when the wizard
// started connected); the root syncs its Enabled flags and TLS note
// exactly like the standalone §E overlay.
func (w *SendWizard) ConnectForm() *ConnectDialog { return w.dlg }

// SetConnectForm installs the root-built form for the connect step and
// puts the connect step at the head of the rail (called when the wizard
// opens offline). The rest of the rail belongs to the root ("ask only
// for what is missing"), so the step is prepended, never a fixed
// four-step shape swapped in.
func (w *SendWizard) SetConnectForm(st ConnectFormState) {
	if w.dlg == nil {
		w.dlg = NewConnectDialog(w.th)
		if len(w.state.Steps) == 0 || w.state.Steps[0] != WizardStepConnect {
			w.state.Steps = append([]string{WizardStepConnect}, w.state.Steps...)
		}
	}
	w.dlg.SetState(st)
}

// OnConnected advances past the connect step (root calls it after
// stamping the dialog's non-in-flight state).
func (w *SendWizard) OnConnected(target string) {
	w.state.Target, w.state.TargetOK = target, true
	if w.step == 0 && w.currentStep() == WizardStepConnect {
		w.step++
	}
	w.resetStepInput()
}

// HomeOnSend parks the wizard on the send step (with spec
// and tx file already loaded, re-walking spec/file on every send is
// friction — the wizard opens where the decision actually is; Esc
// still steps back through the rail to change either pick). Reports
// false when the send step is not in the rail (e.g. the offline
// wizard must start at connect).
func (w *SendWizard) HomeOnSend() bool {
	for i, s := range w.state.Steps {
		if s == WizardStepSend {
			w.step = i
			w.resetStepInput()

			return true
		}
	}

	return false
}

// SetState replaces the root-owned data, preserving the page-owned step,
// cursor, filter and size.
func (w *SendWizard) SetState(state WizardState) {
	w.state = state
	w.clampSel()
}

// currentStep is the id at the cursor position.
func (w *SendWizard) currentStep() string {
	if w.step < 0 || w.step >= len(w.state.Steps) {
		return ""
	}

	return w.state.Steps[w.step]
}

// resetStepInput clears the filter/cursor when the step changes, homing
// the cursor on the item tagged current so Enter with no navigation
// keeps the live value instead of the alphabetically-first one.
func (w *SendWizard) resetStepInput() {
	w.filter, w.sel = "", 0
	switch w.currentStep() {
	case WizardStepSpec:
		w.sel = currentIndexOf(w.state.SpecItems)
	case WizardStepFile:
		w.sel = currentIndexOf(w.state.FileItems)
	case WizardStepSend:
		w.sel = templateIndexOf(w.state.Templates, w.preselect)
	}
}

// templateIndexOf returns the index of the named template, 0 when the
// name is empty or the current listing does not carry it (a pre-selection
// homes the cursor, never strands it past the list).
func templateIndexOf(templates []WizardTemplate, name string) int {
	if name == "" {
		return 0
	}
	for i, t := range templates {
		if t.Name == name {
			return i
		}
	}

	return 0
}

// HomeCursor resets filter/cursor for the current step (root calls it
// once the initial state is loaded, so the cursor opens on the current
// item rather than the alphabetically-first one).
func (w *SendWizard) HomeCursor() { w.resetStepInput() }

// SetPreset names a transaction template for the send step's cursor to
// open on (root sets it when the wizard opens from a chosen transaction).
// A cursor start, never a commit: Enter still runs the step's leg and
// navigation still moves the cursor.
func (w *SendWizard) SetPreset(name string) { w.preselect = name }

// Preset reports the pre-selected template name ("" when none).
func (w *SendWizard) Preset() string { return w.preselect }

// BackToStep lands on an EARLIER rail step for a rail click:
// Backward revisits are free exactly like the wizard's own Esc
// walk, and the arrival clears the step-local input (the back +
// resetStepInput contract). A forward or current index changes nothing —
// forward transitions are the step's Enter leg, run through root's
// validation gates, and a rail click must not bypass them.
func (w *SendWizard) BackToStep(n int) {
	if n < 0 || n >= w.step {
		return
	}
	w.step = n
	w.resetStepInput()
}

// currentIndexOf returns the index of the first item tagged Current.
func currentIndexOf(items []WizardItem) int {
	for i, it := range items {
		if it.Current {
			return i
		}
	}

	return 0
}

// looksLikePath reports whether the filter should be treated as a typed
// path instead of a list filter ("typing a path in the
// bottom input overrides the list").
func looksLikePath(s string) bool {
	s = strings.TrimSpace(s)

	return strings.ContainsAny(s, "/\\~") || strings.HasSuffix(strings.ToLower(s), ".json")
}

// pickedPath returns the Enter value of a list step: the typed path when
// the filter looks like one, else the item under the cursor.
func (w *SendWizard) pickedPath(items []WizardItem) (string, bool) {
	if looksLikePath(w.filter) {
		return strings.TrimSpace(w.filter), true
	}
	idx := w.filteredItems(items)
	if len(idx) == 0 || w.sel >= len(idx) {
		return "", false
	}

	return items[idx[w.sel]].Path, true
}

// filteredCount is the length of the current step's filtered list.
func (w *SendWizard) filteredCount() int {
	switch w.currentStep() {
	case WizardStepSpec:
		return len(w.filteredItems(w.state.SpecItems))
	case WizardStepFile:
		return len(w.filteredItems(w.state.FileItems))
	case WizardStepSend:
		return len(w.filteredTemplates())
	}

	return 0
}

// filteredItems returns the indices of items matching the filter
// (case-insensitive substring over label + path).
func (w *SendWizard) filteredItems(items []WizardItem) []int {
	f := strings.ToLower(w.filter)
	idx := make([]int, 0, len(items))
	for i, it := range items {
		if f == "" || strings.Contains(strings.ToLower(it.Label+" "+it.Path), f) {
			idx = append(idx, i)
		}
	}

	return idx
}

// filteredTemplates returns the template indices matching the filter.
func (w *SendWizard) filteredTemplates() []int {
	f := strings.ToLower(w.filter)
	idx := make([]int, 0, len(w.state.Templates))
	for i, t := range w.state.Templates {
		if f == "" || strings.Contains(strings.ToLower(t.Name+" "+t.Description), f) {
			idx = append(idx, i)
		}
	}

	return idx
}

// clampSel keeps the cursor inside the current step's filtered list.
func (w *SendWizard) clampSel() {
	var n int
	switch w.currentStep() {
	case WizardStepSpec:
		n = len(w.filteredItems(w.state.SpecItems))
	case WizardStepFile:
		n = len(w.filteredItems(w.state.FileItems))
	case WizardStepSend:
		n = len(w.filteredTemplates())
	}
	if w.sel >= n {
		w.sel = max(n-1, 0)
	}
}

var _ Modal = (*SendWizard)(nil)

// AdvanceStep moves to the next step after a root-side leg completes
// (spec/file pick); the last step never advances (Enter sends).
func (w *SendWizard) AdvanceStep() {
	if w.step < len(w.state.Steps)-1 {
		w.step++
		w.resetStepInput()
	}
}
