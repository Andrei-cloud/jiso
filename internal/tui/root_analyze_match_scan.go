// root_analyze_match_scan.go owns the §J matching wizard's root state:
// the scan-once leg (armAnalyzeScan/applyAnalyzeScan), the condition
// edits (the conds screen's messages fold here — root owns the condition
// truth the page only mirrors), the group-by membership, and the live
// line recomputed from the scan cache on every edit — no keystroke
// re-reads the capture.
package tui

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/analyzer"
	app "jiso/internal/app"
	"jiso/internal/tui/pages"
	"jiso/internal/utils"
)

// analyzeScanLoadedMsg closes the scan leg; seq drops stale results the
// same way every other §J leg does.
type analyzeScanLoadedMsg struct {
	seq  uint64
	scan *app.AnalyzeScan
	err  error
}

// armAnalyzeScan starts the one-time pairing leg. Like every §J leg it
// returns a tea.Cmd the program runs off the UI thread; the matching step
// is the only caller (setAnalyzeStep arriving there, and the goal
// teleport), so one arrival arms exactly one scan.
func (m *RootModel) armAnalyzeScan() tea.Cmd {
	src := m.analyzeSource()
	switch {
	case src == nil:
		m.analyzeMatchWarn = analyzeNoEngine

		return nil
	case m.analyzeCapturePath == "":
		m.analyzeMatchWarn = analyzeNeedCapture

		return nil
	case m.analyzeScanWait:
		return nil // one leg per arrival; a re-armed wait is a doubled read
	}
	m.analyzeScanWait = true
	seq := m.analyzeSeq

	return func() tea.Msg {
		out, err := src.ScanForMatch(context.Background(), app.AnalyzeScanOptions{
			PcapPath:   m.analyzeCapturePath,
			HeaderType: m.analyzeHeader,
			SpecPath:   m.analyzeSpecPath,
			Unsecure:   m.analyzeMaskRaw,
		})

		return analyzeScanLoadedMsg{seq: seq, scan: out, err: err}
	}
}

// applyAnalyzeScan folds the scan: a failure stays inline on the step (the
// conds stay editable; the live line names the cause), a success caches
// the pairs, seeds the two starter conditions once (only when the operator
// has typed none), and folds the live line.
func (m *RootModel) applyAnalyzeScan(msg analyzeScanLoadedMsg) (tea.Model, tea.Cmd) {
	m.analyzeScanWait = false
	if msg.seq != m.analyzeSeq || m.Current().ID() != pages.AnalyzePageID {
		return m, nil
	}
	if msg.err != nil || msg.scan == nil {
		m.analyzeScan = nil
		m.analyzeMatchWarn = "scan failed: " + scanErrText(msg.err)

		return m, nil
	}
	m.analyzeScan = msg.scan
	m.seedAnalyzeConds()
	m.recomputeMatchLine()

	return m, nil
}

func scanErrText(err error) string {
	if err == nil {
		return "no scan"
	}

	return err.Error()
}

// seedAnalyzeConds fills the finding's two starter conditions (req 0 =
// headline MTI, req 3 = its headline DE3) exactly once: any condition the
// operator has already typed — including one they deleted down to — is
// left alone.
func (m *RootModel) seedAnalyzeConds() {
	if len(m.analyzeConds) > 0 || m.analyzeScan == nil {
		return
	}
	mti, de3 := analyzer.HeadlineRequest(m.analyzeScan.Pairs)
	if mti == "" {
		return
	}
	m.analyzeConds = append(m.analyzeConds,
		analyzer.MatchCond{Side: analyzer.CondSideReq, Field: "0", Cond: "equals", Value: mti})
	if de3 != "" {
		m.analyzeConds = append(m.analyzeConds,
			analyzer.MatchCond{Side: analyzer.CondSideReq, Field: "3", Cond: "equals", Value: de3})
	}
}

// matchSpec derives the run/preview match spec from the wizard state.
func (m *RootModel) matchSpec() analyzer.MatchSpec {
	return analyzer.MatchSpec{
		Conditions: append([]analyzer.MatchCond(nil), m.analyzeConds...),
		GroupBy:    append([]analyzer.GroupField(nil), m.analyzeGroup...),
	}
}

