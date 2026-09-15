// worker_wizard_test.go covers the §H worker-wizard page units (UAT
// round 4): the per-mode rails, the scrollable 5-row transaction
// window with its "▴ n above" / "v n below" affordances and the
// minimum-scroll cursor model, the "/" filter narrowing, the stress
// multi-select (space toggles + "N selected") versus the bgsend
// single-select, the step-2 inline validation strings (the exact texts
// the retired §N2 forms carried), Enter-to-start messages, Esc
// walking, the keyboard-claim/Editing contract, selection survival
// across a tx-file refresh, and the width guarantee (every line
// clipped, never wrapped). The start legs are root-side
// (root_stress_form_test.go / root_workers_form_test.go).
package pages

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"jiso/internal/tui/frame"
)

// workerTxNames builds n candidates named tx-01..tx-nn.
func workerTxNames(n int) []WizardItem {
	items := make([]WizardItem, 0, n)
	for i := 1; i <= n; i++ {
		name := "tx-" + strconv.Itoa(i)
		if i < 10 {
			name = "tx-0" + strconv.Itoa(i)
		}
		items = append(items, WizardItem{Label: name, Path: name})
	}

	return items
}

// workerWizardAt builds an ascii wizard of the given mode with n
// candidates at the given terminal size (the analyzePage idiom).
func workerWizardAt(t *testing.T, mode string, n, w, h int) *WorkerWizard {
	t.Helper()
	wz := NewWorkerWizard(asciiTheme(t), mode)
	wz.SetState(WorkerWizardState{TxItems: workerTxNames(n)})
	wz.HomeSelection()
	_, _ = wz.Update(windowSize(w, h))

	return wz
}

// body is the wizard's rendered box, escape-stripped.
func workerBody(wz *WorkerWizard) string { return ansi.Strip(wz.View()) }

// txRowLines counts the candidate rows currently rendered.
func workerCountRows(t *testing.T, body string) int {
	t.Helper()
	n := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "tx-") && !strings.Contains(line, "filter") {
			n++
		}
	}

	return n
}

func TestWorkerWizardRails(t *testing.T) {
	t.Parallel()

	s := workerBody(workerWizardAt(t, WorkerModeStress, 2, 120, 32))
	for _, want := range []string{"STRESS", "1 tx", "2 rate", "3 run"} {
		if !strings.Contains(s, want) {
			t.Errorf("stress rail lacks %q:\n%s", want, s)
		}
	}
	b := workerBody(workerWizardAt(t, WorkerModeBg, 2, 120, 32))
	for _, want := range []string{"BGSEND", "1 tx", "2 params", "3 run"} {
		if !strings.Contains(b, want) {
			t.Errorf("bgsend rail lacks %q:\n%s", want, b)
		}
	}
}

