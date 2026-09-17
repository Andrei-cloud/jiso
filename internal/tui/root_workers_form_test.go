// root_workers_form_test.go pins the §H worker wizard's root legs
// The wizard opens only on the workers page
// (b/t), prefills from the SAME sources the legacy paths use (bgsend =
// REPL survey defaults + repository ListNames; stress = cobra
// flag defaults), starts through the injectable App leg as a Cmd,
// keeps the wizard open (with the error line) on failure, validates
// bounds before the leg runs, and the bgsend step is a single-select
// (space never toggles). The wizard-side units (5-row scroll window,
// multi-select, filter) live in pages/worker_wizard_test.go.
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

type formTestRoot struct {
	workerTestRoot
	mu       sync.Mutex
	starts   []string
	interval time.Duration
	count    int
	names    []string
	tps      int
	ramp     time.Duration
	dur      time.Duration
	workers  int
	startErr error
}

func newFormTestRoot(t *testing.T) *formTestRoot {
	t.Helper()

	a := newTxFileApp(t)
	r := &formTestRoot{workerTestRoot: workerTestRoot{
		m:     NewRootModel(a),
		clock: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
	}}
	r.m.now = func() time.Time { return r.clock }
	r.m.workerStartFn = func(name string, count int, interval time.Duration) (string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.starts = append(r.starts, name)
		r.count, r.interval = count, interval

		return "w-9", r.startErr
	}
	r.m.stressStartFn = func(names []string, tps int, ramp, duration time.Duration, workers int) (string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.names = append([]string(nil), names...)
		r.tps, r.ramp, r.dur, r.workers = tps, ramp, duration, workers

		return "w-9", r.startErr
	}
	_, _ = r.m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.gotoPage()

	return r
}

