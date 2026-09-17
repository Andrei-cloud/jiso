package tui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
)

// scenarioFixtureJSON: two transactions, two scenarios (one with an
// extract + validate pair — the §F sample trimmed).
const scenarioFixtureJSON = `[
 {"type":"transaction","name":"Purchase","description":"Purchase authorization","fields":{"0":"0200"}},
 {"type":"transaction","name":"Sign On","description":"Network Sign On","fields":{"0":"0800"}},
 {"type":"scenario","name":"E2E Purchase","description":"e2e","steps":[
   {"name":"Sign On Step","use_transaction_id":"Sign On","validate":[{"field":"39","expect":"00"}]},
   {"name":"Purchase Step","use_transaction_id":"Purchase","validate":[{"field":"39","expect":"00"}],"extract":{"AuthId":"38"}}]},
 {"type":"scenario","name":"Decline matrix","description":"declines","steps":[
   {"name":"Decline Step","use_transaction_id":"Purchase","validate":[{"field":"39","expect":"05"}]}]}
]`

// newScenarioApp builds a real app over the scenario fixture (the
// newTxFileApp singleton idiom; never run in parallel).
func newScenarioApp(t *testing.T) *app.App {
	t.Helper()

	txFile := t.TempDir() + "/pool.json"
	if err := os.WriteFile(txFile, []byte(scenarioFixtureJSON), 0o600); err != nil {
		t.Fatalf("write tx file: %v", err)
	}

	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")
	cfg.SetSpec("../../specs/spec.json")
	cfg.SetFile(txFile)

	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	time.Sleep(20 * time.Millisecond) // let app construction goroutines settle

	return a
}

// scenCollector drains the scenario sender channel the way program.Send
// would; tests re-enter the msgs through Update by hand.
type scenCollector struct{ msgs chan tea.Msg }

func (c *scenCollector) nextAll(t *testing.T) []tea.Msg {
	t.Helper()

	var out []tea.Msg
	for {
		select {
		case msg := <-c.msgs:
			out = append(out, msg)
		case <-time.After(2 * time.Second):
			t.Fatal("scenario goroutine went quiet before finishing")

			return nil
		}
		if len(out) > 0 {
			select {
			case msg := <-c.msgs:
				out = append(out, msg)
				continue
			case <-time.After(120 * time.Millisecond):
				return out
			}
		}
	}
}

// fakeScenarioEngine replays hook events synchronously, then reports.
func fakeScenarioEngine(events []transactions.StepProgress, report *transactions.TestReport, err error) scenarioEngine {
	return func(_ string, observe func(transactions.StepProgress)) (*transactions.TestReport, error) {
		if observe != nil {
			for _, ev := range events {
				observe(ev)
			}
		}

		return report, err
	}
}

func stepDone(index int, name string, res transactions.StepResult) transactions.StepProgress {
	return transactions.StepProgress{Index: index, StepName: name, Result: &res}
}

func stepStarted(index int, name string) transactions.StepProgress {
	return transactions.StepProgress{Index: index, StepName: name, Started: true}
}

func TestRootScenariosPageMounted(t *testing.T) {
	m := NewRootModel(nil)

	var found bool
	for _, p := range m.registry {
		if sc, ok := p.(*pages.Scenarios); ok {
			found = sc.ID() == "scenarios"
		}
	}
	if !found {
		t.Fatal("registry must hold the §F scenarios page")
	}
	if len(PageIDs) != 8 || len(PageLabels) != 8 {
		t.Fatalf("hotkey slots changed: %d/%d", len(PageIDs), len(PageLabels))
	}

	_, cmd := m.Update(palette.GoToPageMsg{ID: "scenarios"})
	if cmd != nil {
		t.Fatalf("jump ran cmd %v", cmd)
	}
	wantStack(t, m, "scenarios")

	// Wireframe order: hotkey 3 lands on the §F scenarios page; the
	// §C inspector is a drill-down entered from transactions and sits in
	// the registry after the eight hotkey slots.
	m2 := NewRootModel(nil)
	_, _ = m2.Update(ch('3'))
	wantStack(t, m2, "scenarios")
	if _, ok := m2.registry[2].(*pages.Scenarios); !ok {
		t.Fatalf("slot 3 = %T, want the scenarios page", m2.registry[2])
	}
	if _, ok := m2.registry[8].(*pages.Inspector); !ok {
		t.Fatalf("registry[8] = %T, want the drill-down inspector", m2.registry[8])
	}
}

