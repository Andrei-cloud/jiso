// hitmap_focus_test.go pins click-to-focus: a left click on a drawn field
// row focuses it (navigate mode, never edit mode) and closes a stale
// header picker; field clicks are inert under the file picker or a
// confirm; wizard-rail clicks move through the wizard's own step rules.
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

// focusRowAt returns the absolute rect published for focus-row index.
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

// contentLineAt is the drawn frame line at absolute terminal row y.
func contentLineAt(v tea.View, y int) string {
	lines := strings.Split(strings.TrimRight(v.Content, "\n"), "\n")
	if y < 0 || y >= len(lines) {
		return ""
	}

	return lines[y]
}

// dlgFieldIdx resolves a named §E field's index in render order.
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

// pickerCovers reports whether a file-picker entry row covers (x,y);
// clicks over its ink resolve the picker's own hit.
func pickerCovers(m *RootModel, x, y int) bool {
	for _, r := range m.pickerRowHits() {
		if r.Rect.Contains(x, y) {
			return true
		}
	}

	return false
}

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

func TestClickFocusClosesHeaderPicker(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('c'))
	header := dlgFieldIdx(t, m.dlg, pages.ConnectFieldHeader)
	port := dlgFieldIdx(t, m.dlg, pages.ConnectFieldPort)

	// open the header overlay the way the keyboard does
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

// drawnLine returns the frame line index whose raw text contains label.
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

// while the header overlay is open, every field below it must keep its
// rect on its own drawn line (no off-by-one against the overlay's height).
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

		// re-open the overlay fresh per leg (the previous leg's click closed it)
		if _, _ = m.Update(focusMsg{region: regionConnectForm, index: header}); m.dlg.Focus() != header {
			t.Fatalf("precondition: click-focus must land on the picker row for the %s leg", key)
		}
		_, _ = m.Update(ch(' ')) // the keyboard's space toggle
		if !m.dlg.PickerOpen() {
			t.Fatalf("precondition: the header overlay must be open for the %s leg", key)
		}

		v := m.View()
		y := drawnLine(t, v, label)
		// x comes from the picker row; only y is under test
		phit := focusRowAt(t, m.connectFormRowHits(), header)
		cmd := v.OnMouse(tea.MouseClickMsg{X: phit.X + phit.W/2, Y: y, Button: tea.MouseLeft})
		if cmd == nil {
			t.Fatalf("a click on the drawn %q line (y %d) emitted nothing; the field's rect is offset from its drawn ink", label, y)
		}
		want := focusMsg{region: regionConnectForm, index: idx}
		if got := cmd(); got != want {
			t.Fatalf("click on the drawn %q line = %#v, want %#v (the rect must sit on its own drawn ink)", label, got, want)
		}

		// a dim (disabled) row clamps onto the nearest enabled field like Tab
		enabled := m.dlg.State().Fields[idx].Enabled
		if _, _ = m.Update(want); enabled && m.dlg.Focus() != idx {
			t.Fatalf("after click-focus on the drawn %q line Focus() = %d, want %d", label, m.dlg.Focus(), idx)
		}
		if !enabled && !m.dlg.State().Fields[m.dlg.Focus()].Enabled {
			t.Fatalf("a click on the dim %q row must clamp onto an enabled field like Tab, focus = %d", label, m.dlg.Focus())
		}
	}
}

// form focus msgs stay inert while the file picker owns the input (the
// narrow guard, not the full modalOpen gate).
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

	// a straggler focusMsg must not move focus while the picker owns the input
	r.upd(focusMsg{region: regionServerForm, index: port})
	if r.m.serverDlg.Focus() != routes {
		t.Fatalf("picker open: Focus() = %d, want %d (inert behind the picker)", r.m.serverDlg.Focus(), routes)
	}

	// the physical path: a click on an uncovered Port-row cell moves no focus
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

// a pending confirm draws above the forms and owns the input; field clicks stay inert.
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

// forward rail clicks replay the current step's Enter leg (validation not
// bypassed); backward is a free revisit; current-step clicks are inert.
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

	// forward click goes through the Enter leg, not around it
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

	// backward click: free revisit
	_ = r.m.View()
	r.upd(focusMsg{region: regionWorkerRail, index: pages.WorkerStepTx})
	if w.Step() != pages.WorkerStepTx {
		t.Fatalf("backward rail click = step %d, want %d (free revisit)", w.Step(), pages.WorkerStepTx)
	}

	// forward gap (skipping params) stays inert: only the next step advances
	r.upd(focusMsg{region: regionWorkerRail, index: pages.WorkerStepRun})
	if w.Step() != pages.WorkerStepTx {
		t.Fatalf("forward-gap rail click = step %d, want %d (inert: only the next step advances)", w.Step(), pages.WorkerStepTx)
	}

	r.upd(focusMsg{region: regionWorkerRail, index: pages.WorkerStepTx})
	if w.Step() != pages.WorkerStepTx {
		t.Fatalf("a click on the current step must stay inert, step = %d", w.Step())
	}
}

// backward rail clicks are free revisits; forward never teleports — only
// the immediately next step's Enter leg replays.
func TestClickSendRailSelectsStep(t *testing.T) {
	r := wizardTestRoot(t)
	r.upd(palette.OpenSendWizardMsg{})
	// land on the file step through the root's own browse sequence
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

	// forward gap stays inert: the rail cannot skip the spec/file steps
	r.updChain(focusMsg{region: regionSendRail, index: 2})
	if w.Step() != 0 {
		t.Fatalf("forward-gap rail click = step %d, want 0 (inert: only the next step advances)", w.Step())
	}
}

// analyze rail: backward clicks jump free, a forward click runs the current
// step's Enter transition (never teleports), current-step clicks are inert.
func TestClickAnalyzeRailSelectsStep(t *testing.T) {
	m := NewRootModel(nil)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(ch('7'))
	m.analyzeStep = pages.StepRun
	m.syncAnalyze()
	v := m.View()

	// backward click: free revisit via the real OnMouse path
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

	// forward click must not teleport: capture's Enter transition runs
	if _, _ = m.Update(focusMsg{region: pages.RegionAnalyzeRail, index: pages.StepRun}); m.analyzeStep == pages.StepRun {
		t.Fatal("a forward click must not teleport past the step gates to the clicked step")
	}
	if m.filePick != nil {
		m.filePick = nil // close the browse the gated transition opened
	}

	if _, _ = m.Update(focusMsg{region: pages.RegionAnalyzeRail, index: pages.StepCapture}); m.analyzeStep != pages.StepCapture {
		t.Fatalf("a click on the current step must stay inert, step = %d", m.analyzeStep)
	}
}
