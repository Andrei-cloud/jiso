// root_scenarios.go builds the §F ScenariosState from the app's loaded
// tx file and derives the display strings the page renders (SCR-506).
// Root owns all App access: the scenario list comes from the same
// TransactionCollection the CLI `scenario list` walks, step MTIs from the
// repository's Info (field 0 of the template — the §B mtiFromFields
// helper), and the completed-report views from app.NewScenarioReport
// (the APP-203 shape) so notes reuse its fields instead of re-deriving.
package tui

import (
	"sort"
	"strings"

	"github.com/moov-io/iso8583"

	"jiso/internal/app"
	"jiso/internal/transactions"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/theme"
)

// scenarioReportDefaultPath is the §F export destination. Source of the
// convention: the CLI `scenario run --report` flag (internal/cli/cmd
// scenario.go) has NO default — it only saves when a path is given, and
// RunScenarioCommand.saveReport writes the marshalled TestReport there
// verbatim. The TUI needs one fixed destination for `e`, so it uses
// scenario-report.json in the cwd and writes the same TestReport JSON;
// a user-set path via the palette lands with TUI-406b (forms).
const scenarioReportDefaultPath = "scenario-report.json"

// scenarioReportPath is the export destination root uses for `e`.
func (m *RootModel) scenarioReportPath() string {
	if m.scenarioExportPath != "" {
		return m.scenarioExportPath
	}

	return scenarioReportDefaultPath
}

// scenarioCollection returns the concrete collection behind the app's
// Repository (the CLI scenario commands type-assert the same way; nil
// without an app or with a foreign implementation).
func (m *RootModel) scenarioCollection() *transactions.TransactionCollection {
	if m.app == nil {
		return nil
	}
	tc, ok := m.app.Transactions().(*transactions.TransactionCollection)
	if !ok {
		return nil
	}

	return tc
}

// syncScenarios pushes a fresh §F snapshot into the canonical page
// instance (Update-wrapper placement mirrors syncDashboard/
// syncTransactions/syncSend).
func (m *RootModel) syncScenarios() {
	if m.scenarios == nil {
		return
	}
	m.scenarios.SetState(m.scenariosState())
}

// scenariosState derives the §F snapshot: the scenario list, the step
// rows for the scenario under the page's cursor (live run rows while
// that scenario runs, declared pending rows otherwise), the run flags,
// the final banner, and the export status line.
func (m *RootModel) scenariosState() pages.ScenariosState {
	st := pages.ScenariosState{
		ReportPath: m.scenarioReportPath(),
		StatusLine: m.scenarioStatusLine,
		Preview:    m.scenarioDetail.preview,
	}
	tc := m.scenarioCollection()
	if tc != nil {
		for _, name := range tc.ListScenarios() {
			display := name
			if scenario, err := tc.GetScenario(name); err == nil && scenario.Name != "" {
				display = scenario.Name
			}
			st.Scenarios = append(st.Scenarios, pages.ScenarioRow{ID: name, Name: display})
		}
	}

	sel := m.scenarios.SelectedID()
	if sel == "" && len(st.Scenarios) > 0 {
		// The page clamps its cursor to row 0 on the first SetState;
		// predict that so the very first snapshot already carries the
		// selected scenario's steps.
		sel = st.Scenarios[0].ID
	}
	if r := m.scenarioRun; r != nil {
		st.Running = !r.done
		if r.id == sel {
			st.SelectedSteps = append([]pages.StepRow(nil), r.steps...)
			st.Summary = r.summary

			return st
		}
	}
	if sel != "" {
		st.SelectedSteps = m.declaredSteps(sel)
	}

	return st
}

// declaredSteps builds the pending step rows for scenario id (Index +
// Name + template MTI). Unknown scenario → no rows (the page shows its
// select-a-scenario hint).
func (m *RootModel) declaredSteps(id string) []pages.StepRow {
	tc := m.scenarioCollection()
	if tc == nil {
		return nil
	}
	scenario, err := tc.GetScenario(id)
	if err != nil {
		return nil
	}
	rows := make([]pages.StepRow, 0, len(scenario.Steps))
	for i, step := range scenario.Steps {
		rows = append(rows, pages.StepRow{
			Index:  i + 1,
			Name:   step.Name,
			MTI:    m.stepMTI(step),
			Status: pages.StepPending,
		})
	}

	return rows
}