func TestRootScenariosListAndStepsFromApp(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))
	_, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = m.Update(palette.GoToPageMsg{ID: "scenarios"})

	// List order is ListScenarios' sort order: "Decline matrix" first.
	content := m.View().Content
	for _, want := range []string{"SCENARIOS (2)", "E2E Purchase", "Decline matrix", "Decline Step", "0200"} {
		if !strings.Contains(content, want) {
			t.Errorf("frame lacks %q:\n%s", want, content)
		}
	}

	// Steps follow the cursor: move to the second scenario.
	_, _ = m.Update(special(tea.KeyDown))
	content = m.View().Content
	if !strings.Contains(content, "Sign On Step") || !strings.Contains(content, "0800") {
		t.Errorf("steps pane must follow the selection:\n%s", content)
	}
	if strings.Contains(content, "Decline Step") {
		t.Errorf("old selection leaked:\n%s", content)
	}
}

func TestRootScenarioRunStreamOrder(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))
	col := &scenCollector{msgs: make(chan tea.Msg, 32)}
	m.SetScenarioSender(func(msg tea.Msg) { col.msgs <- msg })

	report := &transactions.TestReport{
		ScenarioName: "E2E Purchase",
		DurationMs:   3,
		Steps: []transactions.StepResult{
			{StepName: "Sign On Step", Success: true, LatencyMs: 1},
			{
				StepName: "Purchase Step", Success: false, LatencyMs: 2,
				ValidationErrors: []transactions.ValidationError{
					{Field: "39", Expected: "00", Actual: "96"},
				},
			},
		},
	}
	m.runScenario = fakeScenarioEngine([]transactions.StepProgress{
		stepStarted(1, "Sign On Step"),
		stepDone(1, "Sign On Step", report.Steps[0]),
		stepStarted(2, "Purchase Step"),
		stepDone(2, "Purchase Step", report.Steps[1]),
	}, report, nil)

	_, _ = m.Update(pages.ScenarioRunMsg{ID: "E2E Purchase"})
	wantStack(t, m, "dashboard", "scenarios")

	for _, msg := range col.nextAll(t) {
		_, _ = m.Update(msg)
	}

	content := m.View().Content
	for _, want := range []string{`expect "00" got "96"`, "1/2 passed", "validate 39=00"} {
		if !strings.Contains(content, want) {
			t.Errorf("final frame lacks %q:\n%s", want, content)
		}
	}
	if !strings.Contains(content, "report: scenario-report.json") {
		t.Errorf("header must show the report path:\n%s", content)
	}
}

// TestRootScenarioStepStreamOrder replays the fake engine's msgs one at
// a time: pending → running → pass / fail in exact stream order (the
// ⏳ intermediate is pinned at Update-state level; the ⏳-rendering
// itself is pages-level TestScenariosRunningStreamRenders).
func TestRootScenarioStepStreamOrder(t *testing.T) {
	steps := []struct {
		msgIdx  int
		stepIdx int
		want    pages.StepStatus
	}{
		{0, 0, pages.StepRunning},
		{1, 0, pages.StepPass},
		{2, 1, pages.StepRunning},
		{3, 1, pages.StepFail},
	}

	for _, c := range steps {
		m := NewRootModel(newScenarioApp(t))
		col := &scenCollector{msgs: make(chan tea.Msg, 32)}
		m.SetScenarioSender(func(msg tea.Msg) { col.msgs <- msg })

		report := &transactions.TestReport{
			ScenarioName: "E2E Purchase",
			DurationMs:   3,
			Steps: []transactions.StepResult{
				{StepName: "Sign On Step", Success: true, LatencyMs: 1},
				{
					StepName: "Purchase Step", Success: false, LatencyMs: 2,
					ValidationErrors: []transactions.ValidationError{
						{Field: "39", Expected: "00", Actual: "96"},
					},
				},
			},
		}
		m.runScenario = fakeScenarioEngine([]transactions.StepProgress{
			stepStarted(1, "Sign On Step"),
			stepDone(1, "Sign On Step", report.Steps[0]),
			stepStarted(2, "Purchase Step"),
			stepDone(2, "Purchase Step", report.Steps[1]),
		}, report, nil)

		_, _ = m.Update(pages.ScenarioRunMsg{ID: "E2E Purchase"})
		msgs := col.nextAll(t)
		if c.msgIdx >= len(msgs) {
			t.Fatalf("only %d msgs streamed", len(msgs))
		}
		for _, msg := range msgs[:c.msgIdx+1] {
			_, _ = m.Update(msg)
		}
		if got := m.scenarioRun.steps[c.stepIdx].Status; got != c.want {
			t.Fatalf("after msg %d: step %d status = %v, want %v", c.msgIdx, c.stepIdx, got, c.want)
		}
	}
}