// TestWorkerWizardFiveRowWindow: 20 candidates show exactly 5 rows and
// the "v 15 below" marker; j×6 scrolls the window the minimum distance
// (cursor 6 → top 2 → "▴ 2 above" / "^ 2 above"), and the "/" filter
// narrows the list.
func TestWorkerWizardFiveRowWindow(t *testing.T) {
	t.Parallel()

	wz := workerWizardAt(t, WorkerModeStress, 20, 120, 32)
	body := workerBody(wz)

	if got := workerCountRows(t, body); got != WorkerTxVisibleRows {
		t.Errorf("visible rows = %d, want exactly %d:\n%s", got, WorkerTxVisibleRows, body)
	}
	for _, want := range []string{"tx-01", "tx-05", "v 15 below"} {
		if !strings.Contains(body, want) {
			t.Errorf("window lacks %q:\n%s", want, body)
		}
	}
	for _, gone := range []string{"tx-06", "tx-20", "^ ", "▴"} {
		if strings.Contains(body, gone) {
			t.Errorf("row %q must be outside the closed window:\n%s", gone, body)
		}
	}

	// j×6: the window follows the cursor one row at a time (top 2), so
	// BOTH markers frame the five visible rows (UAT expectation).
	for i := 0; i < 6; i++ {
		_, _ = wz.Update(ch('j'))
	}
	body = workerBody(wz)
	if !strings.Contains(body, "^ 2 above") || !strings.Contains(body, "v 13 below") {
		t.Errorf("after j×6 the scroll markers are wrong:\n%s", body)
	}
	if got := workerCountRows(t, body); got != WorkerTxVisibleRows {
		t.Errorf("visible rows after scroll = %d, want %d", got, WorkerTxVisibleRows)
	}
	for _, want := range []string{"tx-03", "tx-07"} {
		if !strings.Contains(body, want) {
			t.Errorf("scrolled window lacks %q:\n%s", want, body)
		}
	}
	for _, gone := range []string{"tx-02", "tx-08"} {
		if strings.Contains(body, gone) {
			t.Errorf("%q must be outside the scrolled window:\n%s", gone, body)
		}
	}

	// The truecolor profile renders the ▴ marker form.
	tc := NewWorkerWizard(testTheme(t, colorprofile.TrueColor), WorkerModeStress)
	tc.SetState(WorkerWizardState{TxItems: workerTxNames(20)})
	_, _ = tc.Update(windowSize(120, 32))
	for i := 0; i < 6; i++ {
		_, _ = tc.Update(ch('j'))
	}
	if b := ansi.Strip(tc.View()); !strings.Contains(b, "\u25b4 2 above") {
		t.Errorf("truecolor must render the \u25b4 marker:\n%s", b)
	}

	// "/" + typing narrows the list (substring over label + path).
	wz = workerWizardAt(t, WorkerModeStress, 20, 120, 32)
	_, _ = wz.Update(ch('/'))
	for _, r := range "tx-1" {
		_, _ = wz.Update(ch(r))
	}
	body = workerBody(wz)
	if !strings.Contains(body, "tx-10") || strings.Contains(body, "tx-01") {
		t.Errorf("filter tx-1 must keep only tx-1x:\n%s", body)
	}
	if got := workerCountRows(t, body); got != WorkerTxVisibleRows {
		t.Errorf("filtered visible rows = %d, want %d", got, WorkerTxVisibleRows)
	}
	if !strings.Contains(body, "v 5 below") { // 10 matches, 5 visible
		t.Errorf("filtered window lacks the below marker:\n%s", body)
	}
}

func TestWorkerWizardScrollMarkersEmptyAndShort(t *testing.T) {
	t.Parallel()

	// Five or fewer candidates: no markers at all.
	body := workerBody(workerWizardAt(t, WorkerModeStress, 5, 120, 32))
	if strings.Contains(body, "below") || strings.Contains(body, "above") {
		t.Errorf("a list that fits must carry no scroll markers:\n%s", body)
	}
	// Empty repository: the empty state names the [f] browse.
	body = workerBody(workerWizardAt(t, WorkerModeStress, 0, 120, 32))
	if !strings.Contains(body, "no transactions loaded") || !strings.Contains(body, "[f] browse") {
		t.Errorf("empty state lacks the browse hint:\n%s", body)
	}
}

func TestWorkerWizardMultiSelectCount(t *testing.T) {
	t.Parallel()

	wz := workerWizardAt(t, WorkerModeStress, 7, 120, 32)
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	body := workerBody(wz)
	if !strings.Contains(body, "[x]") || !strings.Contains(body, "1 selected (space toggles)") {
		t.Errorf("toggled row/count line missing:\n%s", body)
	}
	_, _ = wz.Update(ch('j'))
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := wz.SelectedNames(); len(got) != 2 || got[0] != "tx-01" || got[1] != "tx-02" {
		t.Fatalf("selection = %v, want [tx-01 tx-02]", got)
	}
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := wz.SelectedNames(); len(got) != 1 {
		t.Fatalf("space must toggle back off, got %v", got)
	}
}

func TestWorkerWizardCommitGates(t *testing.T) {
	t.Parallel()

	// Stress Enter with nothing toggled stays on step 1 with the
	// retired form's exact error text.
	wz := workerWizardAt(t, WorkerModeStress, 3, 120, 32)
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if wz.Step() != WorkerStepTx || !strings.Contains(workerBody(wz), "select at least one transaction") {
		t.Errorf("empty stress Enter must stay with the select error:\n%s", workerBody(wz))
	}
	// Bgsend Enter picks the cursor row and advances (single-select).
	wz = workerWizardAt(t, WorkerModeBg, 3, 120, 32)
	_, _ = wz.Update(ch('j'))
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if wz.Step() != WorkerStepParams {
		t.Fatalf("bgsend Enter must advance, step = %d", wz.Step())
	}
	if got := wz.SelectedNames(); len(got) != 1 || got[0] != "tx-02" {
		t.Fatalf("picked = %v, want [tx-02]", got)
	}
	// Bgsend with an empty repository: Enter is the same select error.
	wz = workerWizardAt(t, WorkerModeBg, 0, 120, 32)
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if wz.Step() != WorkerStepTx || !strings.Contains(workerBody(wz), "select at least one transaction") {
		t.Error("empty bgsend Enter must stay with the select error")
	}
}

