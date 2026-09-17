// root_scenario_run.go owns the live scenario run: root runs the same
// engine the CLI `scenario run` uses in a goroutine and feeds every
// per-step transition back as root-internal messages through the
// injectable sender, so tests need no real program.
//
// The engine's StepProgress hook is optional: events ride straight into
// scenarioStepMsg, and a run without the hook still lands correctly
// because applyScenarioDone backfills unresolved rows from the final report.
package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/app"
	"jiso/internal/transactions"
	"jiso/internal/tui/bridge"
	"jiso/internal/tui/pages"
)

// scenarioEngine runs one scenario, optionally reporting per-step progress.
// The production leg installs the observer; tests inject a fake.
type scenarioEngine func(name string, observe func(transactions.StepProgress)) (*transactions.TestReport, error)

// scenarioStepMsg reports one per-step transition from the run goroutine
// into Update (root-internal). Started marks the pre-step event; otherwise
// Result/Extracted carry the finished step.
type scenarioStepMsg struct {
	scenarioID string
	index      int
	started    bool
	result     *transactions.StepResult
	extracted  map[string]string
}

// scenarioDoneMsg closes the run: the final report (nil on engine
// failure) and the engine error.
type scenarioDoneMsg struct {
	scenarioID string
	report     *transactions.TestReport
	err        error
}

// scenarioRun is the live-run machine truth: the step rows as last
// derived, the final banner, and the done flag (nil m.scenarioRun means
// no run has ever started).
type scenarioRun struct {
	id      string
	steps   []pages.StepRow
	summary string
	report  *transactions.TestReport
	done    bool
}

// SetScenarioSender overrides how scenario run messages reach the
// program; tests inject a collector and re-enter Update by hand.
func (m *RootModel) SetScenarioSender(send bridge.Sender) { m.scenarioSender = send }

// startScenarioRun launches (or ignores) a live run for scenario id:
// ignored without an app or while one is in flight (single live op). It
// arms the goroutine and returns no cmd; the page never ticks.
func (m *RootModel) startScenarioRun(id string) (tea.Model, tea.Cmd) {
	if m.app == nil || id == "" {
		m.debug.logf("scenario run id=%s (no app wired)", id)

		return m, nil
	}
	if r := m.scenarioRun; r != nil && !r.done {
		m.debug.logf("scenario run id=%s ignored in-flight", id)

		return m, nil
	}

	m.scenarioRun = &scenarioRun{id: id, steps: m.declaredSteps(id)}
	m.scenarioStatusLine = ""
	// A new run invalidates the step-detail leg: the old preview describes
	// a previous report, so clear it and bump the seq to turn stragglers stale.
	m.scenarioDetail.seq++
	m.scenarioDetail.preview = nil

	if m.Current().ID() != pages.ScenariosPageID {
		m.Push(m.scenarios)
		_, _ = m.scenarios.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	}
	m.syncScenarios()           // land the list so SetSelected can find the row
	m.scenarios.SetSelected(id) // steps pane follows the run, not the old cursor

	sender := m.scenarioSender
	if sender == nil {
		// No program seam: the engine leg's emits would drop and wedge the
		// run in-flight forever, so close it with a terminal failure now.
		m.scenarioRun.done = true
		m.scenarioRun.summary = "run failed: " + errNoSenderWired.Error()
		m.syncScenarios()
		m.debug.logf("scenario run id=%s closed: %v", id, errNoSenderWired)

		return m, nil
	}

	engine := m.runScenario
	if engine == nil {
		engine = m.appScenarioRun
	}

	go m.walkScenario(sender, engine, id)

	return m, nil
}

// walkScenario is the run goroutine: hook events go to the sender as they
// arrive; the final report (or error) closes the run.
func (m *RootModel) walkScenario(
	sender bridge.Sender,
	engine scenarioEngine,
	id string,
) {
	emit := func(msg tea.Msg) {
		if sender != nil {
			sender(msg)
		}
	}

	observe := func(p transactions.StepProgress) {
		emit(scenarioStepMsg{
			scenarioID: id,
			index:      p.Index,
			started:    p.Started,
			result:     p.Result,
			extracted:  p.Extracted,
		})
	}

	report, err := engine(id, observe)
	emit(scenarioDoneMsg{scenarioID: id, report: report, err: err})
}