func TestRootScenarioRunWithoutHookBackfills(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))
	col := &scenCollector{msgs: make(chan tea.Msg, 8)}
	m.SetScenarioSender(func(msg tea.Msg) { col.msgs <- msg })

	report := &transactions.TestReport{
		ScenarioName: "E2E Purchase", DurationMs: 4, Success: true,
		Steps: []transactions.StepResult{
			{StepName: "Sign On Step", Success: true, LatencyMs: 2},
			{StepName: "Purchase Step", Success: true, LatencyMs: 2},
		},
	}
	m.runScenario = fakeScenarioEngine(nil, report, nil) // hook never fires

	_, _ = m.Update(pages.ScenarioRunMsg{ID: "E2E Purchase"})
	for _, msg := range col.nextAll(t) {
		_, _ = m.Update(msg)
	}

	for i, want := range []pages.StepStatus{pages.StepPass, pages.StepPass} {
		if m.scenarioRun.steps[i].Status != want {
			t.Fatalf("step %d = %v, want %v (done backfill)", i, m.scenarioRun.steps[i].Status, want)
		}
	}
	// Glyph-neutral assertions (the process default theme is
	// environment-dependent; separator degradation is page-level and
	// pinned by the pages goldens).
	content := m.View().Content
	if !strings.Contains(content, "2/2 passed") || !strings.Contains(content, "4ms total") {
		t.Errorf("banner missing:\n%s", content)
	}
}

func TestRootScenarioRunNoAppAndInFlight(t *testing.T) {
	m := NewRootModel(nil)
	_, cmd := m.Update(pages.ScenarioRunMsg{ID: "x"})
	if cmd != nil {
		t.Fatalf("run without app ran cmd %v", cmd)
	}
	if m.scenarioRun != nil {
		t.Fatal("run without app must not start state")
	}

	// The seam is wired (msgs dropped into a void is fine): only then is
	// a run genuinely in-flight — without a sender the run closes
	// terminally instead (TestScenarioRunNilSenderTerminal, E5-A5-7).
	m = NewRootModel(newScenarioApp(t))
	m.SetScenarioSender(func(tea.Msg) {})
	started := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	m.runScenario = func(string, func(transactions.StepProgress)) (*transactions.TestReport, error) {
		close(started)
		<-release

		return &transactions.TestReport{ScenarioName: "E2E Purchase"}, nil
	}
	_, _ = m.Update(pages.ScenarioRunMsg{ID: "E2E Purchase"})
	<-started

	first := m.scenarioRun
	_, _ = m.Update(pages.ScenarioRunMsg{ID: "Decline matrix"})
	if m.scenarioRun != first {
		t.Fatal("a run while one is in flight must be ignored (no queue, no retry)")
	}
}

