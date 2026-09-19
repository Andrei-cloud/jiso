// root_analyze_use_test.go pins the F12.4 "use it now" root legs: after a
// scenario write, [l] offers the extract through the tx-file spec gate
// (chained spec browse when entries declare no spec and none is chosen),
// and [g] opens the §G start form pre-filled with the extract as the
// routes file. Before the write lands both messages are inert.
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	app "jiso/internal/app"
	"jiso/internal/config"
	"jiso/internal/tui/pages"
)

// scenarioRunOut builds a realistic scenario-goal output: transaction +
// its dataset + the scenario item + one mock route (the complete roster
// the F12 write confirm accepts without asking; the fake's default roster
// is a tx-mode one and a scenario run never ships without its scenario
// item).
func scenarioRunOut(extract string) *app.AnalyzeOutput {
	out := &app.AnalyzeOutput{Mode: "scenario", ScenarioName: "Captured Run"}
	out.AttachGeneratedItems([]config.Item{
		{Type: config.TypeTransaction, Name: "Captured 0200", DatasetName: "dataset_captured"},
		{Type: config.TypeDataset, Name: "dataset_captured"},
		{Type: config.TypeMockRoute, Name: "Captured Route", ResponseMTI: "0210"},
		{Type: config.TypeScenario, Name: "Captured Run"},
	})
	out.OutputFile = extract

	return out
}

// scenarioWriteFixture: the fake's run output is a scenario whose output
// file exists on disk as a specless transaction + scenario (the shape a
// wizard-written extract has when analyzed without a spec path).
func scenarioWriteFixture(t *testing.T) (*analyzeTestRoot, *fakeAnalyze, string) {
	t.Helper()

	fake := fakeAnalyzeFixture()

	extract := filepath.Join(t.TempDir(), "captured-extract.json")
	body := `[{"type":"transaction","name":"Captured 0200","description":"d","fields":{"0":"0200"}},` +
		`{"type":"scenario","name":"Captured Run","description":"d","steps":[{"name":"s1","use_transaction_id":"Captured 0200"}]},` +
		`{"type":"mock_route","name":"Captured Route","description":"d","response_mti":"0210","match_fields":{"0":"0200"}}]`
	if err := os.WriteFile(extract, []byte(body), 0o644); err != nil {
		t.Fatalf("extract: %v", err)
	}
	fake.runOut = scenarioRunOut(extract)

	r := newAnalyzeTestRoot(t, fake)
	r.walkToRun(t)
	r.pump(pages.AnalyzeChooseGoalMsg{Goal: pages.AnalyzeGoalScenario})
	r.enter() // run: the item picker auto-presents
	r.closePicker()
	r.pump(ch('w')) // write: the stat default is "absent", straight to the leg
	if fake.writeN != 1 {
		t.Fatalf("write legs = %d, want 1", fake.writeN)
	}

	return r, fake, extract
}

func TestAnalyzeUseTxFileGatesLikeEveryTxPick(t *testing.T) {
	r, _, extract := scenarioWriteFixture(t)

	r.pump(pages.AnalyzeUseTxFileMsg{})

	// No spec is chosen and the extract's transaction declares none: the
	// chained spec browse must open seated on the extract's dir, never a
	// silent default-spec apply.
	if r.m.filePick == nil {
		t.Fatalf("the tx-file gate did not open a browse (note %q)", r.m.analyzeNote)
	}
	if r.m.filePickTarget != settingsSpecForFileTarget {
		t.Errorf("browse target = %q, want the chained spec target", r.m.filePickTarget)
	}
	if r.m.pendingTxFile != extract {
		t.Errorf("pending tx file = %q, want %q", r.m.pendingTxFile, extract)
	}
	if got := r.m.filePick.CurrentDir(); got != filepath.Dir(extract) {
		t.Errorf("browse dir = %q, want the extract's dir %q", got, filepath.Dir(extract))
	}
}

func TestAnalyzeUseServerOpensFormWithRoutesPreFilled(t *testing.T) {
	r, _, extract := scenarioWriteFixture(t)

	r.pump(pages.AnalyzeUseServerMsg{})

	if r.m.serverDlg == nil {
		t.Fatal("the §G start form did not open")
	}
	st := r.m.serverDlg.State()
	f := st.Field(serverFieldRoutes)
	if f == nil {
		t.Fatal("the form has no routes field")
	}
	if f.Value != extract {
		t.Errorf("routes field = %q, want the extract path %q", f.Value, extract)
	}
}

