// root_analyze_use.go is the §J "use it now" leg (F12.4): once a scenario
// run has been WRITTEN, [l] offers the extract as the session's
// transactions file — through the very same gate every tx-file pick
// passes, so specless entries still chain the spec browse — and [g] opens
// the §G server start form with the extract pre-filled as the routes
// file. Both travel through existing apply seams; nothing here invents a
// new write or start path.
package tui

import (
	tea "charm.land/bubbletea/v2"

	"jiso/internal/tui/pages"
)

// analyzeScenarioWritten is the root mirror of the page's use-it-now gate:
// the scenario goal, the run done, and its items actually on disk. A
// write in flight is refused like everywhere else (one write, no queue).
func (m *RootModel) analyzeScenarioWritten() bool {
	return m.analyzeGoal == pages.AnalyzeGoalScenario &&
		m.analyzeStep == pages.StepRun &&
		m.analyzeStatus == pages.AnalyzeStatusDone &&
		m.analyzeFileWritten && !m.analyzeWriteWait &&
		m.analyzeOutput != nil && m.analyzeOutput.OutputFile != ""
}

// handleAnalyzeUseTxFile folds [l]: the extract travels the tx-file gate
// (a chained spec browse when its entries declare no spec and none is
// chosen), never a silent apply.
func (m *RootModel) handleAnalyzeUseTxFile() (tea.Model, tea.Cmd) {
	if !m.analyzeScenarioWritten() {
		return m, nil
	}

	return m.gateTxFilePick(m.analyzeOutput.OutputFile)
}

// handleAnalyzeUseServer folds [g]: open the §G start form and pre-fill
// its routes field with the extract — the operator still confirms the
// start, and the [f] browse stays available to change it.
func (m *RootModel) handleAnalyzeUseServer() (tea.Model, tea.Cmd) {
	if !m.analyzeScenarioWritten() {
		return m, nil
	}
	opened, cmd := m.openServerForm()
	if m.serverDlg == nil {
		return m, cmd
	}
	_, pickCmd := m.pickServerFormField(serverFieldRoutes, m.analyzeOutput.OutputFile)
	if cmd == nil {
		cmd = pickCmd
	}

	return opened, cmd
}
