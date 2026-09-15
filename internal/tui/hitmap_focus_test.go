// hitmap_focus_test.go pins Task 8.5 (UAT round 8 finding 9, click to
// focus): a LEFT CLICK on a drawn field row of the §E connect dialog or
// the §G server form focuses that field — NAVIGATE mode, a click never
// enters edit mode (typing still does, the 4.2 two-mode model) — and a
// click-focus closes an open header picker overlay so no stale overlay
// stays drawn over the newly focused field (carry 3). While the shared
// file picker or a §N3 confirm sits ON TOP of a form the field clicks are
// inert (those own the input; the narrow guard, not the full modalOpen,
// which would always be true for a form's own fields). A click on a
// wizard step-rail label moves to that step THROUGH the wizard's own step
// navigation: backward is a free revisit (the Esc walk's rule), forward
// replays the current step's Enter leg with its validation/gates, and a
// click on the current step is inert. The resolve/addAbs/OnMouse
// machinery itself is pinned in hitmap_test.go / hitmap_select_test.go.
package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/geom"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
	"jiso/internal/tui/widgets"
)

// focusRowAt returns the ABSOLUTE rect published for focus-row index —
// the cell a click must land on — failing the test when the last render
// published no such row.
func focusRowAt(t *testing.T, hits []widgets.RowHit, index int) geom.Rect {
	t.Helper()

	for _, h := range hits {
		if h.Index == index {
			if h.Rect.W <= 0 || h.Rect.H <= 0 {
				t.Fatalf("focus row %d published with no drawn cells: %v", index, h.Rect)
			}

			return h.Rect
		}
	}
	t.Fatalf("the last render published no focus row %d: %#v", index, hits)

	return geom.Rect{}
}

// contentLineAt is the drawn frame line at absolute terminal row y (the
// row a focus rect claims to sit on).
func contentLineAt(v tea.View, y int) string {
	lines := strings.Split(strings.TrimRight(v.Content, "\n"), "\n")
	if y < 0 || y >= len(lines) {
		return ""
	}

	return lines[y]
}

// dlgFieldIdx resolves a named §E field's index in render order (the
// server form's twin is serverFieldIdx, root_server_form_test.go).
func dlgFieldIdx(t *testing.T, d *pages.ConnectDialog, key string) int {
	t.Helper()

	for i, f := range d.State().Fields {
		if f.Key == key {
			return i
		}
	}
	t.Fatalf("no §E field %q", key)

	return -1
}

// pickerCovers reports whether a file-picker entry row covers the
// absolute cell (x,y): where the picker's ink covers a form row the click
// resolves the picker's own select hit (Task 8.3 z-order), so an
// inert-focus click must land on a cell it does not cover.
func pickerCovers(m *RootModel, x, y int) bool {
	for _, r := range m.pickerRowHits() {
		if r.Rect.Contains(x, y) {
			return true
		}
	}

	return false
}

// TestClickFocusesConnectField is the Task 8.5 tracer (brief Step 1): a
// left click at the drawn y of the "Port" field in the §E dialog focuses
// it (ConnectDialog.Focus() is that field's index) in NAVIGATE mode —
// Focus() lands there, Editing() stays false.
func TestClickFocusesConnectField(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('c'))
	if m.dlg == nil {
		t.Fatal("c must open the §E connect dialog")
	}
	v := m.View()

	port := dlgFieldIdx(t, m.dlg, pages.ConnectFieldPort)
	if m.dlg.Focus() == port {
		t.Fatal("precondition: the Port field must not start focused")
	}

	row := focusRowAt(t, m.connectFormRowHits(), port)
	if line := contentLineAt(v, row.Y); !strings.Contains(line, "Port") {
		t.Fatalf("the Port row rect must sit on the drawn Port line (y %d): %q\n%s", row.Y, line, v.Content)
	}

	cmd := v.OnMouse(tea.MouseClickMsg{X: row.X + row.W/2, Y: row.Y, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("a left click inside a drawn field row must emit a focus msg, got nil")
	}
	want := focusMsg{region: regionConnectForm, index: port}
	if got := cmd(); got != want {
		t.Fatalf("field click = %#v, want %#v", got, want)
	}

	if _, _ = m.Update(want); m.dlg.Focus() != port {
		t.Fatalf("after click-focus Focus() = %d, want %d", m.dlg.Focus(), port)
	}
	if m.dlg.Editing() {
		t.Fatal("a click focuses (navigate mode); it must not enter edit mode (typing still does)")
	}
}

