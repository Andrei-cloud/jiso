// root_analyze_state.go derives the §J page snapshot from root's wizard
// truth (the SCR-501 data-flow contract): the page receives display data
// only — candidate lists, radio lists, flows rows with root-formatted
// MTI histograms, root-stamped elapsed text — and never touches the app
// or the clock itself. syncAnalyze runs in the Update wrapper, so every
// folded message is reflected in the next View.
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

// syncAnalyze prefills the spec/header defaults once the source exists
// (the App reports the configured spec path and effective header;
// without a source the frameProps effective-header idiom applies: the
// app default) and pushes the current snapshot into the page.
func (m *RootModel) syncAnalyze() {
	if m.analyze == nil {
		return
	}
	if !m.analyzePrefilled {
		m.prefillAnalyzeDefaults()
	}
	m.analyze.SetState(m.analyzeState())
}

// prefillAnalyzeDefaults fills the spec/header/goal defaults from the analyze
// source once, leaving any values already set by the user untouched.
func (m *RootModel) prefillAnalyzeDefaults() {
	spec, header := "", app.DefaultLengthType
	if src := m.analyzeSource(); src != nil {
		s, h := src.AnalyzeDefaults()
		if s != "" {
			spec = s
		}
		if h != "" {
			header = h
		}
	}
	if m.analyzeSpecPath == "" {
		m.analyzeSpecPath = spec
	}
	if m.analyzeHeader == "" {
		m.analyzeHeader = header
	}
	if m.analyzeGoal == "" {
		m.analyzeGoal = pages.AnalyzeGoalTransactions
	}
	m.analyzePrefilled = true
}

// analyzeState builds the immutable snapshot the page renders. The
// candidate lists are only walked while the §J page is current (the
// list pages build their data the same on-demand way); the page
// renders View nowhere else.
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

// analyzeOutputDisplay resolves the effective output file for display
// on the run step (UAT round 5): the [o] pick, else the path the last
// run bound, else the engine default for the current goal (the config
// tx file, or transactions/*.json).
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

// analyzeCaptureItems lists the capture-step candidates: the session
// recents (newest first, the m.wizardFiles pattern), the current pick,
// then the *.pcap files in the working directory.
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

// analyzeSpecItems lists the spec-step candidates with the same rules
// as the send wizard's spec step: the current spec first, then the
// *.json files beside it, then the working directory's *.json files.
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
// canonical set the engine accepts (utils.SelectLength; E5-FIX/B2: the
// old hardcoded list offered "bit31"/"llvm", which SelectLength rejects
// — a selection the engine could never honor). The current/effective
// framing leads the list so Enter with no navigation keeps it.
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

// analyzeFlowRows renders the run step's flows block. UAT round 7: every
// direction row is an independent selectable unit for the transactions and
// mock-routes goals; the scenario goal correlates a whole port, so both
// directions of a port mirror the port's selection. The selection is root's
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
