// root_scenario_export.go owns the §F `e` export: the page
// yields ScenarioExportMsg, and root writes the LAST completed report as
// JSON in a tea.Cmd (Update never blocks on I/O), folding the result
// back as scenarioExportedMsg. With no completed report the status line
// says "no report yet" instead of silently succeeding (no toast widget
// until footer/banner line).
//
// The write is gated like the §K export — `e` first runs an
// os.Stat leg; an existing scenario-report.json opens the §N3 overwrite
// confirm (default No) instead of silently destroying the previous
// report, and scenarioWriteWait is the single-flight so two rapid `e`
// cannot interleave a stat and a write.
package tui

import (
	"encoding/json"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/transactions"
	"jiso/internal/tui/widgets"
)

type (
	// scenarioExportStatMsg is the overwrite-stat verdict for `e`.
	scenarioExportStatMsg struct {
		path   string
		exists bool
	}
	// scenarioExportedMsg is the write result routed back into Update.
	scenarioExportedMsg struct {
		path string
		err  error
	}
)

// scenarioStat resolves the injectable os.Stat leg (nil = os.Stat; the
// §K ctfStatFn idiom).
func (m *RootModel) scenarioStat() func(string) (os.FileInfo, error) {
	if m.scenarioStatFn != nil {
		return m.scenarioStatFn
	}

	return os.Stat
}

// exportScenarioReport interprets `e`. No completed report → the honest
// status line and no cmd (nothing was written, nothing pretends to). A
// stat or write already in flight is ignored (single-flight: two rapid
// `e` must not interleave). Otherwise the stat leg runs as a cmd and
// routes to the §N3 overwrite confirm or straight to the write.
func (m *RootModel) exportScenarioReport() (tea.Model, tea.Cmd) {
	report := m.scenarioLastReport
	if report == nil {
		m.scenarioStatusLine = "no report yet"
		m.debug.logf("scenario export (no report)")

		return m, nil
	}
	if m.scenarioWriteWait || m.scenarioConfirm != nil {
		m.debug.logf("scenario export ignored in-flight")

		return m, nil
	}

	path := m.scenarioReportPath()
	m.debug.logf("scenario export stat path=%s", path)
	m.scenarioWriteWait = true
	stat := m.scenarioStat()

	return m, func() tea.Msg {
		_, serr := stat(path)

		return scenarioExportStatMsg{path: path, exists: serr == nil}
	}
}

// applyScenarioExportStat routes the stat verdict: an existing file
// opens the §N3 overwrite confirm (default No — the previous report
// survives); a fresh path proceeds to the write.
func (m *RootModel) applyScenarioExportStat(msg scenarioExportStatMsg) (tea.Model, tea.Cmd) {
	m.scenarioWriteWait = false
	if m.scenarioConfirm != nil || m.scenarioLastReport == nil {
		return m, nil // straggler after a decision or a reset
	}
	if msg.exists {
		m.scenarioExportPending = msg.path
		m.scenarioConfirm = widgets.NewConfirmDialog(m.themeOrNil(), "overwrite "+msg.path+"?")

		return m, nil
	}

	return m.armScenarioExport(msg.path)
}

// armScenarioExport launches the write cmd: MkdirAll of the destination
// dir, MarshalIndent of the transactions.TestReport (the exact JSON the
// CLI --report file carries), 0o644.
func (m *RootModel) armScenarioExport(path string) (tea.Model, tea.Cmd) {
	report := m.scenarioLastReport
	if report == nil {
		return m, nil
	}
	m.scenarioWriteWait = true
	m.debug.logf("scenario export write path=%s", path)

	return m, func() tea.Msg {
		return scenarioExportedMsg{path: path, err: writeScenarioReport(path, report)}
	}
}

// applyScenarioExportConfirmed proceeds with the write of the exact
// pending path (never recovered from the question text — a trailing
// "?" would mangle it; the lesson).
func (m *RootModel) applyScenarioExportConfirmed() (tea.Model, tea.Cmd) {
	path := m.scenarioExportPending
	m.scenarioConfirm = nil
	m.scenarioExportPending = ""

	return m.armScenarioExport(path)
}

// applyScenarioExportCancelled closes the confirm; nothing is written.
func (m *RootModel) applyScenarioExportCancelled() (tea.Model, tea.Cmd) {
	m.scenarioConfirm = nil
	m.scenarioExportPending = ""
	m.scenarioWriteWait = false

	return m, nil
}

// writeScenarioReport is the export's file side (cmd territory, never
// Update). Same bytes as the CLI --report file for the same report.
func writeScenarioReport(path string, report *transactions.TestReport) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

// applyScenarioExported lands the write result on the status line:
// `report → scenario-report.json` on success, the error otherwise.
func (m *RootModel) applyScenarioExported(msg scenarioExportedMsg) (tea.Model, tea.Cmd) {
	m.scenarioWriteWait = false
	if msg.err != nil {
		m.scenarioStatusLine = "report failed: " + msg.err.Error()
	} else {
		m.scenarioStatusLine = "report → " + msg.path
	}

	return m, nil
}