func TestAnalyzeUseKeysInertBeforeWrite(t *testing.T) {
	fake := fakeAnalyzeFixture()
	fake.runOut.Mode = app.AnalyzeModeScenario
	r := newAnalyzeTestRoot(t, fake)
	r.walkToRun(t)
	r.pump(pages.AnalyzeChooseGoalMsg{Goal: pages.AnalyzeGoalScenario})
	r.enter()
	r.closePicker()

	r.pump(pages.AnalyzeUseTxFileMsg{})
	if r.m.filePick != nil || r.m.pendingTxFile != "" {
		t.Error("[l] before the write must not open the tx-file gate")
	}
	r.pump(pages.AnalyzeUseServerMsg{})
	if r.m.serverDlg != nil {
		t.Error("[g] before the write must not open the server form")
	}
}

// scenarioExtractRoot: a scenario-goal run whose picker auto-presents a
// four-row roster (transaction, dataset, route, scenario).
func scenarioExtractRoot(t *testing.T) (*analyzeTestRoot, *fakeAnalyze, string) {
	t.Helper()

	fake := fakeAnalyzeFixture()

	extract := filepath.Join(t.TempDir(), "captured-extract.json")
	if err := os.WriteFile(extract, []byte("[]"), 0o644); err != nil {
		t.Fatalf("seed extract: %v", err)
	}
	fake.runOut = scenarioRunOut(extract)

	r := newAnalyzeTestRoot(t, fake)
	r.walkToRun(t)
	r.pump(pages.AnalyzeChooseGoalMsg{Goal: pages.AnalyzeGoalScenario})
	r.enter() // run: picker auto-presents

	return r, fake, extract
}

// TestAnalyzeWriteIncompleteConfirmsThenWrites: the picker is free to
// deselect anything, and a deliberate subset must LAND (UAT finding: w
// did nothing). With the scenario item dropped, [w] opens the F12
// confirm naming the gap - never a silent refusal - and yes writes the
// selection as-is.
func TestAnalyzeWriteIncompleteConfirmsThenWrites(t *testing.T) {
	r, fake, _ := scenarioExtractRoot(t)

	// Roster rows: 0 transaction, 1 dataset, 2 route, 3 scenario.
	// Deselect the scenario row and apply.
	r.pump(ch('j'))
	r.pump(ch('j'))
	r.pump(ch('j'))
	r.pump(ch(' '))
	r.pump(tea.KeyPressMsg{Code: tea.KeyEnter})
	r.pump(ch('w'))

	if fake.writeN != 0 {
		t.Fatalf("an incomplete extract wrote before the confirm (%d legs)", fake.writeN)
	}
	c := r.m.analyzeOverwriteConfirm
	if c == nil || !c.Pending() {
		t.Fatalf("w with a dropped scenario item must confirm, not refuse:\n%s", r.view())
	}
	if !strings.Contains(c.Question(), "no scenario item") {
		t.Errorf("confirm question = %q, want it to name the missing scenario item", c.Question())
	}
	// The box owns its decision keys in-body (module-window hotkeys).
	if view := r.view(); !strings.Contains(view, "y confirm") || !strings.Contains(view, "esc cancel") {
		t.Errorf("the confirm box must carry its keys:\n%s", view)
	}

	r.pump(ch('y'))
	if fake.writeN != 1 {
		t.Fatalf("after y: writeN = %d, want the deliberate subset written", fake.writeN)
	}
	if r.m.analyzeIntegrityAsk != "" {
		t.Errorf("a confirmed write must clear the integrity ask, got %q", r.m.analyzeIntegrityAsk)
	}
}

