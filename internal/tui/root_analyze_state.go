// root_analyze_state.go derives the §J page snapshot from root's wizard
// truth: the page receives display data only and never touches the app or
// the clock itself. syncAnalyze runs in the Update wrapper, so every folded
// message is reflected in the next View.
package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/utils"
)

// syncAnalyze pushes the current snapshot into the page. The wizard paths
// (capture/spec/header) start EMPTY: chosen by the operator, nothing
// inherited from config; unset values ride the run legs as "" (engine default).
func (m *RootModel) syncAnalyze() {
	if m.analyze == nil {
		return
	}
	if m.analyzeGoal == "" {
		m.analyzeGoal = pages.AnalyzeGoalTransactions
	}
	m.analyze.SetState(m.analyzeState())
}

// analyzeState builds the immutable snapshot the page renders; the
// candidate lists are walked only while the §J page is current.
func (m *RootModel) analyzeState() pages.AnalyzeState {
	st := pages.AnalyzeState{
		Step:         m.analyzeStep,
		Status:       m.analyzeStatus,
		Goal:         m.analyzeGoal,
		Goals:        m.analyzeGoalRadios(),
		SpecPath:     m.analyzeSpecPath,
		SpecError:    m.analyzeSpecError,
		Header:       m.analyzeHeader,
		Headers:      m.analyzeHeaderList(),
		CapturePath:  m.analyzeCapturePath,
		CaptureError: m.analyzeCaptureError,
		Flows:        m.analyzeFlowRows(),
		FlowFilter:   m.analyzeFlowFilter,
		Parsed:       m.analyzeParsed,
		Unparsable:   m.analyzeUnparsable,
		MaskRaw:      m.analyzeMaskRaw,
		Masking:      m.analyzeMaskRadios(),
		Elapsed:      m.analyzeElapsed,
		Preview:      m.analyzePreview,
		WriteLine:    m.analyzeWriteLine,
		WriteOK:      m.analyzeWriteOK,
		Note:         m.analyzeNote,
		OutputPath:   m.analyzeOutputDisplay(),

		Conds:         m.analyzeCondViews(),
		Variances:     m.analyzeGroupOptions(),
		MatchLine:     m.analyzeMatchLine,
		MatchWarn:     m.analyzeMatchWarn,
		MatchScanning: m.analyzeScanWait,
	}
	st.Items = m.analyzeItemsView()
	st.ItemsID = m.analyzeItemsID
	st.UnparsableRows = m.analyzeUnparsableRows
	st.UnparsableID = m.analyzeUnparsableID
	if m.Current().ID() == pages.AnalyzePageID {
		st.CaptureItems = m.analyzeCaptureItems()
		st.SpecItems = m.analyzeSpecItems()
	}

	return st
}

// analyzeOutputDisplay resolves the run step's effective output file: the
// [o] pick, else the path the last run bound, else the engine default for
// the current goal.
func (m *RootModel) analyzeOutputDisplay() string {
	if m.analyzeOutputPath != "" {
		return m.analyzeOutputPath
	}
	if m.analyzeOutput != nil {
		return m.analyzeOutput.OutputFile
	}
	if cfg := m.configOrNil(); cfg != nil {
		return app.AnalyzeOutputFile(cfg, analyzeEngineMode(m.analyzeGoal))
	}

	return ""
}

// analyzeCaptureItems lists the capture-step candidates: the session recents
// (newest first), the current pick, then the *.pcap files in the working
// directory.
func (m *RootModel) analyzeCaptureItems() []pages.WizardItem {
	seen := map[string]bool{}
	items := make([]pages.WizardItem, 0, 16)
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		items = append(items, pages.WizardItem{
			Label: filepath.Base(path), Path: path, Current: path == m.analyzeCapturePath,
		})
	}
	for _, r := range m.analyzeRecents {
		add(r)
	}
	add(m.analyzeCapturePath)
	for _, p := range analyzePcapDirItems(".") {
		add(p)
	}
	if len(items) > wizardTemplateLimit {
		items = items[:wizardTemplateLimit]
	}

	return items
}

// analyzeSpecItems lists the spec-step candidates: the current spec first,
// then the *.json files beside it, then the working directory's *.json.
func (m *RootModel) analyzeSpecItems() []pages.WizardItem {
	seen := map[string]bool{}
	items := make([]pages.WizardItem, 0, 16)
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		items = append(items, pages.WizardItem{
			Label: filepath.Base(path), Path: path, Current: path == m.analyzeSpecPath,
		})
	}
	add(m.analyzeSpecPath)
	for _, it := range wizardDirItems(m.analyzeSpecPath, m.analyzeSpecPath) {
		add(it.Path)
	}
	for _, it := range wizardDirItems(".", m.analyzeSpecPath) {
		add(it.Path)
	}
	if len(items) > wizardTemplateLimit {
		items = items[:wizardTemplateLimit]
	}

	return items
}

