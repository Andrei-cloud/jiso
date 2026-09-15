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

// drawnLine returns the frame line whose raw text contains label (field
// labels draw as raw text — the existing drawn-y pins rely on the same
// fact) — the position a click must land on INDEPENDENT of the y maths
// under test.
func drawnLine(t *testing.T, v tea.View, label string) int {
	t.Helper()

	for i, line := range strings.Split(strings.TrimRight(v.Content, "\n"), "\n") {
		if strings.Contains(line, label) {
			return i
		}
	}
	t.Fatalf("no drawn line contains %q:\n%s", label, v.Content)

	return -1
}

// TestClickFocusBelowOpenHeaderPicker pins the drawn-ink alignment of the
// field-row rects WHILE the header picker overlay is open (review fix):
// the overlay occupies exactly Height(pickerBox) lines — the '\n'
// between the picker row and the overlay starts the overlay's first
// line, it adds no blank line — so every field BELOW the overlay must
// keep its rect on its own drawn line. With the off-by-one each below
// rect sat one row too low: clicking the drawn "Station ID" line emitted
// nothing (dead), and the drawn "Unsolicited"/"TLS" lines focused the
// neighbour one row above. The dim (visa-only) station row additionally
// pins the Tab-like enabled-field clamp a click inherits from SetFocus.
func TestClickFocusBelowOpenHeaderPicker(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	_, _ = m.Update(ch('c'))
	header := dlgFieldIdx(t, m.dlg, pages.ConnectFieldHeader)

	for _, key := range []string{
		pages.ConnectFieldStation, pages.ConnectFieldUnsolicited, pages.ConnectFieldTLS,
	} {
		idx := dlgFieldIdx(t, m.dlg, key)
		label := ""
		for _, f := range m.dlg.State().Fields {
			if f.Key == key {
				label = f.Label

				break
			}
		}

		// Re-open the overlay fresh per leg: the previous leg's click
		// closed it (carry 3), so each leg measures its drawn line against
		// a freshly opened overlay.
		if _, _ = m.Update(focusMsg{region: regionConnectForm, index: header}); m.dlg.Focus() != header {
			t.Fatalf("precondition: click-focus must land on the picker row for the %s leg", key)
		}
		_, _ = m.Update(ch(' ')) // the keyboard's space toggle
		if !m.dlg.PickerOpen() {
			t.Fatalf("precondition: the header overlay must be open for the %s leg", key)
		}

		v := m.View()
		y := drawnLine(t, v, label)
		// X/width are shared by every row and unaffected by the y maths,
		// so the click's x may come from the published picker row.
		phit := focusRowAt(t, m.connectFormRowHits(), header)
		cmd := v.OnMouse(tea.MouseClickMsg{X: phit.X + phit.W/2, Y: y, Button: tea.MouseLeft})
		if cmd == nil {
			t.Fatalf("a click on the drawn %q line (y %d) emitted nothing; the field's rect is offset from its drawn ink", label, y)
		}
		want := focusMsg{region: regionConnectForm, index: idx}
		if got := cmd(); got != want {
			t.Fatalf("click on the drawn %q line = %#v, want %#v (the rect must sit on its own drawn ink)", label, got, want)
		}

		// The resolved click focuses the field itself; a DIM row (station
		// is visa-only, disabled under the default header) clamps onto the
		// nearest enabled field exactly like Tab does.
		enabled := m.dlg.State().Fields[idx].Enabled
		if _, _ = m.Update(want); enabled && m.dlg.Focus() != idx {
			t.Fatalf("after click-focus on the drawn %q line Focus() = %d, want %d", label, m.dlg.Focus(), idx)
		}
		if !enabled && !m.dlg.State().Fields[m.dlg.Focus()].Enabled {
			t.Fatalf("a click on the dim %q row must clamp onto an enabled field like Tab, focus = %d", label, m.dlg.Focus())
		}
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

	// Forward GAP: clicking "3 run" from tx would skip the params step,
	// so the click stays inert — only the immediately next step replays
	// the Enter leg.
	r.upd(focusMsg{region: regionWorkerRail, index: pages.WorkerStepRun})
	if w.Step() != pages.WorkerStepTx {
		t.Fatalf("forward-gap rail click = step %d, want %d (inert: only the next step advances)", w.Step(), pages.WorkerStepTx)
	}

	// The current step: inert.
	r.upd(focusMsg{region: regionWorkerRail, index: pages.WorkerStepTx})
	if w.Step() != pages.WorkerStepTx {
		t.Fatalf("a click on the current step must stay inert, step = %d", w.Step())
	}
}

// TestClickSendRailSelectsStep walks the send wizard's rail: the
// backward click on the rail's first step is a free revisit, and a
// forward click never teleports to the clicked step — only the
// immediately next step's Enter leg replays, larger forward gaps are
// inert.
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

	// Forward GAP: clicking "3 send" from the first step (gap 2) is
	// inert — the rail cannot skip the spec/file steps, and only the
	// immediately next step's Enter leg may be replayed.
	r.updChain(focusMsg{region: regionSendRail, index: 2})
	if w.Step() != 0 {
		t.Fatalf("forward-gap rail click = step %d, want 0 (inert: only the next step advances)", w.Step())
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