// recomputeMatchLine folds "~N pairs match · M route(s)" from the scan
// cache (no capture read) and derives the inline warning: an invalid
// regex, a card-data condition, or clear.
func (m *RootModel) recomputeMatchLine() {
	m.analyzeMatchWarn = matchWarn(m.analyzeConds)
	if m.analyzeScanWait || m.analyzeScan == nil {
		m.analyzeMatchLine = ""

		return
	}
	surv, groups := analyzer.MatchPreview(m.analyzeScan.Pairs, m.matchSpec())
	m.analyzeMatchLine = "~" + strconv.Itoa(surv) + " pairs match · " + strconv.Itoa(groups) + " route(s)"
}

// matchWarn is the inline warning for the conds table: the first invalid
// regex or card-data condition (empty when the conditions are clean). An
// invalid pattern is named so the operator never reads "~0 pairs match"
// as "the capture has none of these" — an invalid pattern never matches.
func matchWarn(conds []analyzer.MatchCond) string {
	for _, c := range conds {
		if c.Cond == "regex" && c.Value != "" {
			if _, err := regexp.Compile(c.Value); err != nil {
				return "invalid regex on field " + c.Field + ": " + err.Error()
			}
		}
		if c.CondIsCard() {
			return "card/track data will be matched - captured PANs are anonymized"
		}
	}

	return ""
}

// --- conds screen messages -------------------------------------------------

func (m *RootModel) handleAnalyzeCondAdd() (tea.Model, tea.Cmd) {
	m.analyzeConds = append(m.analyzeConds, analyzer.MatchCond{
		Side: analyzer.CondSideReq, Cond: "equals",
	})
	m.analyzeRunStale = true

	return m, nil
}

func (m *RootModel) handleAnalyzeCondDelete(msg pages.AnalyzeCondDeleteMsg) (tea.Model, tea.Cmd) {
	if msg.Index < 0 || msg.Index >= len(m.analyzeConds) {
		return m, nil
	}
	m.analyzeConds = append(m.analyzeConds[:msg.Index], m.analyzeConds[msg.Index+1:]...)
	m.condEditDone()

	return m, nil
}

// handleAnalyzeCondWhen cycles the ladder; the value re-renders across
// the scalar/list boundary so editing a value never loses data silently:
// equals "000000" cycling to one-of stays a one-value list text.
func (m *RootModel) handleAnalyzeCondWhen(msg pages.AnalyzeCondWhenMsg) (tea.Model, tea.Cmd) {
	if msg.Index < 0 || msg.Index >= len(m.analyzeConds) {
		return m, nil
	}
	c := &m.analyzeConds[msg.Index]
	next := analyzer.NextCond(c.Cond)
	switch {
	case analyzer.CondTakesList(next) && !analyzer.CondTakesList(c.Cond):
		if c.Value != "" {
			c.Values = []string{c.Value}
		}
		c.Value = ""
	case !analyzer.CondTakesList(next) && analyzer.CondTakesList(c.Cond):
		if len(c.Values) > 0 {
			c.Value = strings.Join(c.Values, ",")
		}
		c.Values = nil
	}
	c.Cond = next
	m.condEditDone()

	return m, nil
}

func (m *RootModel) handleAnalyzeCondSide(msg pages.AnalyzeCondSideMsg) (tea.Model, tea.Cmd) {
	if msg.Index < 0 || msg.Index >= len(m.analyzeConds) {
		return m, nil
	}
	c := &m.analyzeConds[msg.Index]
	if c.Side == analyzer.CondSideResp {
		c.Side = analyzer.CondSideReq
	} else {
		c.Side = analyzer.CondSideResp
	}
	m.condEditDone()

	return m, nil
}

func (m *RootModel) handleAnalyzeCondSetField(msg pages.AnalyzeCondSetFieldMsg) (tea.Model, tea.Cmd) {
	if msg.Index < 0 || msg.Index >= len(m.analyzeConds) {
		return m, nil
	}
	field := strings.TrimSpace(msg.Value)
	if !matchFieldPathValid(field) {
		m.analyzeMatchWarn = "field path must be dotted ids: " + field

		return m, nil
	}
	m.analyzeConds[msg.Index].Field = field
	if warn := matchFieldSpecWarn(m.analyzeSpecPath, field); warn != "" {
		m.analyzeMatchWarn = warn
	}
	m.condEditDone()

	return m, nil
}