// stepMTI is the step row's MTI: field 0 of the step's transaction
// template via the repository's Info (the §B derivation); "" (dash) for
// template-less steps or repository failures — unknown, never guessed.
func (m *RootModel) stepMTI(step transactions.ScenarioStep) string {
	if m.app == nil || step.UseTransactionID == "" {
		return ""
	}
	info, err := m.app.Transactions().Info(step.UseTransactionID)
	if err != nil {
		return ""
	}

	return mtiFromFields(info.FieldsJSON)
}

// scenarioAssertions returns the validate list of scenario id's step
// index (for the pass sub-line "validate 39=00"); nil on any miss.
func (m *RootModel) scenarioAssertions(id string, index int) []transactions.Assertion {
	tc := m.scenarioCollection()
	if tc == nil {
		return nil
	}
	scenario, err := tc.GetScenario(id)
	if err != nil || index < 0 || index >= len(scenario.Steps) {
		return nil
	}

	return scenario.Steps[index].Validate
}

// scenarioStepNote derives a step row's sub-line from the APP-203
// ScenarioStepView fields (never re-derived from raw payloads): the
// engine error verbatim when set, else the validation diff
// `39 expect "00" got "96"` per failed assertion, else the pass summary
// `extract AuthId=482913 · validate 39=00` (extract values come from the
// engine's per-step progress event, assertion expectations from the
// scenario definition — a passed assertion's actual equals its expect).
func scenarioStepNote(th *theme.Theme, sv app.ScenarioStepView, extracted map[string]string, assertions []transactions.Assertion) string {
	if sv.Error != "" {
		return sv.Error
	}
	if len(sv.ValidationErrors) > 0 {
		parts := make([]string, 0, len(sv.ValidationErrors))
		for _, v := range sv.ValidationErrors {
			part := `expect "` + v.Expected + `" got "` + v.Actual + `"`
			if v.Field != "" {
				part = v.Field + " " + part
			}
			parts = append(parts, part)
		}

		return strings.Join(parts, th.Separator())
	}

	var segs []string
	if len(extracted) > 0 {
		keys := make([]string, 0, len(extracted))
		for k := range extracted {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			pairs = append(pairs, k+"="+extracted[k])
		}
		segs = append(segs, "extract "+strings.Join(pairs, " "))
	}
	if seg := validateSeg(assertions); seg != "" {
		segs = append(segs, seg)
	}

	return strings.Join(segs, th.Separator())
}

// validateSeg renders "validate 39=00 7=auto"-style expectations: exact
// assertions as field=expect, regex as field~re, existence as
// field exists=t/f. Empty when the step declares no assertions.
func validateSeg(assertions []transactions.Assertion) string {
	parts := make([]string, 0, len(assertions))
	for _, a := range assertions {
		switch {
		case a.Expect != "":
			parts = append(parts, a.Field+"="+a.Expect)
		case a.Regex != "":
			parts = append(parts, a.Field+"~"+a.Regex)
		case a.Exists != nil:
			parts = append(parts, a.Field+" exists=true")
			if !*a.Exists {
				parts[len(parts)-1] = a.Field + " exists=false"
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}

	return "validate " + strings.Join(parts, " ")
}

// scenarioStepRC is the step row's response code: field 39 of the step's
// response payload, best-effort unpacked with the app's spec (the same
// spec the engine asserted against). "" (dash) when no payload, no spec,
// or the payload does not unpack — unknown, never guessed.
func (m *RootModel) scenarioStepRC(result *transactions.StepResult) string {
	if m.app == nil || result == nil || result.ResponsePayload == "" {
		return ""
	}
	svc := m.app.Service()
	if svc == nil {
		return ""
	}
	spec := svc.GetSpec()
	if spec == nil {
		return ""
	}
	msg := iso8583.NewMessage(spec)
	if err := msg.Unpack([]byte(result.ResponsePayload)); err != nil {
		return ""
	}
	rc, err := msg.GetString(39)
	if err != nil {
		return ""
	}

	return rc
}
