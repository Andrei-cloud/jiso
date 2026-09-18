// root_scenario_specgate_test.go pins the scenario spec gates: a run
// whose steps would resolve through the engine default spec must wait in
// the shared spec browse instead of starting (a spec pick continues it on
// the chosen spec; Esc drops it with a one-line notice and no run), and a
// step preview that would compose against the default shows an honest
// line and opens the same browse (the pick re-arms the compose; Esc keeps
// the line and the keys). Scenarios whose steps all declare specs, and
// any scenario under an explicit spec, stay prompt-free.
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// scenarioGateJSON: Echo declares no spec, Signed declares one; "Specless
// Run" steps fall back (referenced and template-less), "Declared Run"
// does not.
const scenarioGateJSON = `[
 {"type":"transaction","name":"Echo","description":"Echo request","fields":{"0":"0800"}},
 {"type":"transaction","name":"Signed","description":"Signed request","fields":{"0":"0800"},"spec":"%s"},
 {"type":"scenario","name":"Specless Run","description":"steps without specs","steps":[
   {"name":"Echo Step","use_transaction_id":"Echo"},
   {"name":"Fresh Step","fields":{"0":"0800"}}]},
 {"type":"scenario","name":"Declared Run","description":"every step declares","steps":[
   {"name":"Signed Step","use_transaction_id":"Signed"}]}
]`

// scenSpecGateFix is one temp dir holding the chosen spec copy and the
// scenario file, over a real app rooted at §F.
type scenSpecGateFix struct {
	r      *specGateRoot
	dir    string
	chosen string
	txs    string
}