// TestClickFocusClosesHeaderPicker pins carry 3: ConnectDialog.SetFocus
// must not leave the header picker overlay drawn over the newly focused
// field — a click that focuses a field closes an open picker first.
func TestClickFocusClosesHeaderPicker(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('c'))
	header := dlgFieldIdx(t, m.dlg, pages.ConnectFieldHeader)
	port := dlgFieldIdx(t, m.dlg, pages.ConnectFieldPort)

	// Open the header overlay the way the keyboard does: click-focus the
	// picker row, then toggle it open.
	if _, _ = m.Update(focusMsg{region: regionConnectForm, index: header}); m.dlg.Focus() != header {
		t.Fatalf("click-focus landed on %d, want the picker row %d", m.dlg.Focus(), header)
	}
	_, _ = m.Update(ch(' ')) // space toggles the collapsed picker row (4.2)
	if !m.dlg.PickerOpen() {
		t.Fatal("precondition: space must open the header picker overlay")
	}

	v := m.View()
	row := focusRowAt(t, m.connectFormRowHits(), port) // the row ABOVE the overlay still draws
	cmd := v.OnMouse(tea.MouseClickMsg{X: row.X + row.W/2, Y: row.Y, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("a field row outside the open overlay must stay clickable")
	}
	if got := cmd(); got != (focusMsg{region: regionConnectForm, index: port}) {
		t.Fatalf("field click with the overlay open = %#v, want the Port focus hit", got)
	}
	_, _ = m.Update(cmd())

	if m.dlg.Focus() != port {
		t.Fatalf("after click-focus Focus() = %d, want %d", m.dlg.Focus(), port)
	}
	if m.dlg.PickerOpen() {
		t.Fatal("click-to-focus must close the stale header picker; the overlay must not stay drawn over the newly focused field")
	}
}