// handleAnalyzeCondSetValue commits the editor text: list conditions
// parse the comma-separated text (blank entries dropped), scalars keep it
// verbatim (leading zeros matter to ISO 8583 values).
func (m *RootModel) handleAnalyzeCondSetValue(msg pages.AnalyzeCondSetValueMsg) (tea.Model, tea.Cmd) {
	if msg.Index < 0 || msg.Index >= len(m.analyzeConds) {
		return m, nil
	}
	c := &m.analyzeConds[msg.Index]
	if analyzer.CondTakesList(c.Cond) {
		c.Values = splitCondList(msg.Value)
		c.Value = ""
	} else {
		c.Value = strings.TrimSpace(msg.Value)
		c.Values = nil
	}
	m.condEditDone()

	return m, nil
}

func (m *RootModel) handleAnalyzeGroupToggle(msg pages.AnalyzeGroupToggleMsg) (tea.Model, tea.Cmd) {
	side := msg.Side
	if side == "" {
		side = analyzer.CondSideReq
	}
	for i, g := range m.analyzeGroup {
		if g.Field == msg.Field && g.Side == side {
			m.analyzeGroup = append(m.analyzeGroup[:i], m.analyzeGroup[i+1:]...)
			m.condEditDone()

			return m, nil
		}
	}
	m.analyzeGroup = append(m.analyzeGroup, analyzer.GroupField{Field: msg.Field, Side: side})
	m.condEditDone()

	return m, nil
}

// condEditDone marks the run stale (the conditions changed under it),
// recomputes the live line from the cache, and clears a stale warning now
// that the row that caused it was edited.
func (m *RootModel) condEditDone() {
	m.analyzeRunStale = true
	m.recomputeMatchLine()
}

// --- field-path helpers ----------------------------------------------------

// analyzeCondViews derives the page rows from the wizard's conditions.
func (m *RootModel) analyzeCondViews() []pages.AnalyzeCond {
	out := make([]pages.AnalyzeCond, 0, len(m.analyzeConds))
	for _, c := range m.analyzeConds {
		side := c.Side
		if side == "" {
			side = analyzer.CondSideReq
		}
		row := pages.AnalyzeCond{Side: side, Field: c.Field, When: c.Cond, Value: c.Value}
		if len(c.Values) > 0 {
			row.ValuesText = strings.Join(c.Values, ", ")
		}
		out = append(out, row)
	}

	return out
}

// analyzeGroupOptions seats the group-by pane from the scan's variance
// report, marking the operator's current membership.
func (m *RootModel) analyzeGroupOptions() []pages.AnalyzeGroupOption {
	if m.analyzeScan == nil {
		return nil
	}
	out := make([]pages.AnalyzeGroupOption, 0, len(m.analyzeScan.Variances))
	for _, v := range m.analyzeScan.Variances {
		on := false
		for _, g := range m.analyzeGroup {
			if g.Field == v.Field && g.Side == v.Side {
				on = true

				break
			}
		}
		out = append(out, pages.AnalyzeGroupOption{
			Field: v.Field, Side: v.Side,
			Vary: strconv.Itoa(v.Distinct) + " distinct values",
			On:   on,
		})
	}

	return out
}

// matchFieldPathValid reports a dotted decimal id path ("4", "55.1").
func matchFieldPathValid(path string) bool {
	if path == "" {
		return false
	}
	for _, part := range strings.Split(path, ".") {
		if _, err := strconv.Atoi(part); err != nil || part == "" {
			return false
		}
	}

	return true
}

// matchFieldSpecWarn checks the top-level id against the wizard's chosen
// spec (the engine default cannot be checked before the leg resolves it,
// so "" asks nothing). A miss is a warning: the condition stays editable
// (it will simply match nothing), never a refused keystroke.
func matchFieldSpecWarn(specPath, path string) string {
	if specPath == "" {
		return ""
	}
	spec, err := utils.CreateSpecFromFile(specPath)
	if err != nil || spec == nil || spec.Fields == nil {
		return ""
	}
	top, _, _ := strings.Cut(path, ".")
	id, err := strconv.Atoi(top)
	if err != nil {
		return ""
	}
	if _, ok := spec.Fields[id]; ok {
		return ""
	}

	return "field " + top + " is not in the loaded spec"
}

// splitCondList parses the comma-separated editor text into list values.
func splitCondList(text string) []string {
	var out []string
	for _, part := range strings.Split(text, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}

	return out
}