// newScenSpecGateFix builds the app §7 describes: the live collection
// composes against the engine default while cfg.GetSpec() stays empty
// (the file lands through the settings leg, which the startup guard does
// not bound), or the explicit-spec variant with the same file.
func newScenSpecGateFix(t *testing.T, explicitSpec bool) *scenSpecGateFix {
	t.Helper()

	t.Setenv("JISO_STATE_DIR", t.TempDir())
	dir := t.TempDir()
	f := &scenSpecGateFix{dir: dir}
	f.chosen = filepath.Join(dir, "chosen.json")
	f.txs = filepath.Join(dir, "scenarios.json")

	flex, err := os.ReadFile(filepath.Join("..", "..", "specs", "flex.json"))
	if err != nil {
		t.Fatalf("read flex spec: %v", err)
	}
	write := func(path string, body []byte) {
		t.Helper()

		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	write(f.chosen, flex)
	write(f.txs, []byte(fmt.Sprintf(scenarioGateJSON, f.chosen)))

	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")
	if explicitSpec {
		cfg.SetSpec(filepath.Join("..", "..", "specs", "spec.json"))
		cfg.SetFile(f.txs)
	}

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	time.Sleep(20 * time.Millisecond) // let app construction goroutines settle

	if !explicitSpec {
		if errs := a.ApplySettings(context.Background(), map[string]string{app.SettingTxFile: f.txs}); len(errs) != 0 {
			t.Fatalf("land scenarios: %v", errs)
		}
	}

	r := &specGateRoot{t: t, m: NewRootModel(a)}
	r.m.toastTickf = func(time.Duration, func() tea.Msg) tea.Cmd { return nil }
	r.pump(tea.WindowSizeMsg{Width: 120, Height: 32})
	r.pump(ch('3'))
	f.r = r

	return f
}

// wireRunLegs installs the sender seam and a recording fake engine; it
// reports the collector and a channel naming each started run.
func (f *scenSpecGateFix) wireRunLegs() (*scenCollector, chan string) {
	col := &scenCollector{msgs: make(chan tea.Msg, 32)}
	f.r.m.SetScenarioSender(func(msg tea.Msg) { col.msgs <- msg })
	started := make(chan string, 1)
	f.r.m.runScenario = func(name string, observe func(transactions.StepProgress)) (*transactions.TestReport, error) {
		started <- name

		return &transactions.TestReport{
			ScenarioName: name, Success: true,
			Steps: []transactions.StepResult{{StepName: "Step 1", Success: true, LatencyMs: 2}},
		}, nil
	}

	return col, started
}

// drain closes the live run through the collector the way the program
// would feed the per-step msgs back into Update.
func (f *scenSpecGateFix) drain(col *scenCollector) {
	for _, msg := range col.nextAll(f.r.t) {
		f.r.pump(msg)
	}
}

func TestRootScenarioRunGatesSpeclessSteps(t *testing.T) {
	f := newScenSpecGateFix(t, false)
	f.r.pump(pages.ScenarioRunMsg{ID: "Specless Run"})

	m := f.r.m
	if m.scenarioRun != nil {
		t.Fatal("the gated run must not start while the spec is unchosen")
	}
	if m.filePick == nil {
		t.Fatal("a run that would resolve a step via the engine default must open the spec browse")
	}
	if m.filePickTarget != "scenario:runspec" {
		t.Fatalf("picker target = %q, want scenario:runspec", m.filePickTarget)
	}
	if m.pendingScenarioRun != "Specless Run" {
		t.Fatalf("pending run = %q, want the gated scenario", m.pendingScenarioRun)
	}
	if got := m.filePick.CurrentDir(); got != "/" {
		t.Errorf("the spec browse starts at the root while no spec is set: %q", got)
	}
	if m.toast != nil && m.toast.Len() != 0 {
		t.Error("gating alone must not toast")
	}
}

func TestRootScenarioRunSpecPickStartsRunOnChosenSpec(t *testing.T) {
	f := newScenSpecGateFix(t, false)
	col, started := f.wireRunLegs()
	f.r.pump(pages.ScenarioRunMsg{ID: "Specless Run"})
	f.r.pump(widgets.FilePickedMsg{Path: f.chosen})

	if got := <-started; got != "Specless Run" {
		t.Fatalf("engine started %q, want the gated scenario", got)
	}
	m := f.r.m
	if got := m.app.Config().GetSpec(); got != f.chosen {
		t.Fatalf("cfg.GetSpec() = %q, want the picked spec under the run", got)
	}
	if m.pendingScenarioRun != "" {
		t.Errorf("the resumed run must consume the pending scenario, kept %q", m.pendingScenarioRun)
	}
	if m.filePick != nil {
		t.Error("the pick must close the browse")
	}
	f.drain(col)
	if r := f.r.m.scenarioRun; r == nil || !r.done || r.id != "Specless Run" {
		t.Fatalf("run = %+v, want the gated scenario done", r)
	}
	if body := f.r.m.View().Content; !strings.Contains(body, "1/1 passed") {
		t.Errorf("final frame lacks the run banner:\n%s", body)
	}
}

func TestRootScenarioRunEscDropsPendingWithoutRun(t *testing.T) {
	f := newScenSpecGateFix(t, false)
	_, started := f.wireRunLegs()
	f.r.pump(pages.ScenarioRunMsg{ID: "Specless Run"})
	f.r.pump(special(tea.KeyEscape))

	m := f.r.m
	if m.filePick != nil {
		t.Fatal("esc must close the spec browse")
	}
	if m.pendingScenarioRun != "" {
		t.Fatalf("esc must drop the pending scenario, kept %q", m.pendingScenarioRun)
	}
	if m.scenarioRun != nil {
		t.Fatal("esc must leave no run started")
	}
	if got := m.app.Config().GetSpec(); got != "" {
		t.Errorf("esc must not apply a spec either: cfg.GetSpec() = %q", got)
	}
	if m.toast == nil || m.toast.Len() != 1 {
		t.Fatal("the cancel must surface a visible one-line notice")
	}
	notice := strings.Join(m.toast.Lines(), "\n")
	if !strings.Contains(notice, "scenario not run") || !strings.Contains(notice, "pick a specification file first") {
		t.Fatalf("notice text = %q", notice)
	}
	select {
	case name := <-started:
		t.Fatalf("engine ran %q after the cancel", name)
	default:
	}

	// The next run re-arms the spec browse (closing it never wedges §F).
	f.r.pump(pages.ScenarioRunMsg{ID: "Specless Run"})
	if m = f.r.m; m.filePick == nil || m.filePickTarget != "scenario:runspec" {
		t.Fatal("a second run must re-open the spec browse")
	}
}

func TestRootScenarioRunAllDeclaredNeedsNoPrompt(t *testing.T) {
	f := newScenSpecGateFix(t, false)
	col, started := f.wireRunLegs()
	f.r.pump(pages.ScenarioRunMsg{ID: "Declared Run"})

	if got := <-started; got != "Declared Run" {
		t.Fatalf("engine started %q, want the declared scenario", got)
	}
	f.drain(col)
	m := f.r.m
	if m.filePick != nil {
		t.Error("every step declaring a spec must run with the global spec empty")
	}
	if m.toast != nil && m.toast.Len() != 0 {
		t.Error("the silent run must stay quiet")
	}
}

func TestRootScenarioRunWithExplicitSpecIsSilent(t *testing.T) {
	f := newScenSpecGateFix(t, true)
	col, started := f.wireRunLegs()
	f.r.pump(pages.ScenarioRunMsg{ID: "Specless Run"})

	if got := <-started; got != "Specless Run" {
		t.Fatalf("engine started %q, want the specless scenario under the explicit spec", got)
	}
	f.drain(col)
	if f.r.m.filePick != nil {
		t.Error("an explicit spec must run the specless scenario without a prompt")
	}
}

func TestRootScenarioPreviewGateHonestLineAndBrowseOnce(t *testing.T) {
	f := newScenSpecGateFix(t, false)
	f.r.pump(pages.ScenarioStepDetailMsg{StepIndex: 1, ScenarioID: "Specless Run"})

	m := f.r.m
	if m.scenarioDetail.wait {
		t.Fatal("the gated preview must not arm a compose load")
	}
	p := m.scenarioDetail.preview
	if p == nil || p.Note != "no specification selected" || p.Request != nil {
		t.Fatalf("preview = %+v, want the honest no-specification line", p)
	}
	if !m.scenarios.StepPreviewOpen() {
		t.Error("the honest line must be visible in the step-detail overlay")
	}
	if m.filePick == nil || m.filePickTarget != "scenario:runspec" {
		t.Fatal("the preview gate opens the spec browse")
	}
	if m.pendingScenarioPreviewID != "Specless Run" || m.pendingScenarioPreviewAt != 1 {
		t.Errorf("pending preview = %q/%d, want the gated step", m.pendingScenarioPreviewID, m.pendingScenarioPreviewAt)
	}

	browse := m.filePick
	f.r.pump(pages.ScenarioStepDetailMsg{StepIndex: 1, ScenarioID: "Specless Run"})
	if m = f.r.m; m.filePick != browse {
		t.Error("the spec browse opens once per preview opening, not per request")
	}
	if m.scenarioDetail.wait {
		t.Error("no compose load may be armed against the default")
	}
}

func TestRootScenarioPreviewGateEscKeepsLineAndKeys(t *testing.T) {
	f := newScenSpecGateFix(t, false)
	f.r.pump(ch('j')) // select "Specless Run" in the list pane
	f.r.pump(pages.ScenarioStepDetailMsg{StepIndex: 1, ScenarioID: "Specless Run"})
	f.r.pump(special(tea.KeyEscape))

	m := f.r.m
	if m.filePick != nil {
		t.Fatal("esc must close the spec browse")
	}
	if m.pendingScenarioPreviewID != "" {
		t.Errorf("esc must drop the pending preview, kept %q", m.pendingScenarioPreviewID)
	}
	if p := m.scenarioDetail.preview; p == nil || p.Note != "no specification selected" {
		t.Fatalf("the honest line must stay: %+v", p)
	}
	if !m.scenarios.StepPreviewOpen() {
		t.Error("the overlay keeps showing the honest line")
	}
	if m.scenarioRun != nil {
		t.Error("nothing may run")
	}
	if m.toast != nil && m.toast.Len() != 0 {
		t.Error("the honest line is the visible line: esc must not toast over it")
	}

	// Esc closes the overlay, Tab focuses STEPS, j moves the cursor.
	f.r.pump(special(tea.KeyEscape))
	if m = f.r.m; m.scenarios.StepPreviewOpen() {
		t.Fatal("esc must close the preview overlay")
	}
	f.r.pump(pages.PaneFocusMsg{})
	f.r.pump(ch('j'))
	if m = f.r.m; m.scenarios.StepCursor() != 1 {
		t.Errorf("the step cursor must stay usable, at %d", m.scenarios.StepCursor())
	}
}

func TestRootScenarioPreviewGateSpecPickComposes(t *testing.T) {
	f := newScenSpecGateFix(t, false)
	f.r.pump(pages.ScenarioStepDetailMsg{StepIndex: 1, ScenarioID: "Specless Run"})
	f.r.pump(widgets.FilePickedMsg{Path: f.chosen})

	m := f.r.m
	if got := m.app.Config().GetSpec(); got != f.chosen {
		t.Fatalf("cfg.GetSpec() = %q, want the chosen spec", got)
	}
	if m.pendingScenarioPreviewID != "" {
		t.Error("the pick must consume the pending preview")
	}
	if m.scenarioDetail.wait {
		t.Error("the re-armed load must have folded by now")
	}
	p := m.scenarioDetail.preview
	if p == nil || p.Request == nil || !p.Composed || p.Note != "" {
		t.Fatalf("preview = %+v, want the composed request under the chosen spec", p)
	}
	if m.errModal != nil {
		t.Errorf("the fixture template packs under the chosen spec, got: %s", m.errModal.body)
	}
	if !m.scenarios.StepPreviewOpen() {
		t.Error("the compose result lands in the re-armed overlay")
	}
	if body := m.View().Content; !strings.Contains(body, "request composed from template - not sent yet") {
		t.Errorf("the composed label must show:\n%s", body)
	}
}
