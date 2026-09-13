// root_analyze_flows.go is the §J enumeration leg: asking the engine which flows
// a capture contains, and folding the operator's per-flow selections in. Like every
// analyze leg it runs off the UI thread and reports back with the analyzeSeq token,
// so a step jump or an abort makes the result stale instead of racing it.
package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/analyzer"
	app "jiso/internal/app"
	"jiso/internal/tui/pages"
)

// armAnalyzeEnum launches the flow enumeration leg when the run step is
// entered: status running, seq-tokened result. The enumeration never
// fabricates flows — an empty capture comes back as a typed error and
// lands as the run-step note.
func (m *RootModel) armAnalyzeEnum() tea.Cmd {
	m.analyzeFlows, m.analyzeSelected = nil, nil
	m.analyzeParsed, m.analyzeUnparsable = 0, 0
	m.analyzeUnparsableRows = nil
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
	m.analyzeEnumWait = true
	m.analyzeStatus = pages.AnalyzeStatusRunning
	seq := m.analyzeSeq
	header, spec, path := m.analyzeHeader, m.analyzeSpecPath, m.analyzeCapturePath

	return func() tea.Msg {
		enum, err := src.EnumerateFlows(context.Background(), path, header, spec)

		return analyzeEnumLoadedMsg{seq: seq, enum: enum, err: err}
	}
}

// applyAnalyzeEnum folds enumeration into the run step: a typed error
// becomes the step note (the façade names the path it refuses — spec
// or capture); success populates the flows block and marks the run
// stale so the next Enter re-runs the engine with the fresh flow set.
func (m *RootModel) applyAnalyzeEnum(msg analyzeEnumLoadedMsg) (tea.Model, tea.Cmd) {
	m.analyzeEnumWait = false
	if msg.seq != m.analyzeSeq || m.Current().ID() != pages.AnalyzePageID {
		return m, nil
	}
	if msg.err != nil {
		m.analyzeStatus = pages.AnalyzeStatusError
		m.analyzeNote = analyzeErrorText(msg.err)

		return m, nil
	}
	if msg.enum == nil {
		m.analyzeStatus = pages.AnalyzeStatusError
		m.analyzeNote = "analyze produced no flows"

		return m, nil
	}

	m.analyzeStatus = pages.AnalyzeStatusIdle
	m.analyzeFlows = msg.enum.Flows
	m.analyzeParsed, m.analyzeUnparsable = msg.enum.Parsed, msg.enum.Unparsable
	// The unparsable-message reviewer (UAT round 6): build its roster
	// once per enumeration and bump the id so the run step re-arms a
	// fresh viewer cursor over the new samples.
	m.analyzeUnparsableRows = analyzeUnparsableRows(msg.enum.Samples)
	if m.analyzeUnparsableRows != nil {
		m.analyzeUnparsableID++
	}
	// Seed the run set: requests (dst) selected, responses (src) not —
	// except a src-only port whose requests arrive as src (UAT round 7
	// Option A: the honest default matching what the engine analyses).
	m.analyzeSelected = m.analyzeDefaultFlows()
	m.analyzeRunStale = true

	return m, nil
}

// analyzeDefaultFlows is the fresh-enumeration run set: every request (dst)
// direction, plus a response (src) direction only where it is the sole half
// of a port (a server-side capture whose requests arrive as src). UAT round
// 7 Option A — responses are not analysed until the operator picks them.
func (m *RootModel) analyzeDefaultFlows() []app.FlowSelection {
	hasDst := make(map[int]bool, len(m.analyzeFlows))
	for _, f := range m.analyzeFlows {
		if f.Direction == analyzer.DirectionDst {
			hasDst[f.ServerPort] = true
		}
	}
	sels := make([]app.FlowSelection, 0, len(m.analyzeFlows))
	for _, f := range m.analyzeFlows {
		if f.Direction == analyzer.DirectionSrc && hasDst[f.ServerPort] {
			continue // responses wait for an explicit pick
		}
		sels = append(sels, app.FlowSelection{Port: f.ServerPort, Dir: f.Direction})
	}

	return sels
}

// analyzeAllFlows is every enumerated direction — the "a" (all) run set.
func (m *RootModel) analyzeAllFlows() []app.FlowSelection {
	sels := make([]app.FlowSelection, 0, len(m.analyzeFlows))
	for _, f := range m.analyzeFlows {
		sels = append(sels, app.FlowSelection{Port: f.ServerPort, Dir: f.Direction})
	}

	return sels
}

// handleAnalyzeFlowToggleMsg is space on the run step's flow cursor. For the
// transactions and mock-routes goals it flips exactly the cursor's
// (port, direction); the scenario goal toggles a whole port (both halves),
// since it correlates requests and responses together. Both mark the run
// stale. handleAnalyzeFlowToggleAllMsg is "a".
func (m *RootModel) handleAnalyzeFlowToggleMsg(msg pages.AnalyzeFlowToggleMsg) (tea.Model, tea.Cmd) {
	if m.analyzeRunWait {
		m.analyzeNote = analyzeInFlight

		return m, nil
	}
	if m.analyzeGoal == pages.AnalyzeGoalScenario {
		return m.toggleFlowPort(msg.Port)
	}

	return m.toggleFlowDir(msg.Port, msg.Dir)
}

// toggleFlowDir flips one (port, direction) in the run set.
func (m *RootModel) toggleFlowDir(port int, dir string) (tea.Model, tea.Cmd) {
	for i, s := range m.analyzeSelected {
		if s.Port == port && s.Dir == dir {
			m.analyzeSelected = append(m.analyzeSelected[:i], m.analyzeSelected[i+1:]...)
			m.analyzeRunStale = true

			return m, nil
		}
	}
	m.analyzeSelected = append(m.analyzeSelected, app.FlowSelection{Port: port, Dir: dir})
	m.analyzeRunStale = true

	return m, nil
}

// toggleFlowPort flips a whole port (every direction at it) — the scenario
// goal's correlated unit, where picking either half brings the other in.
func (m *RootModel) toggleFlowPort(port int) (tea.Model, tea.Cmd) {
	if m.selectedPort(port) {
		kept := make([]app.FlowSelection, 0, len(m.analyzeSelected))
		for _, s := range m.analyzeSelected {
			if s.Port != port {
				kept = append(kept, s)
			}
		}
		m.analyzeSelected = kept
	} else {
		for _, f := range m.analyzeFlows {
			if f.ServerPort == port && !m.selectedFlow(port, f.Direction) {
				m.analyzeSelected = append(m.analyzeSelected, app.FlowSelection{Port: port, Dir: f.Direction})
			}
		}
	}
	m.analyzeRunStale = true

	return m, nil
}

// handleAnalyzeFlowToggleAllMsg is "a": include every direction, or clear
// them all when they already are included.
func (m *RootModel) handleAnalyzeFlowToggleAllMsg(pages.AnalyzeFlowToggleAllMsg) (tea.Model, tea.Cmd) {
	if m.analyzeRunWait {
		m.analyzeNote = analyzeInFlight

		return m, nil
	}
	all := true
	for _, f := range m.analyzeFlows {
		if !m.selectedFlow(f.ServerPort, f.Direction) {
			all = false

			break
		}
	}
	if all {
		m.analyzeSelected = nil
	} else {
		m.analyzeSelected = m.analyzeAllFlows()
	}
	m.analyzeRunStale = true

	return m, nil
}
