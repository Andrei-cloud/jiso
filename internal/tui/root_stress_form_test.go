// root_stress_form_test.go pins the §H stress start leg driven through
// the worker wizard (UAT round 4): [f] on the wizard's tx step opens
// the file picker and the picker — not the wizard — owns j/k (UAT: the
// cursor was frozen because the form swallowed keys); the step-2 rows
// prefill from the PAR-306 cobra flag defaults; bound violations are
// caught by the wizard's Enter gate and never reach the start leg;
// space toggles the multi-select; and the happy walk delivers the
// cobra-default parameters to the injectable StressStart leg, closing
// the wizard.
package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
)

// TestStressFormPickerOwnsKeys: [f] on the wizard's tx step opens the
// file picker, and the picker — not the wizard — owns j/k from then on.
func TestStressFormPickerOwnsKeys(t *testing.T) {
	r := newWorkerTestRoot(t)
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	if _, cmd := r.m.Update(ch('5')); cmd != nil && isQuit(t, cmd) {
		t.Fatal("unexpected quit")
	}
	r.key(ch('t'))
	if r.m.workerWiz == nil {
		t.Fatal("stress wizard did not open")
	}
	r.key(ch('f'))
	if r.m.filePick == nil {
		t.Fatal("[f] must open the tx-file picker")
	}

	cursorLine := func() int {
		for i, line := range strings.Split(r.m.filePick.View(), "\n") {
			trimmed := strings.TrimLeft(line, " ")
			if strings.HasPrefix(trimmed, "▸") || strings.HasPrefix(trimmed, ">") {
				return i
			}
		}

		return -1
	}
	before := cursorLine()
	if before < 0 {
		t.Fatal("picker view shows no cursor")
	}
	_, _ = r.m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if after := cursorLine(); after != before+1 {
		t.Fatalf("picker cursor stuck: line %d -> %d (wizard swallowed j)", before, after)
	}

	// Esc returns to the wizard, not past it (the cancel travels as a
	// cmd the program loop would pump).
	r.key(special(tea.KeyEscape))
	if r.m.filePick != nil {
		t.Fatal("esc must close the picker")
	}
	if r.m.workerWiz == nil {
		t.Fatal("the wizard must still be open after closing the picker")
	}
}

// TestStressFormPrefillMatchesCobraDefaults: the step-2 rows open with
// the `jiso stress` flag defaults.
func TestStressFormPrefillMatchesCobraDefaults(t *testing.T) {
	r := newFormTestRoot(t)
	r.key(tea.KeyPressMsg{Code: 't', Text: "t"})
	r.key(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // one tx toggled
	r.key(tea.KeyPressMsg{Code: tea.KeyEnter})            // tx ▸ rate
	w := r.m.workerWiz
	if w.Step() != pages.WorkerStepParams {
		t.Fatalf("step = %d, want the rate step", w.Step())
	}
	for _, tc := range []struct{ key, want string }{
		{pages.WorkerParamTps, pages.WorkerDefaultTps},
		{pages.WorkerParamRamp, pages.WorkerDefaultRamp},
		{pages.WorkerParamDuration, pages.WorkerDefaultDuration},
		{pages.WorkerParamWorkers, pages.WorkerDefaultWorkers},
	} {
		if got := w.Param(tc.key); got != tc.want {
			t.Errorf("%s prefill = %q, want %q (PAR-306 flag default)", tc.key, got, tc.want)
		}
	}
}

// TestStressFormBoundsStayOpen: tps=0 on the rate step must never
// reach the start leg; Enter shows the inline error and stays.
func TestStressFormBoundsStayOpen(t *testing.T) {
	r := newFormTestRoot(t)
	r.key(tea.KeyPressMsg{Code: 't', Text: "t"})
	r.key(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	r.key(tea.KeyPressMsg{Code: tea.KeyEnter}) // tx ▸ rate (tps row focused)
	for i := 0; i < 2; i++ {
		r.key(tea.KeyPressMsg{Code: tea.KeyBackspace}) // "10" -> ""
	}
	r.key(ch('0')) // "0"
	r.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	r.mu.Lock()
	n := len(r.names)
	r.mu.Unlock()
	if n != 0 {
		t.Fatalf("tps=0 must never reach the start leg (%d calls)", n)
	}
	if r.m.workerWiz == nil {
		t.Fatal("a bound violation must keep the wizard open")
	}
	if r.m.workerWiz.Step() != pages.WorkerStepParams {
		t.Errorf("step = %d, want to stay on the rate step", r.m.workerWiz.Step())
	}
	if body := r.body(); !strings.Contains(body, "TPS must be greater than 0") {
		t.Fatalf("inline bounds error missing:\n%s", body)
	}
}

// TestStressFormChecklistSpaceToggles: space flips the tx checkbox and
// the dim count line mirrors the toggle count.
func TestStressFormChecklistSpaceToggles(t *testing.T) {
	r := newFormTestRoot(t)
	r.key(tea.KeyPressMsg{Code: 't', Text: "t"})
	w := r.m.workerWiz
	r.key(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := w.SelectedNames(); len(got) != 1 || got[0] != "Purchase" {
		t.Fatalf("after space selection = %v, want [Purchase]", got)
	}
	if body := w.View(); !strings.Contains(body, "[x]") || !strings.Contains(body, "1 selected") {
		t.Fatalf("checked row / count line missing:\n%s", body)
	}
	r.key(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := w.SelectedNames(); len(got) != 0 {
		t.Fatalf("after second space selection = %v, want none", got)
	}
}

// TestStressFormHappyPathReachesLeg: toggle, walk the wizard, and the
// cobra-default parameters arrive at the injectable StressStart leg;
// success closes the wizard (the bus owns the table row).
func TestStressFormHappyPathReachesLeg(t *testing.T) {
	r := newFormTestRoot(t)
	r.key(tea.KeyPressMsg{Code: 't', Text: "t"})
	r.key(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	r.key(tea.KeyPressMsg{Code: tea.KeyEnter}) // tx ▸ rate
	r.key(tea.KeyPressMsg{Code: tea.KeyEnter}) // rate ▸ run
	r.key(tea.KeyPressMsg{Code: tea.KeyEnter}) // start
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.names) != 1 || r.names[0] != "Purchase" {
		t.Fatalf("stress start names = %v, want [Purchase]", r.names)
	}
	if r.tps != 10 || r.ramp != 30*time.Second || r.dur != time.Minute || r.workers != 1 {
		t.Fatalf("stress start args tps=%d ramp=%v dur=%v workers=%d", r.tps, r.ramp, r.dur, r.workers)
	}
	if r.m.workerWiz != nil {
		t.Fatal("a successful start must close the wizard")
	}
}
