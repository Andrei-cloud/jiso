// root_analyze_run.go is the §J run leg: the goal/header/mask picks, arming
// the engine, and applying its result. The engine call happens in a
// tea.Cmd; the apply is pure state and drops results whose seq token is
// not the current one.
package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/analyzer"
	app "jiso/internal/app"
	"jiso/internal/tui/pages"
)

// handleAnalyzeRunMsg is Enter on the run step: start the analysis with the
// folded inline options. The "/" filter maps to the run's port set; a filter
// matching none is a note, never a fabricated selection.
func (m *RootModel) handleAnalyzeRunMsg(msg pages.AnalyzeRunMsg) (tea.Model, tea.Cmd) {
	if m.analyzeStep != pages.StepRun {
		return m, nil
	}
	if m.analyzeEnumWait || m.analyzeSpecWait || m.analyzeRunWait || m.analyzeWriteWait {
		m.analyzeNote = analyzeInFlight

		return m, nil
	}
	filter := strings.TrimSpace(msg.Filter)
	m.analyzeFlowFilter = filter

	rows := m.analyzeFlowRows()
	if len(rows) > 0 {
		sels, note := analyzeRunSelection(rows, filter)
		if sels == nil {
			m.analyzeNote = note

			return m, nil
		}
		m.analyzeSelected = sels
	}

	return m, m.armAnalyzeRun()
}

// analyzeRunSelection computes the run set: pending directions intersected
// with the filter-visible rows. An empty selection returns nil plus a note;
// Enter never resurrects excluded flows.
func analyzeRunSelection(rows []pages.AnalyzeFlowRow, filter string) ([]app.FlowSelection, string) {
	sels := make([]app.FlowSelection, 0, len(rows))
	for _, r := range rows {
		if r.Selectable && r.Selected && pages.FlowMatchesFilter(r, filter) {
			sels = append(sels, app.FlowSelection{Port: r.Port, Dir: r.Direction})
		}
	}

	if len(sels) > 0 {
		return sels, ""
	}

	if filter != "" {
		return nil, "no flows match the filter - clear it or fix it"
	}

	return nil, "no flows selected - space includes, a includes all"
}

// handleAnalyzeChooseGoal / Header / Mask fold the inline selections into
// the wizard truth; any change marks the run stale so results match picks.
func (m *RootModel) handleAnalyzeChooseGoal(msg pages.AnalyzeChooseGoalMsg) (tea.Model, tea.Cmd) {
	switch msg.Goal {
	case pages.AnalyzeGoalTransactions, pages.AnalyzeGoalMockRoutes, pages.AnalyzeGoalScenario:
		m.analyzeGoal = msg.Goal
		m.analyzeRunStale = true
	default:
		return m, nil
	}
	// The routes goal owns the matching step and the other two do not:
	// switching goals relocates the operator to the step that exists in
	// the new rail. Pressing r on the run step walks into the wizard
	// (scan armed); pressing t/s from the wizard lands on run. A write in
	// flight refuses the relocation (one write, no queue, no undo).
	switch {
	case m.analyzeWriteWait:
		m.analyzeNote = "write in flight - wait for it to finish"

		return m, nil
	case msg.Goal == pages.AnalyzeGoalMockRoutes && m.analyzeStep == pages.StepRun:
		return m, m.setAnalyzeStep(pages.StepMatching)
	case msg.Goal != pages.AnalyzeGoalMockRoutes && m.analyzeStep == pages.StepMatching:
		return m, m.setAnalyzeStep(pages.StepRun)
	}

	return m, nil
}

func (m *RootModel) handleAnalyzeChooseHeader(msg pages.AnalyzeChooseHeaderMsg) (tea.Model, tea.Cmd) {
	if msg.Header == "" {
		return m, nil
	}
	m.analyzeHeader = msg.Header
	m.analyzeRunStale = true

	return m, nil
}

