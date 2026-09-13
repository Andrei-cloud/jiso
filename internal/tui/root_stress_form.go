// root_stress_form.go owns the §H stress start leg (SCR-508, reworked
// into the worker wizard by UAT round 4: "the option for background
// and for stress testing probably should be in a form of wizard
// rather than on empty pane two options as now"). The wizard page
// (pages.WorkerWizard) collects the tx multi-select and the PAR-306
// parameters (tps 10, ramp 30s, duration 1m, workers 1 — the same
// `jiso stress` flag defaults internal/cli/cmd/stress.go carries, now
// prefilled by pages.WorkerDefault*); the root keeps the start leg:
// the shim's own bounds re-check (pages.StressBoundsError mirrors
// validateStressNumbers, so an invalid start never reaches the App)
// and StressStart through the injectable leg (the same entry the
// cobra shim and the CLI worker shim drive). A failure keeps the
// wizard open with the error line; success closes it and stamps the
// ETA bookkeeping — the workers table underneath owns the row.
package tui

import (
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"jiso/internal/config"
	"jiso/internal/tui/pages"
	"jiso/internal/tui/widgets"
)

// stressPickFileTarget routes the stress wizard's [f] tx-file pick
// through applyFilePicked (UAT: an empty checklist was a dead end).
const stressPickFileTarget = "stress:file"

// stressStartResultMsg is the terminal verdict of a stress start; the
// run parameters ride along so the success path can stamp the ETA
// bookkeeping without re-reading the wizard.
type stressStartResultMsg struct {
	id  string
	run workerRunParams
	err error
}

// startStressWorker gates Enter on the wizard's run step: the resolved
// parameters pass the shim's own bounds first (the retired form's
// contract: an invalid run never reaches the App), then the wizard
// flips into its in-flight line and the start runs through the
// injectable leg (nil = app.StressStart).
func (m *RootModel) startStressWorker(run pages.WorkerRun) (tea.Model, tea.Cmd) {
	if len(run.Names) == 0 {
		return m.workerWizError("select at least one transaction")
	}
	if msg := pages.StressBoundsError(run.Tps, run.Workers, run.Ramp, run.Duration); msg != "" {
		return m.workerWizError(msg)
	}

	start := m.stressStartFn
	if start == nil {
		if m.app == nil {
			return m.workerWizError(errNoAppWired)
		}
		start = m.app.StressStart
	}

	st := m.workerWiz.State()
	st.InFlight = true
	st.Error = ""
	st.Progress = "starting " + strconv.Itoa(run.Tps) + " tps x" + strconv.Itoa(run.Workers) + " (" + run.Duration.String() + ")"
	m.workerWiz.SetState(st)
	m.debug.logf("stress start requested tps=%d ramp=%s duration=%s workers=%d", run.Tps, run.Ramp, run.Duration, run.Workers)

	return m, func() tea.Msg {
		id, err := start(run.Names, run.Tps, run.Ramp, run.Duration, run.Workers)

		return stressStartResultMsg{
			id: id,
			run: workerRunParams{
				names: run.Names, targetTps: run.Tps,
				ramp: run.Ramp, duration: run.Duration, workers: run.Workers,
			},
			err: err,
		}
	}
}

// applyStressStartResult closes the run: failure keeps the wizard open
// with the error line (no auto-retry), success closes it, toasts, and
// stamps the run parameters the progress rows derive ETAs from. The
// table row arrives on the bus — never written here.
func (m *RootModel) applyStressStartResult(msg stressStartResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if m.workerWiz != nil {
			st := m.workerWiz.State()
			st.InFlight = false
			st.Progress = ""
			st.Error = msg.err.Error()
			m.workerWiz.SetState(st)
		}
		m.debug.logf("stress start failed: %v", msg.err)

		return m, nil
	}
	if m.workerRuns == nil {
		m.workerRuns = map[string]workerRunParams{}
	}
	m.workerRuns[msg.id] = msg.run
	m.closeWorkerWizard()
	m.pushToast("stress started", widgets.ToastSuccess)
	m.debug.logf("stress started id=%s", msg.id)

	return m, nil
}

// stressFormStartDir is the wizard's [f] picker start directory: the
// loaded tx file's dir, else the working directory (absolute).
func stressFormStartDir(c *config.Config) string {
	start := "."
	if c != nil && c.GetFile() != "" {
		start = filepath.Dir(c.GetFile())
	}
	if abs, err := filepath.Abs(start); err == nil {
		start = abs
	}

	return start
}