func TestWorkerWizardBrowseMsg(t *testing.T) {
	t.Parallel()

	wz := workerWizardAt(t, WorkerModeStress, 2, 120, 32)
	_, cmd := wz.Update(ch('f'))
	if _, ok := cmdMsg(t, cmd).(WorkerWizardBrowseMsg); !ok {
		t.Errorf("f -> %T, want WorkerWizardBrowseMsg", cmdMsg(t, cmd))
	}
}

// TestWorkerWizardStepTwoValidation drives the inline rows end-to-end
// (tps=0 through Enter) and pins the exact validation strings of both
// modes through the resolvers.
func TestWorkerWizardStepTwoValidation(t *testing.T) {
	t.Parallel()

	wz := workerWizardAt(t, WorkerModeStress, 2, 120, 32)
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // ▸ rate
	if wz.Step() != WorkerStepParams {
		t.Fatal("the wizard must be on the rate step")
	}
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	_, _ = wz.Update(ch('0'))
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if wz.Step() != WorkerStepParams {
		t.Fatalf("tps=0 must not advance, step = %d", wz.Step())
	}
	if !strings.Contains(workerBody(wz), "TPS must be greater than 0") {
		t.Errorf("inline bounds error missing:\n%s", workerBody(wz))
	}

	cases := []struct {
		name                    string
		tps, ramp, dur, workers string
		want                    string
	}{
		{"tps blank", "", "30s", "1m", "1", "please enter a valid number"},
		{"tps bound high", "100001", "30s", "1m", "1", "TPS cannot exceed 100000"},
		{"tps bound low", "0", "30s", "1m", "1", "TPS must be greater than 0"},
		{"workers bound high", "10", "30s", "1m", "51", "workers cannot exceed 50"},
		{"workers bound low", "10", "30s", "1m", "0", "workers must be greater than 0"},
		{"workers blank", "10", "30s", "1m", "", "please enter a valid number"},
		{"ramp unparsable", "10", "xx", "1m", "1", "please enter a valid duration"},
		{"ramp negative", "10", "-5s", "1m", "1", "ramp must not be negative, got -5s"},
		{"duration unparsable", "10", "30s", "nope", "1", "please enter a valid duration"},
		{"duration zero", "10", "30s", "0s", "1", "duration must be greater than 0, got 0s"},
	}
	for _, c := range cases {
		_, errText := ResolveStressRun([]string{"Purchase"}, c.tps, c.ramp, c.dur, c.workers)
		if errText != c.want {
			t.Errorf("%s: error = %q, want %q", c.name, errText, c.want)
		}
	}
	bgCases := []struct {
		name, tx, interval, count, want string
	}{
		{"no tx", "", "1s", "1", "select at least one transaction"},
		{"interval zero", "Purchase", "0s", "1", "interval must be greater than 0"},
		{"interval unparsable", "Purchase", "xx", "1", "interval must be greater than 0"},
		{"count zero", "Purchase", "1s", "0", "count must be a number greater than 0"},
		{"count blank", "Purchase", "1s", "", "count must be a number greater than 0"},
	}
	for _, c := range bgCases {
		_, errText := ResolveBgRun(c.tx, c.interval, c.count)
		if errText != c.want {
			t.Errorf("%s: error = %q, want %q", c.name, errText, c.want)
		}
	}
}

