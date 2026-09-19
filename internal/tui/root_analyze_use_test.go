// root_analyze_use_test.go pins the F12.4 "use it now" root legs: after a
// scenario write, [l] offers the extract through the tx-file spec gate
// (chained spec browse when entries declare no spec and none is chosen),
// and [g] opens the §G start form pre-filled with the extract as the
// routes file. Before the write lands both messages are inert.
package tui

import (
	"os"
	"path/filepath"
	"testing"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
)

// scenarioWriteFixture: the fake's run output is a scenario whose output
// file exists on disk as a specless transaction + scenario (the shape a
// wizard-written extract has when analyzed without a spec path).
func scenarioWriteFixture(t *testing.T) (*analyzeTestRoot, *fakeAnalyze, string) {
	t.Helper()

	fake := fakeAnalyzeFixture()
	fake.runOut.Mode = app.AnalyzeModeScenario
	fake.runOut.ScenarioName = "PCAP Captured Test Scenario"

	extract := filepath.Join(t.TempDir(), "captured-extract.json")
	body := `[{"type":"transaction","name":"Captured 0200","description":"d","fields":{"0":"0200"}},` +
		`{"type":"scenario","name":"Captured Run","description":"d","steps":[{"name":"s1","use_transaction_id":"Captured 0200"}]}]`
	if err := os.WriteFile(extract, []byte(body), 0o644); err != nil {
		t.Fatalf("extract: %v", err)
	}
	fake.runOut.OutputFile = extract

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