// walk drives the wizard with Enter from its tx step through the run
// step (one Enter per step: tx ▸ params ▸ run ▸ start).
func (r *formTestRoot) walk(enters int) {
	for i := 0; i < enters; i++ {
		r.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
}

func TestWorkersFormsOpenOnlyOnWorkersPage(t *testing.T) {
	r := newFormTestRoot(t)
	r.upd(ch('1'))
	r.key(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if r.m.workerWiz != nil {
		t.Fatal("b opened the wizard off the workers page")
	}
	r.gotoPage()
	r.key(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if r.m.workerWiz == nil {
		t.Fatal("b must open the bgsend wizard on the workers page")
	}
	if r.m.workerWiz.Mode() != pages.WorkerModeBg {
		t.Errorf("mode = %q, want bgsend", r.m.workerWiz.Mode())
	}
}

func TestBgFormPrefillFromSurveyDefaults(t *testing.T) {
	r := newFormTestRoot(t)
	r.key(tea.KeyPressMsg{Code: 'b', Text: "b"})
	w := r.m.workerWiz
	if got := w.SelectedNames(); len(got) != 1 || got[0] != "Purchase" {
		t.Fatalf("tx prefill = %v, want the first repository name", got)
	}
	body := strings.Split(r.body(), "\n")
	if !strings.Contains(r.body(), "Purchase") || !strings.Contains(r.body(), "Sign On") {
		t.Fatalf("tx list lacks the repository ListNames:\n%s", strings.Join(body, "\n"))
	}

	r.upd(tea.KeyPressMsg{Code: tea.KeyEnter}) // step 1: pick the cursor row
	if w.Step() != pages.WorkerStepParams {
		t.Fatalf("step = %d, want the params step", w.Step())
	}
	if got := w.Param(pages.WorkerParamInterval); got != pages.WorkerDefaultInterval {
		t.Fatalf("interval prefill = %q, want %q (REPL bgsend survey)", got, pages.WorkerDefaultInterval)
	}
	if got := w.Param(pages.WorkerParamCount); got != pages.WorkerDefaultCount {
		t.Fatalf("count prefill = %q, want %q", got, pages.WorkerDefaultCount)
	}
}

func TestBgWizardSingleSelect(t *testing.T) {
	r := newFormTestRoot(t)
	r.key(tea.KeyPressMsg{Code: 'b', Text: "b"})
	w := r.m.workerWiz
	r.upd(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // never toggles
	if body := w.View(); strings.Contains(body, "[x]") {
		t.Fatalf("bgsend must not render checkboxes:\n%s", body)
	}
	if got := w.SelectedNames(); len(got) != 1 || got[0] != "Purchase" {
		t.Fatalf("selection = %v, want the single seeded pick", got)
	}
}

func TestBgFormStartHappyPath(t *testing.T) {
	r := newFormTestRoot(t)
	r.key(tea.KeyPressMsg{Code: 'b', Text: "b"})
	r.walk(3) // tx ▸ params ▸ run ▸ start
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.starts) != 1 || r.starts[0] != "Purchase" {
		t.Fatalf("starts = %v, want [Purchase]", r.starts)
	}
	if r.count != 1 || r.interval != time.Second {
		t.Fatalf("start args count=%d interval=%v, want 1 and 1s", r.count, r.interval)
	}
	if r.m.workerWiz != nil {
		t.Fatal("a successful start must close the wizard")
	}
}

func TestBgFormStartErrorKeepsFormOpen(t *testing.T) {
	r := newFormTestRoot(t)
	r.mu.Lock()
	r.startErr = errBoom
	r.mu.Unlock()
	r.key(tea.KeyPressMsg{Code: 'b', Text: "b"})
	r.walk(3)
	if r.m.workerWiz == nil {
		t.Fatal("a failed start must keep the wizard open")
	}
	if !strings.Contains(r.body(), "boom") {
		t.Fatalf("error line missing from the open wizard:\n%s", r.body())
	}
}

var errBoom = &staticErr{}

type staticErr struct{}

func (staticErr) Error() string { return "boom" }

// "?" in navigate mode opens the overlay (esc closes it, the wizard stays
// open); once a field is being typed into, "?" types into it.
func TestWorkerWizardHelpEscape(t *testing.T) {
	r := newFormTestRoot(t)
	r.key(ch('t'))
	r.key(ch('?'))
	if r.m.help == nil {
		t.Fatal("? must open the help overlay from the fresh wizard")
	}
	r.key(special(tea.KeyEscape)) // closes the overlay, not the wizard
	if r.m.help != nil {
		t.Fatal("esc must close the overlay first")
	}
	if r.m.workerWiz == nil {
		t.Fatal("the wizard must stay open under the overlay")
	}
	r.key(ch('/'))
	r.key(ch('x'))
	r.key(ch('?'))
	if r.m.help != nil {
		t.Fatal(`"? typed into a filter must not open help`)
	}
	if !strings.Contains(r.body(), "x?") {
		t.Errorf("the ? must have reached the filter:\n%s", r.body())
	}

	// The param step opens in navigate mode: "?" is still the help key.
	// Typing enters edit mode, where "?" types literally.
	r.key(special(tea.KeyEscape)) // leave the filter (navigate mode)
	if r.m.workerWiz == nil {
		t.Fatal("leaving the filter must not close the wizard")
	}
	r.key(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // toggle a row
	r.key(tea.KeyPressMsg{Code: tea.KeyEnter})            // commit ▸ params
	if r.m.workerWiz.Step() != pages.WorkerStepParams {
		t.Fatalf("step = %d, want the params step", r.m.workerWiz.Step())
	}
	r.key(ch('?'))
	if r.m.help == nil {
		t.Fatal("? on the navigate-mode param step must open the help overlay")
	}
	r.key(special(tea.KeyEscape)) // closes the overlay, not the wizard
	if r.m.workerWiz == nil {
		t.Fatal("the wizard must survive the overlay")
	}
	r.key(ch('7')) // typing enters edit mode
	if !r.m.workerWiz.Editing() {
		t.Fatal("typing into a param row must enter edit mode")
	}
	r.key(ch('?'))
	if r.m.help != nil {
		t.Fatal(`"? while editing must type into the row, not open help`)
	}
}

// TestWorkerWizardTxFilePickRefreshes: the [f] pick commits through
// the §L ApplySettings seam, the wizard candidates refresh from the
// reloaded repository, the selection survives by name, and the wizard
// stays on step 1.
func TestWorkerWizardTxFilePickRefreshes(t *testing.T) {
	r := newFormTestRoot(t)
	dir := t.TempDir()
	other := filepath.Join(dir, "pool2.json")
	txJSON := `[{"type":"transaction","name":"Purchase","description":"kept",` +
		`"fields":{"0":"0200"}},{"type":"transaction","name":"Refund","description":"new",` +
		`"fields":{"0":"0220"}}]`
	if err := os.WriteFile(other, []byte(txJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	r.key(ch('t'))
	r.key(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}) // Purchase toggled
	r.key(ch('f'))
	if r.m.filePick == nil {
		t.Fatal("[f] must open the picker")
	}
	r.key(widgets.FilePickedMsg{Path: other})

	w := r.m.workerWiz
	if w == nil {
		t.Fatal("the wizard must stay open after the pick")
	}
	if w.Step() != pages.WorkerStepTx {
		t.Errorf("step = %d, want to stay on the tx step", w.Step())
	}
	if got := w.SelectedNames(); len(got) != 1 || got[0] != "Purchase" {
		t.Fatalf("selection after refresh = %v, want [Purchase]", got)
	}
	body := r.body()
	if !strings.Contains(body, "Refund") {
		t.Errorf("candidates must come from the reloaded repository:\n%s", body)
	}
	if !strings.Contains(body, "tx file loaded: pool2.json") {
		t.Errorf("success toast missing:\n%s", body)
	}
}