// appScenarioRun is the production leg: the same ScenarioRunner the CLI
// scenario run builds, with the per-step observer installed. It performs
// no connect — steps honestly fail with the engine's own
// "connection is offline" until a connection exists.
func (m *RootModel) appScenarioRun(name string, observe func(transactions.StepProgress)) (*transactions.TestReport, error) {
	tc, ok := m.app.Transactions().(*transactions.TransactionCollection)
	if !ok {
		return nil, fmt.Errorf("scenario engine unavailable: tx repository is %T", m.app.Transactions())
	}

	runner := transactions.NewScenarioRunner(m.app.Service(), tc)
	if observe != nil {
		runner.Observe = observe
	}

	return runner.RunScenario(name)
}

// applyScenarioStep folds one per-step transition into the run truth:
// started lights the running row; finished derives status, latency, RC,
// and the sub-line note from the app's step view.
func (m *RootModel) applyScenarioStep(msg scenarioStepMsg) (tea.Model, tea.Cmd) {
	r := m.scenarioRun
	if r == nil || r.id != msg.scenarioID || r.done {
		return m, nil // straggler after a reset or a newer run
	}
	i := msg.index - 1
	if i < 0 || i >= len(r.steps) {
		return m, nil
	}

	if msg.started {
		r.steps[i].Status = pages.StepRunning

		return m, nil
	}
	if msg.result == nil {
		return m, nil
	}

	view := app.NewScenarioReport(&transactions.TestReport{
		Steps: []transactions.StepResult{*msg.result},
	})
	sv := view.Steps[0]

	r.steps[i].Status = pages.StepPass
	if !sv.Success {
		r.steps[i].Status = pages.StepFail
	}
	r.steps[i].Latency = sv.Latency
	r.steps[i].RC = m.scenarioStepRC(msg.result)
	r.steps[i].Note = scenarioStepNote(m.themeOrNil(), sv, msg.extracted, m.scenarioAssertions(r.id, i))

	return m, nil
}

// applyScenarioDone closes the run: the banner comes from the report
// totals, unresolved rows (a hook-less engine) are backfilled from the
// same view, and the report lands in m.scenarioLastReport for `e`.
func (m *RootModel) applyScenarioDone(msg scenarioDoneMsg) (tea.Model, tea.Cmd) {
	r := m.scenarioRun
	if r == nil || r.id != msg.scenarioID || r.done {
		return m, nil
	}
	r.done = true

	if msg.err != nil {
		r.summary = "run failed: " + msg.err.Error()

		return m, nil
	}

	r.report = msg.report
	m.scenarioLastReport = msg.report

	view := app.NewScenarioReport(msg.report)
	r.summary = formatScenarioSummary(view)

	for i, sv := range view.Steps {
		if i >= len(r.steps) {
			break
		}
		if r.steps[i].Status != pages.StepPending && r.steps[i].Status != pages.StepRunning {
			continue
		}
		r.steps[i].Status = pages.StepPass
		if !sv.Success {
			r.steps[i].Status = pages.StepFail
		}
		r.steps[i].Latency = sv.Latency
		r.steps[i].RC = m.scenarioStepRC(&msg.report.Steps[i])
		r.steps[i].Note = scenarioStepNote(m.themeOrNil(), sv, nil, m.scenarioAssertions(r.id, i))
	}

	return m, nil
}

// formatScenarioSummary renders the final banner `3/3 passed · 7ms total`
// from the report totals.
func formatScenarioSummary(v *app.ScenarioReport) string {
	return fmt.Sprintf("%d/%d passed · %s total", v.PassedSteps, v.TotalSteps, formatScenarioDuration(v.Duration))
}

// formatScenarioDuration formats the banner total: ms, seconds, or
// minutes; "" only for the never-run zero.
func formatScenarioDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < time.Second:
		ms := d.Milliseconds()
		if d != time.Duration(ms)*time.Millisecond {
			return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
		}

		return fmt.Sprintf("%dms", ms)
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%dm", int64(d.Minutes()))
	}
}