func TestWorkerWizardStartMessages(t *testing.T) {
	t.Parallel()

	// Stress: toggle tx-02, walk to the run step, Enter resolves the
	// cobra defaults with the selection.
	wz := workerWizardAt(t, WorkerModeStress, 3, 120, 32)
	_, _ = wz.Update(ch('j'))
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // ▸ rate
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // ▸ run
	if body := workerBody(wz); !strings.Contains(body, "tx-02") || !strings.Contains(body, "1m") {
		t.Errorf("run summary lacks the selection/params:\n%s", body)
	}
	_, cmd := wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok := cmdMsg(t, cmd).(WorkerWizardStartMsg)
	if !ok {
		t.Fatalf("Enter -> %T, want WorkerWizardStartMsg", cmdMsg(t, cmd))
	}
	run := msg.Run
	if run.Mode != WorkerModeStress || len(run.Names) != 1 || run.Names[0] != "tx-02" ||
		run.Tps != 10 || run.Ramp != 30*time.Second || run.Duration != time.Minute || run.Workers != 1 {
		t.Errorf("stress run = %+v, want the cobra defaults over [tx-02]", run)
	}

	// Bgsend: the seeded pick + survey defaults.
	wz = workerWizardAt(t, WorkerModeBg, 3, 120, 32)
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // pick ▸ params
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // ▸ run
	_, cmd = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg, ok = cmdMsg(t, cmd).(WorkerWizardStartMsg)
	if !ok {
		t.Fatalf("Enter -> %T, want WorkerWizardStartMsg", cmdMsg(t, cmd))
	}
	if msg.Run.Mode != WorkerModeBg || msg.Run.Name != "tx-01" ||
		msg.Run.Count != 1 || msg.Run.Interval != time.Second {
		t.Errorf("bgsend run = %+v, want tx-01 x1 @1s", msg.Run)
	}
}

func TestWorkerWizardEscWalksAndCloses(t *testing.T) {
	t.Parallel()

	wz := workerWizardAt(t, WorkerModeStress, 3, 120, 32)
	// Esc with an open filter clears it first (no step message).
	_, _ = wz.Update(ch('/'))
	_, _ = wz.Update(ch('x'))
	_, cmd := wz.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Errorf("Esc with a draft -> %T, want nil (clear only)", cmdMsg(t, cmd))
	}
	// Walk the wizard up: run <- rate <- tx, then close on step 1.
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for want := WorkerStepTx; want < WorkerStepRun; want++ {
		_, cmd = wz.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if cmd != nil {
			t.Fatalf("Esc on step %d -> %T, want nil (step back)", wz.Step(), cmdMsg(t, cmd))
		}
	}
	if wz.Step() != WorkerStepTx {
		t.Fatalf("step = %d, want the tx step", wz.Step())
	}
	_, cmd = wz.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := cmdMsg(t, cmd).(WorkerWizardCloseMsg); !ok {
		t.Errorf("Esc on step 1 -> %T, want WorkerWizardCloseMsg", cmdMsg(t, cmd))
	}
}

// TestWorkerWizardClaimAndEditing pins the two-mode keyboard contract of
// the modal (UAT round 8 / D3, the FreshDraft hatch's successor): every
// step opens in NAVIGATE mode (ClaimsKeyboard false — the root modal
// branch keeps "?" its §M help key then); opening the "/" filter or
// typing into a param row enters EDIT mode, which claims the keyboard so
// "?" types literally; esc leaves the field/first the filter before any
// step unwinds.
func TestWorkerWizardClaimAndEditing(t *testing.T) {
	t.Parallel()

	wz := workerWizardAt(t, WorkerModeStress, 3, 120, 32)
	if wz.ClaimsKeyboard() || wz.Editing() {
		t.Error("a fresh tx step must be navigate mode: no claim, ? stays a help key")
	}
	_, _ = wz.Update(ch('/'))
	if !wz.ClaimsKeyboard() || !wz.Editing() {
		t.Error("an opened filter is edit mode: it claims the keyboard (? types)")
	}
	_, _ = wz.Update(ch('x'))
	if !wz.Editing() {
		t.Error(`typing keeps edit mode ("?" is a filter byte)`)
	}
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if !wz.Editing() {
		t.Error("backspace keeps edit mode while the filter is open")
	}
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // first esc: close the filter
	if wz.filtering || wz.draft != "" || wz.Editing() {
		t.Error("esc must close the filter and land back in navigate mode")
	}

	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // toggle tx-01
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter})            // commit ▸ rate
	if wz.ClaimsKeyboard() || wz.Editing() {
		t.Fatal("the param step must open in navigate mode (? stays a help key)")
	}
	_, _ = wz.Update(ch('7')) // typing enters edit mode and types itself
	if !wz.ClaimsKeyboard() || !wz.Editing() {
		t.Fatal("typing into a param row must enter edit mode")
	}
	if got := wz.Param(WorkerParamTps); got != WorkerDefaultTps+"7" {
		t.Fatalf("tps = %q, want the typed suffix %q", got, WorkerDefaultTps+"7")
	}
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // esc leaves the field first
	if wz.Editing() {
		t.Fatal("esc must leave edit mode")
	}
	if wz.Step() != WorkerStepParams {
		t.Fatalf("esc left the step (step %d); it must stay on the param row", wz.Step())
	}
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // never types a space
	if wz.Param(WorkerParamTps) != WorkerDefaultTps+"7" {
		t.Error("space must not edit the focused param row")
	}
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEscape}) // esc in navigate mode: back one step
	if wz.Step() != WorkerStepTx {
		t.Fatalf("esc in navigate mode must back to the tx step, got step %d", wz.Step())
	}
}

