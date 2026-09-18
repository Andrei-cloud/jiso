// root_scenario_export_test.go is the §F `e` export leg's test file:
// the honest no-report refusal (and its error screen), the write result,
// the §N3 overwrite confirm, and single-flight across rapid presses
// (the §F page and run harness — newScenarioApp, mustCmd — live in
// root_scenarios_test.go, same package).
package tui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/transactions"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/palette"
)

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

// `e` with nothing exported yet is an explicit failed action: the honest
// refusal opens the error screen explaining that e exports after a run
// (the status line stays as the page's inline truth).
func TestRootScenarioExportNoReportOpensModal(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))
	_, _ = m.Update(palette.GoToPageMsg{ID: "scenarios"})

	_, cmd := m.Update(pages.ScenarioExportMsg{})
	if cmd != nil {
		t.Fatalf("no-report refusal armed %v, want no cmd", cmd)
	}
	if m.scenarioStatusLine != "no report yet" {
		t.Errorf("status line = %q, want the honest line", m.scenarioStatusLine)
	}
	if m.errModal == nil {
		t.Fatal("the refusal must open the error screen")
	}
	mustShow(t, m.View().Content, "no scenario report to export", "run a scenario")

	_, _ = m.Update(special(tea.KeyEscape))
	if m.errModal != nil {
		t.Fatal("esc must close the screen")
	}
}

// a failed report write opens the error screen with the OS error named
// (the inline status line stays).
func TestRootScenarioExportWriteErrorOpensModal(t *testing.T) {
	m := NewRootModel(newScenarioApp(t))

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

	if m.errModal == nil {
		t.Fatal("a failed export write must open the error screen")
	}
	mustShow(t, m.View().Content, "cannot export scenario report", "not a directory")
	if !strings.HasPrefix(m.scenarioStatusLine, "report failed:") {
		t.Errorf("status line = %q, want the inline failure line", m.scenarioStatusLine)
	}

	_, _ = m.Update(special(tea.KeyEscape))
	if m.errModal != nil {
		t.Fatal("esc must close the screen")
	}
}