// analyzePcapDirItems lists the *.pcap files in dir (sorted by name,
// capped); an unreadable directory yields nil (the empty-state line
// covers it).
func analyzePcapDirItems(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, 16)
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".pcap") {
			continue
		}
		names = append(names, filepath.Join(dir, e.Name()))
	}
	sort.Strings(names)
	if len(names) > wizardTemplateLimit {
		names = names[:wizardTemplateLimit]
	}

	return names
}

func (m *RootModel) analyzeGoalRadios() []pages.AnalyzeRadio {
	goals := []pages.AnalyzeRadio{
		{Key: "t", Label: "transactions + datasets", Selected: m.analyzeGoal == pages.AnalyzeGoalTransactions},
		{Key: "r", Label: "mock routes", Selected: m.analyzeGoal == pages.AnalyzeGoalMockRoutes},
		{Key: "s", Label: "scenario flow", Selected: m.analyzeGoal == pages.AnalyzeGoalScenario},
	}

	return goals
}

// analyzeHeaderList is the length-header list, sourced from the one
// canonical set the engine accepts. The step starts unchosen — an unchosen
// framing rides the run legs as "" (the engine default); the operator's
// pick, if any, is marked selected and ordered first.
func (m *RootModel) analyzeHeaderList() []pages.AnalyzeHeaderItem {
	headers := utils.LengthTypeOptions()
	items := make([]pages.AnalyzeHeaderItem, 0, len(headers))
	add := func(h string) {
		items = append(items, pages.AnalyzeHeaderItem{Header: h, Selected: h == m.analyzeHeader})
	}
	for _, h := range headers {
		if h == m.analyzeHeader {
			add(h)
		}
	}
	for _, h := range headers {
		if h != m.analyzeHeader {
			add(h)
		}
	}

	return items
}

func (m *RootModel) analyzeMaskRadios() []pages.AnalyzeRadio {
	return []pages.AnalyzeRadio{
		{Key: "m", Label: "mask sensitive fields", Selected: !m.analyzeMaskRaw},
		{Key: "r", Label: "keep raw (unsecure)", Selected: m.analyzeMaskRaw},
	}
}

// analyzeFlowRows renders the run step's flows block: direction rows are
// independent selectable units, except under the scenario goal where both
// directions of a port mirror the port's selection. Selected is root's
// committed run set.
func (m *RootModel) analyzeFlowRows() []pages.AnalyzeFlowRow {
	scenario := m.analyzeGoal == pages.AnalyzeGoalScenario
	rows := make([]pages.AnalyzeFlowRow, 0, len(m.analyzeFlows))
	for _, f := range m.analyzeFlows {
		selected := m.selectedFlow(f.ServerPort, f.Direction)
		if scenario {
			selected = m.selectedPort(f.ServerPort)
		}
		rows = append(rows, pages.AnalyzeFlowRow{
			Port:       f.ServerPort,
			PeerPort:   f.PeerPort,
			Direction:  f.Direction,
			Msgs:       f.Count,
			MTIs:       analyzeMTIsText(f.MTIHistogram),
			Signon:     f.SignonCount > 0,
			Selectable: true,
			Selected:   selected,
		})
	}

	return rows
}

// selectedFlow reports whether one (port, direction) is in the committed run set.
func (m *RootModel) selectedFlow(port int, dir string) bool {
	for _, s := range m.analyzeSelected {
		if s.Port == port && s.Dir == dir {
			return true
		}
	}

	return false
}

// selectedPort reports whether EITHER direction of a port is in the run set —
// the scenario goal's port-level display and toggle unit.
func (m *RootModel) selectedPort(port int) bool {
	for _, s := range m.analyzeSelected {
		if s.Port == port {
			return true
		}
	}

	return false
}

// analyzeMTIsText formats one flow's histogram deterministically:
// "0200(180) 0800(25)".
func analyzeMTIsText(hist []app.AnalyzeMTICount) string {
	parts := make([]string, 0, len(hist))
	for _, h := range hist {
		parts = append(parts, h.MTI+"("+strconv.Itoa(h.Count)+")")
	}

	return strings.Join(parts, " ")
}

// elapsedCell renders a run duration the way the §J elapsed cell shows
// it: "340ms" under a second, else "1.2s".
func elapsedCell(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Second {
		return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
	}

	return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + "s"
}
