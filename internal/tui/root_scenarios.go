// root_scenarios.go builds the ScenariosState from the app's loaded tx
// file and derives the display strings the page renders. Root owns all
// app access; the page consumes snapshots only.
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

// scenarioReportDefaultPath is the export destination for `e`: the CLI
// --report flag has no default, so the TUI fixes scenario-report.json in
// the cwd and writes the same TestReport JSON there.
const scenarioReportDefaultPath = "scenario-report.json"

// scenarioReportPath is the export destination root uses for `e`.
func (m *RootModel) scenarioReportPath() string {
	if m.scenarioExportPath != "" {
		return m.scenarioExportPath
	}

	return scenarioReportDefaultPath
}

// scenarioCollection returns the concrete collection behind the app's
// Repository (nil without an app or with a foreign implementation).
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

// syncScenarios pushes a fresh snapshot into the canonical page instance.
func (m *RootModel) syncScenarios() {
	if m.scenarios == nil {
		return
	}
	m.scenarios.SetState(m.scenariosState())
}

// scenariosState derives the snapshot: the scenario list, the step rows
// for the scenario under the cursor (live-run rows while that scenario
// runs, declared pending rows otherwise), run flags, and the banners.
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
		// Predict the page's clamp to row 0 on the first SetState so the
		// very first snapshot already carries the selected scenario's steps.
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

// declaredSteps builds the pending step rows for scenario id (Index,
// Name, template MTI); an unknown scenario yields no rows.
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
// template via the repository's Info; "" (dash) when unknown, never guessed.
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

// scenarioStepNote derives a step row's sub-line from the app's
// ScenarioStepView fields: the engine error verbatim when set, else the
// validation diff per failed assertion, else the extract/validate pass
// summary (expectations come from the scenario definition).
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
// assertions as field=expect, regex as field~re, existence as field exists=t/f.
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
// response payload, best-effort unpacked with the app's spec. "" (dash)
// when unknown, never guessed.
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