func TestRootScenarioExport(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))

	// Chdir after app construction (the fixture's spec path is relative
	// to the package dir): the export resolves its default path against
	// the cwd at write time, exactly like the CLI --report file does.
	dir := t.TempDir()
	t.Chdir(dir)

	_, _ = m.Update(palette.GoToPageMsg{ID: "scenarios"}) // render the §F page

	// No report yet → honest status line, no cmd, no file.
	_, cmd := m.Update(pages.ScenarioExportMsg{})
	if cmd != nil {
		t.Fatalf("export without a report ran cmd %v", cmd)
	}
	if m.scenarioStatusLine != "no report yet" {
		t.Fatalf("status line = %q, want %q", m.scenarioStatusLine, "no report yet")
	}
	if _, err := os.Stat(dir + "/scenario-report.json"); err == nil {
		t.Fatal("no file may be written")
	}

	// Completed run → the cmd writes the CLI-shaped JSON.
	report := &transactions.TestReport{
		ScenarioName: "E2E Purchase", Success: true,
		Steps: []transactions.StepResult{{StepName: "Sign On Step", Success: true, LatencyMs: 1}},
	}
	m.scenarioLastReport = report

	_, cmd = m.Update(pages.ScenarioExportMsg{})
	if cmd == nil {
		t.Fatal("export with a report must yield a cmd")
	}
	statMsg, ok := cmd().(scenarioExportStatMsg)
	if !ok || statMsg.exists {
		t.Fatalf("export stat leg = %#v, want a fresh-target verdict", statMsg)
	}
	_, cmd = m.Update(statMsg) // fresh → straight to the write leg
	msg, ok := cmd().(scenarioExportedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("export cmd msg = %#v", msg)
	}
	_, _ = m.Update(msg)

	data, err := os.ReadFile(dir + "/scenario-report.json")
	if err != nil {
		t.Fatalf("report file: %v", err)
	}
	var back transactions.TestReport
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("report json: %v", err)
	}
	if back.ScenarioName != "E2E Purchase" {
		t.Fatalf("report scenario = %q", back.ScenarioName)
	}
	if m.scenarioStatusLine != "report → scenario-report.json" {
		t.Fatalf("status line = %q", m.scenarioStatusLine)
	}
}

func TestRootScenarioExportWriteError(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))

	// A plain file where the report directory must be → MkdirAll fails.
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(dir+"/blocked", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.scenarioLastReport = &transactions.TestReport{ScenarioName: "s"}
	m.scenarioExportPath = "blocked/report.json"

	_, cmd := m.Update(pages.ScenarioExportMsg{})
	statMsg := mustCmd[scenarioExportStatMsg](t, cmd) // stat fails (ENOTDIR) → fresh
	_, cmd = m.Update(statMsg)
	msg := mustCmd[scenarioExportedMsg](t, cmd)
	if msg.err == nil {
		t.Fatal("write into a non-directory must fail")
	}
	_, _ = m.Update(msg)
	if !strings.HasPrefix(m.scenarioStatusLine, "report failed:") {
		t.Fatalf("status line = %q", m.scenarioStatusLine)
	}
}

func TestRootScenarioPopConsistentWithOtherPages(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))

	// Depth 1: Esc unwinds home to the dashboard (the
	// stack never empties).
	_, _ = m.Update(palette.GoToPageMsg{ID: "scenarios"})
	_, _ = m.Update(pages.ScenarioPopMsg{})
	wantStack(t, m, "dashboard")

	// Depth 2 (Enter on a scenario pushed the page): Esc pops back.
	_, _ = m.Update(palette.GoToPageMsg{ID: "dashboard"})
	m.Push(m.scenarios)
	_, _ = m.Update(pages.ScenarioPopMsg{})
	wantStack(t, m, "dashboard")
}

func TestFormatScenarioSummaryShapes(t *testing.T) {
	v := &app.ScenarioReport{PassedSteps: 3, TotalSteps: 3, Duration: 7 * time.Millisecond}
	if got := formatScenarioSummary(v); got != "3/3 passed · 7ms total" {
		t.Fatalf("summary = %q", got)
	}
	if got := formatScenarioDuration(7100 * time.Microsecond); got != "7.1ms" {
		t.Fatalf("sub-second decimal = %q", got)
	}
	if got := formatScenarioDuration(1200 * time.Millisecond); got != "1.2s" {
		t.Fatalf("seconds = %q", got)
	}
	if got := formatScenarioDuration(0); got != "" {
		t.Fatalf("zero must render nothing, got %q", got)
	}
}

// --- regression tests --------------------------------------

// feedScenarioCmds delivers a Cmd's message and keeps following the
// resulting chain (confirm decision → write leg → export result).
func feedScenarioCmds(t *testing.T, m *RootModel, cmd tea.Cmd) {
	t.Helper()
	for depth := 0; cmd != nil && depth < 8; depth++ {
		msg := cmd()
		if msg == nil {
			return
		}
		_, cmd = m.Update(msg)
	}
}