func (m *RootModel) handleAnalyzeChooseMask(msg pages.AnalyzeChooseMaskMsg) (tea.Model, tea.Cmd) {
	m.analyzeMaskRaw = msg.Raw
	m.analyzeRunStale = true

	return m, nil
}

// armAnalyzeRun launches the engine leg when Enter on the run step
// starts the analysis with stale selections (or never run): status
// running, seq-tokened result.
func (m *RootModel) armAnalyzeRun() tea.Cmd {
	m.analyzeWriteLine, m.analyzeWriteOK, m.analyzeFileWritten = "", false, false
	src := m.analyzeSource()
	if src == nil {
		m.analyzeStatus = pages.AnalyzeStatusError
		m.analyzeNote = analyzeNoEngine

		return nil
	}
	if m.analyzeCapturePath == "" {
		m.analyzeStatus = pages.AnalyzeStatusError
		m.analyzeNote = analyzeNeedCapture

		return nil
	}
	if !m.analyzeRunStale && m.analyzeOutput != nil {
		// Revisit with unchanged selections: the preview stands.
		m.analyzeStatus = pages.AnalyzeStatusDone

		return nil
	}

	// The routes goal runs through the matching wizard: the operator's
	// conditions ride the leg whole (no auto-inference alongside), and
	// zero conditions is refused — "match everything as one route" is
	// never the intent of an empty wizard.
	var match *analyzer.MatchSpec
	if m.analyzeGoal == pages.AnalyzeGoalMockRoutes {
		if len(m.analyzeConds) == 0 {
			m.analyzeNote = "matching: add at least one condition first (r walks the wizard)"

			return nil
		}
		ms := m.matchSpec()
		match = &ms
	}

	m.analyzeRunWait = true
	m.analyzeRunStale = false
	m.analyzeStatus = pages.AnalyzeStatusRunning
	m.analyzeOutput = nil
	m.analyzePreview = ""
	m.analyzeElapsed = ""
	m.analyzeRunStart = m.now()
	seq := m.analyzeSeq
	opts := app.AnalyzeRunOptions{
		PcapPath:   m.analyzeCapturePath,
		HeaderType: m.analyzeHeader,
		SpecPath:   m.analyzeSpecPath,
		Mode:       analyzeEngineMode(m.analyzeGoal),
		Unsecure:   m.analyzeMaskRaw,
		Flows:      append([]app.FlowSelection(nil), m.analyzeSelected...),
		OutputFile: m.analyzeOutputPath,
		Match:      match,
	}

	return func() tea.Msg {
		out, err := src.RunAnalyze(context.Background(), opts)

		return analyzeRunLoadedMsg{seq: seq, out: out, err: err}
	}
}

// applyAnalyzeRun folds the engine result: success attaches the run output
// and results preview; errors land as the run-step Note.
func (m *RootModel) applyAnalyzeRun(msg analyzeRunLoadedMsg) (tea.Model, tea.Cmd) {
	m.analyzeRunWait = false
	if msg.seq != m.analyzeSeq || m.Current().ID() != pages.AnalyzePageID {
		return m, nil
	}
	m.analyzeElapsed = elapsedCell(m.now().Sub(m.analyzeRunStart))
	if msg.err != nil {
		m.analyzeStatus = pages.AnalyzeStatusError
		m.analyzeNote = analyzeErrorText(msg.err)

		return m, nil
	}
	if msg.out == nil {
		// A nil output without an error is a broken leg, not a preview.
		m.analyzeStatus = pages.AnalyzeStatusError
		m.analyzeNote = "analyze produced no output"

		return m, nil
	}
	m.analyzeOutput = msg.out
	m.analyzeExcluded = nil
	m.analyzeItemsID++ // re-arms (opens) the generated-item picker
	m.analyzeItemRows = analyzeItemRows(msg.out)
	m.analyzePreview = analyzeRunSummary(msg.out)
	m.analyzeStatus = pages.AnalyzeStatusDone

	return m, nil
}