// TestAnalyzeWriteIncompleteRoutesConfirm: same honesty - an extract
// whose selection has a scenario but no mock routes confirms, and yes
// writes.
func TestAnalyzeWriteIncompleteRoutesConfirm(t *testing.T) {
	r, fake, _ := scenarioExtractRoot(t)

	// Deselect the route row (index 2) and apply.
	r.pump(ch('j'))
	r.pump(ch('j'))
	r.pump(ch(' '))
	r.pump(tea.KeyPressMsg{Code: tea.KeyEnter})
	r.pump(ch('w'))

	if fake.writeN != 0 {
		t.Fatalf("an incomplete extract wrote before the confirm (%d legs)", fake.writeN)
	}
	c := r.m.analyzeOverwriteConfirm
	if c == nil || !strings.Contains(c.Question(), "no mock routes") {
		t.Fatalf("w with the route row dropped must confirm naming the gap, got %v", c)
	}

	r.pump(ch('y'))
	if fake.writeN != 1 {
		t.Fatalf("after y: writeN = %d, want the deliberate subset written", fake.writeN)
	}
}

// TestAnalyzeWriteIncompleteCancelNamesGap: no at the confirm writes
// nothing, and the note explains the gap WITH THE KEYS THAT FIX IT on
// the run step - the done status renders notes (invisible-feedback
// regression: the old silent refusal landed where nothing showed it).
func TestAnalyzeWriteIncompleteCancelNamesGap(t *testing.T) {
	r, fake, _ := scenarioExtractRoot(t)

	r.pump(ch('j'))
	r.pump(ch('j'))
	r.pump(ch('j'))
	r.pump(ch(' '))
	r.pump(tea.KeyPressMsg{Code: tea.KeyEnter})
	r.pump(ch('w'))
	r.pump(ch('n')) // explicit No — nothing is written

	if fake.writeN != 0 {
		t.Fatalf("cancel must write nothing, got %d legs", fake.writeN)
	}
	if r.m.analyzeOverwriteConfirm != nil {
		t.Fatal("cancel must close the confirm")
	}
	if !strings.Contains(r.m.analyzeNote, "no scenario item") {
		t.Errorf("cancel note = %q, want it to name the missing scenario item", r.m.analyzeNote)
	}
	if view := r.view(); !strings.Contains(view, "no scenario item") {
		t.Errorf("the run step must render the cancel note:\n%s", view)
	}
}

// TestAnalyzeWriteCancelThenConfirmSuccessStandsAlone: the note from a
// cancel must not survive into a later successful write - UAT found the
// "⚠ no scenario item" note sitting under "✓ wrote 1 item(s)", reading
// like a warning about the fresh success.
func TestAnalyzeWriteCancelThenConfirmSuccessStandsAlone(t *testing.T) {
	r, fake, _ := scenarioExtractRoot(t)

	r.pump(ch('j'))
	r.pump(ch('j'))
	r.pump(ch('j'))
	r.pump(ch(' '))
	r.pump(tea.KeyPressMsg{Code: tea.KeyEnter})
	r.pump(ch('w'))
	r.pump(ch('n')) // cancel first: the note is set
	if !strings.Contains(r.m.analyzeNote, "no scenario item") {
		t.Fatalf("cancel must leave the note, got %q", r.m.analyzeNote)
	}

	r.pump(ch('w')) // write again, deliberately
	if r.m.analyzeNote != "" {
		t.Errorf("a fresh write intent must clear the stale note, got %q", r.m.analyzeNote)
	}
	if r.m.analyzeOverwriteConfirm == nil {
		t.Fatal("the incomplete selection must confirm again")
	}
	r.pump(ch('y'))
	if fake.writeN != 1 {
		t.Fatalf("after y: writeN = %d, want 1", fake.writeN)
	}
	view := r.view()
	if !strings.Contains(view, "wrote") {
		t.Fatalf("the success line must show:\n%s", view)
	}
	if strings.Contains(view, "no scenario item") {
		t.Errorf("the success must stand alone, no stale warning:\n%s", view)
	}
}

// TestAnalyzeWriteFullSelectionStillWrites: the confirm must not stand
// in the way of the default complete selection - no box, straight leg.
func TestAnalyzeWriteFullSelectionStillWrites(t *testing.T) {
	r, fake, _ := scenarioExtractRoot(t)

	r.closePicker() // Esc applies the all-included selection
	r.pump(ch('w'))

	if fake.writeN != 1 {
		t.Fatalf("write legs = %d, want 1 for the complete scenario selection", fake.writeN)
	}
	if r.m.analyzeOverwriteConfirm != nil {
		t.Error("a complete extract must not be asked the integrity question")
	}
}