// TestFocusMsgInertUnderFilePicker pins carry 4: while the shared file
// picker is open ON TOP of the §G form, the picker owns the input — a
// focusMsg for a form field changes nothing (the narrow guard: the form
// itself is open, so the full modalOpen() gate cannot be what makes this
// inert, and a naive one would make click-focus dead forever).
func TestFocusMsgInertUnderFilePicker(t *testing.T) {
	r := newServeTestRoot(t)
	r.upd(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	r.key('c')
	if r.m.serverDlg == nil {
		t.Fatal("the §G server form must open")
	}
	routes := serverFieldIdx(t, r.m.serverDlg, serverFieldRoutes)
	r.m.serverDlg.SetFocus(routes)
	r.key('f') // [f] navigate-mode browse on the focused routes row opens the shared picker over the form
	if r.m.filePick == nil {
		t.Fatal("precondition: [f] must open the file picker over the form")
	}

	port := serverFieldIdx(t, r.m.serverDlg, serverFieldPort)

	// A straggler focusMsg must not move the form's focus while the
	// picker owns the input (the handleFocusMsg gate, pinned the way
	// TestSelectMsgFrozenByModal pins handleSelectMsg's).
	r.upd(focusMsg{region: regionServerForm, index: port})
	if r.m.serverDlg.Focus() != routes {
		t.Fatalf("picker open: Focus() = %d, want %d (inert behind the picker)", r.m.serverDlg.Focus(), routes)
	}

	// And the physical path: a click on an UNCOVERED cell of the Port row
	// (where the picker's rows are drawn over the row, the click resolves
	// the picker's own select hit instead — Task 8.3's z-order, unchanged)
	// still moves no focus.
	v := r.m.View()
	row := focusRowAt(t, r.m.serverFormRowHits(), port)
	x, uncovered := row.X, false
	for ; x < row.X+row.W; x++ {
		if !pickerCovers(r.m, x, row.Y) {
			uncovered = true

			break
		}
	}
	if !uncovered {
		t.Fatal("the picker must leave an uncovered cell on the Port row for this pin to mean anything")
	}
	if cmd := v.OnMouse(tea.MouseClickMsg{X: x, Y: row.Y, Button: tea.MouseLeft}); cmd != nil {
		r.upd(cmd()) // whatever it resolved to, the form's focus must not move
	}
	if r.m.serverDlg == nil || r.m.serverDlg.Focus() != routes {
		t.Fatalf("a field click over the open picker moved the form's focus to %d, want it inert", r.m.serverDlg.Focus())
	}
}

// TestFocusMsgInertUnderConfirm pins the §N3 leg of carry 4: the overlay
// stack draws confirms LAST, above even the forms, so a pending confirm
// owns the input and a field click stays inert.
func TestFocusMsgInertUnderConfirm(t *testing.T) {
	r := newServeTestRoot(t)
	r.upd(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.key('4')
	r.key('c')
	routes := serverFieldIdx(t, r.m.serverDlg, serverFieldRoutes)
	r.m.serverDlg.SetFocus(routes)

	r.m.workersConfirm = widgets.NewConfirmDialog(r.m.themeOrNil(), "quit jiso?")
	if !r.m.confirmPending() {
		t.Fatal("precondition: the confirm must be pending")
	}

	v := r.m.View()
	port := serverFieldIdx(t, r.m.serverDlg, serverFieldPort)
	row := focusRowAt(t, r.m.serverFormRowHits(), port)
	if cmd := v.OnMouse(tea.MouseClickMsg{X: row.X + row.W/2, Y: row.Y, Button: tea.MouseLeft}); cmd != nil {
		r.upd(cmd()) // whatever it resolved to, the form's focus must not move
	}
	if r.m.serverDlg == nil || r.m.serverDlg.Focus() != routes {
		t.Fatalf("a field click under a pending confirm moved focus to %d, want %d (inert)", r.m.serverDlg.Focus(), routes)
	}
}

// TestClickWorkerRailSelectsStep walks the §H worker wizard's rail
// (brief: "a wizard-step-rail click moves to that step"): a forward click
// on "2 params" runs the current step's own Enter leg (the seeded
// single-select commits through commitTx — validation never bypassed),
// the backward click on "1 tx" is a free revisit through setStep, and a
// click on the current step is inert.
func TestClickWorkerRailSelectsStep(t *testing.T) {
	r := newFormTestRoot(t)
	r.key(tea.KeyPressMsg{Code: 'b', Text: "b"})
	w := r.m.workerWiz
	if w == nil {
		t.Fatal("b must open the bgsend wizard")
	}
	if w.Step() != pages.WorkerStepTx {
		t.Fatalf("precondition: the wizard must open on the tx step, got %d", w.Step())
	}

	// Forward click: "2 params" lands on the params step THROUGH the
	// Enter leg, not around it.
	v := r.m.View()
	row := focusRowAt(t, r.m.workerRailRowHits(), pages.WorkerStepParams)
	if line := contentLineAt(v, row.Y); !strings.Contains(line, "2 params") {
		t.Fatalf("the rail step-2 rect must sit on the drawn rail line (y %d): %q", row.Y, line)
	}
	cmd := v.OnMouse(tea.MouseClickMsg{X: row.X + row.W/2, Y: row.Y, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("a rail-label click must emit a focus msg, got nil")
	}
	want := focusMsg{region: regionWorkerRail, index: pages.WorkerStepParams}
	if got := cmd(); got != want {
		t.Fatalf("rail click = %#v, want %#v", got, want)
	}
	r.upd(cmd())
	if w.Step() != pages.WorkerStepParams {
		t.Fatalf("forward rail click = step %d, want %d (the Enter leg's outcome)", w.Step(), pages.WorkerStepParams)
	}

	// Backward click: "1 tx" is a free revisit (the wizard's own Esc-walk
	// rule, applied through setStep so the arrival lands clean).
	_ = r.m.View()
	r.upd(focusMsg{region: regionWorkerRail, index: pages.WorkerStepTx})
	if w.Step() != pages.WorkerStepTx {
		t.Fatalf("backward rail click = step %d, want %d (free revisit)", w.Step(), pages.WorkerStepTx)
	}

	// The current step: inert.
	r.upd(focusMsg{region: regionWorkerRail, index: pages.WorkerStepTx})
	if w.Step() != pages.WorkerStepTx {
		t.Fatalf("a click on the current step must stay inert, step = %d", w.Step())
	}
}

// TestClickSendRailSelectsStep walks the send wizard's rail: the
// backward click on the rail's first step is a free revisit, and a
// forward click never teleports to the clicked step — it replays the
// current step's Enter leg and lands at most one gated step forward.
func TestClickSendRailSelectsStep(t *testing.T) {
	r := wizardTestRoot(t)
	r.upd(palette.OpenSendWizardMsg{})
	// Land on the file step through the root's own transition (the proven
	// browse feeds-back sequence, TestRootWizardBrowseOpensPickerAndFeedsBack).
	r.upd(pages.WizardBrowseMsg{IsSpec: true})
	if r.m.filePick == nil {
		t.Fatal("precondition: [f] must open the shared file picker over the wizard")
	}
	r.upd(widgets.FilePickedMsg{Path: "../../specs/spec.json"})
	w := r.m.wizard
	if w == nil || w.Step() != 1 {
		t.Fatalf("precondition: the spec selection must advance the wizard to the file step, got %#v", w)
	}

	v := r.m.View()
	row := focusRowAt(t, r.m.sendRailRowHits(), 0)
	if line := contentLineAt(v, row.Y); !strings.Contains(line, "1 connect") {
		t.Fatalf("the rail step-1 rect must sit on the drawn rail line (y %d): %q", row.Y, line)
	}
	cmd := v.OnMouse(tea.MouseClickMsg{X: row.X + row.W/2, Y: row.Y, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("a rail-label click must emit a focus msg, got nil")
	}
	if got := cmd(); got != (focusMsg{region: regionSendRail, index: 0}) {
		t.Fatalf("rail click = %#v, want the step-1 focus hit", got)
	}
	r.upd(cmd())
	if w.Step() != 0 {
		t.Fatalf("backward rail click = step %d, want 0 (free revisit)", w.Step())
	}

	// Forward click: clicking "3 send" from the spec step replays the
	// spec step's Enter leg — the whole cmd chain runs (the program pump,
	// done by hand) and the walk lands AT MOST one gated step forward; it
	// must NOT teleport to the clicked step.
	r.updChain(focusMsg{region: regionSendRail, index: 2})
	if w.Step() == 2 {
		t.Fatal("a forward click must not bypass the step's Enter gate and teleport to the clicked step")
	}
}

// TestClickAnalyzeRailSelectsStep walks the §J analyze rail (the page-side
// seam): the rail labels publish as pages.Focuser regions on the page's
// first line, a backward click jumps free through root's
// handleAnalyzeStepDelta (the PgUp path), a forward click never teleports
// past the gates (it runs the current step's Enter transition), and a
// click on the current step is inert.
func TestClickAnalyzeRailSelectsStep(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('7'))
	m.analyzeStep = pages.StepRun
	m.syncAnalyze()
	v := m.View()

	// Backward click "1 capture": a free revisit, resolved through the
	// real OnMouse path on the drawn label.
	row := focusRowAt(t, m.analyzeRailRowHits(), pages.StepCapture)
	if line := contentLineAt(v, row.Y); !strings.Contains(line, "1 capture") {
		t.Fatalf("the rail step-1 rect must sit on the drawn rail line (y %d): %q", row.Y, line)
	}
	cmd := v.OnMouse(tea.MouseClickMsg{X: row.X + row.W/2, Y: row.Y, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("a rail-label click must emit a focus msg, got nil")
	}
	if got := cmd(); got != (focusMsg{region: pages.RegionAnalyzeRail, index: pages.StepCapture}) {
		t.Fatalf("rail click = %#v, want the analyze-rail step-1 focus hit", got)
	}
	if _, _ = m.Update(cmd()); m.analyzeStep != pages.StepCapture {
		t.Fatalf("backward rail click = step %d, want %d (free revisit)", m.analyzeStep, pages.StepCapture)
	}

	// Forward click "4 run" from capture: the capture step's own Enter
	// transition runs (with no capture to commit it opens the browse, the
	// Enter path's behaviour) — the step must NOT teleport to run.
	if _, _ = m.Update(focusMsg{region: pages.RegionAnalyzeRail, index: pages.StepRun}); m.analyzeStep == pages.StepRun {
		t.Fatal("a forward click must not teleport past the step gates to the clicked step")
	}
	if m.filePick != nil {
		m.filePick = nil // close the browse the gated transition opened
	}

	// The current step: inert.
	if _, _ = m.Update(focusMsg{region: pages.RegionAnalyzeRail, index: pages.StepCapture}); m.analyzeStep != pages.StepCapture {
		t.Fatalf("a click on the current step must stay inert, step = %d", m.analyzeStep)
	}
}