func TestWorkerWizardSetStateKeepsSelection(t *testing.T) {
	t.Parallel()

	wz := workerWizardAt(t, WorkerModeStress, 6, 120, 32)
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // tx-01
	_, _ = wz.Update(ch('j'))
	_, _ = wz.Update(ch('j'))
	_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // tx-03

	// A refresh dropping tx-03 keeps tx-01 (UAT: the [f] pick keeps
	// selections whose names still exist).
	wz.SetState(WorkerWizardState{TxItems: []WizardItem{
		{Label: "tx-01", Path: "tx-01"}, {Label: "tx-05", Path: "tx-05"},
	}})
	if got := wz.SelectedNames(); len(got) != 1 || got[0] != "tx-01" {
		t.Fatalf("after refresh selection = %v, want [tx-01]", got)
	}

	// The bgsend pick vanishes with its name.
	b := workerWizardAt(t, WorkerModeBg, 3, 120, 32)
	b.SetState(WorkerWizardState{TxItems: []WizardItem{{Label: "tx-09", Path: "tx-09"}}})
	if got := b.SelectedNames(); len(got) != 0 {
		t.Fatalf("vanished pick = %v, want none", got)
	}
	b.SetState(WorkerWizardState{TxItems: []WizardItem{
		{Label: "tx-08", Path: "tx-08"}, {Label: "tx-07", Path: "tx-07"},
	}})
	if got := b.SelectedNames(); len(got) != 0 {
		t.Fatalf("step-1 refresh must not re-seed a vanished pick, got %v", got)
	}
}

// TestWorkerWizardNoLineExceedsContentWidth: at 160/120/100/80 columns
// (and the narrow box clamp) every rendered line stays inside the
// content width on every step, with long names and a full selection
// (clip, never wrap — the frame must never break).
func TestWorkerWizardNoLineExceedsContentWidth(t *testing.T) {
	t.Parallel()

	long := WizardItem{
		Label: "a-very-long-transaction-name-that-cannot-fit-anywhere",
		Path:  "a-very-long-transaction-name-that-cannot-fit-anywhere",
	}
	for _, mode := range []string{WorkerModeStress, WorkerModeBg} {
		for _, step := range []int{WorkerStepTx, WorkerStepParams, WorkerStepRun} {
			for _, w := range []int{160, 120, 100, 80, 48} {
				// Root sizes the modal to the CONTENT area (the send
				// wizard's innerWSOf wiring), so the wizard sees that.
				cw, chH := frame.ContentSize(w, 32)
				wz := NewWorkerWizard(asciiTheme(t), mode)
				items := append(workerTxNames(12), long)
				wz.SetState(WorkerWizardState{TxItems: items})
				wz.HomeSelection()
				_, _ = wz.Update(windowSize(cw, chH))
				for i := 0; i < 6; i++ {
					_, _ = wz.Update(ch('j')) // scroll the window
					if mode == WorkerModeStress {
						_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
					}
				}
				for i := wz.Step(); i < step; i++ {
					_, _ = wz.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				}
				if wz.Step() != step { // the gates must not trap the walk here
					t.Fatalf("mode %s could not reach step %d", mode, step)
				}
				for i, line := range strings.Split(wz.View(), "\n") {
					if lw := lipgloss.Width(line); lw > cw {
						t.Errorf("mode %s step %d width %d line %d overflows (%d > %d): %q",
							mode, step, w, i, lw, cw, line)
					}
				}
			}
		}
	}
}