// TestRootScenarioExportOverwriteConfirm: `e` over an EXISTING
// scenario-report.json must open the §N3 confirm (default No) instead
// of silently destroying the previous report; cancel keeps the old
// bytes, y proceeds.
func TestRootScenarioExportOverwriteConfirm(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))
	dir := t.TempDir()

	path := dir + "/scenario-report.json"
	old := `{"previous":true}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	m.scenarioLastReport = &transactions.TestReport{ScenarioName: "E2E Purchase", Success: true}
	m.scenarioExportPath = path

	_, cmd := m.Update(pages.ScenarioExportMsg{})
	statMsg, ok := cmd().(scenarioExportStatMsg)
	if !ok || !statMsg.exists {
		t.Fatalf("stat leg = %#v, want exists", statMsg)
	}
	_, cmd = m.Update(statMsg)
	if cmd != nil {
		t.Fatal("an existing target must not write before confirmation")
	}
	if m.scenarioConfirm == nil || !m.scenarioConfirm.Pending() {
		t.Fatalf("existing report must open the overwrite confirm:\n%s", m.View().Content)
	}

	_, cmd = m.Update(special(tea.KeyEnter)) // default No
	feedScenarioCmds(t, m, cmd)
	if m.scenarioConfirm != nil {
		t.Fatal("Enter must cancel the confirm")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != old {
		t.Fatalf("cancel rewrote the report: %q %v", data, err)
	}

	_, cmd = m.Update(pages.ScenarioExportMsg{})
	statMsg = mustCmd[scenarioExportStatMsg](t, cmd)
	_, _ = m.Update(statMsg)
	if m.scenarioConfirm == nil {
		t.Fatal("second e must re-ask")
	}
	_, cmd = m.Update(ch('y')) // explicit yes writes
	feedScenarioCmds(t, m, cmd)
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == old {
		t.Fatalf("y must write the report, file still %q", old)
	}
	var back transactions.TestReport
	if err := json.Unmarshal(data, &back); err != nil || back.ScenarioName != "E2E Purchase" {
		t.Fatalf("report json = %q err=%v", data, err)
	}
}

// TestRootScenarioExportRapidDoubleEOnce: two rapid `e` must not
// interleave legs (the old code armed two writes back to back); the
// second `e` while a leg is in flight is a silent no-op.
func TestRootScenarioExportRapidDoubleEOnce(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))
	dir := t.TempDir()

	m.scenarioLastReport = &transactions.TestReport{ScenarioName: "s", Success: true}
	m.scenarioExportPath = dir + "/scenario-report.json"

	_, cmd1 := m.Update(pages.ScenarioExportMsg{})
	if cmd1 == nil {
		t.Fatal("first e must yield the stat leg")
	}
	_, cmd2 := m.Update(pages.ScenarioExportMsg{})
	if cmd2 != nil {
		t.Fatal("second e during an in-flight leg must be ignored")
	}

	statMsg := mustCmd[scenarioExportStatMsg](t, cmd1)
	_, cmd3 := m.Update(statMsg)
	if cmd3 == nil {
		t.Fatal("fresh target must proceed to the write leg")
	}
	_, cmd4 := m.Update(pages.ScenarioExportMsg{})
	if cmd4 != nil {
		t.Fatal("e during the write leg must be ignored")
	}
	_, _ = m.Update(cmd3())
	if m.scenarioStatusLine == "" || !strings.HasPrefix(m.scenarioStatusLine, "report →") {
		t.Fatalf("status line = %q", m.scenarioStatusLine)
	}
}

// TestScenarioRunNilSenderTerminal: driving RootModel without
// SetScenarioSender must not wedge §F: the run closes synchronously
// with the no-sender failure (the engine leg never runs), and a second
// ScenarioRunMsg closes again instead of being ignored in-flight
// (E5-A5-7).
func TestScenarioRunNilSenderTerminal(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))
	engineRan := false
	m.runScenario = func(string, func(transactions.StepProgress)) (*transactions.TestReport, error) {
		engineRan = true

		return nil, nil
	}

	_, _ = m.Update(pages.ScenarioRunMsg{ID: "E2E Purchase"})
	if engineRan {
		t.Error("engine leg ran without a sender seam")
	}
	if m.scenarioRun == nil || !m.scenarioRun.done {
		t.Fatal("nil-sender run never reached done")
	}
	if !strings.Contains(m.scenarioRun.summary, "no sender wired") {
		t.Errorf("summary = %q, want the no-sender failure", m.scenarioRun.summary)
	}

	_, _ = m.Update(pages.ScenarioRunMsg{ID: "E2E Purchase"})
	if m.scenarioRun == nil || !m.scenarioRun.done {
		t.Error("second nil-sender run was ignored in-flight (wedge)")
	}
}
