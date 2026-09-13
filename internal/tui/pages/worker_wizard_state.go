// worker_wizard_state.go holds the §H worker-wizard contract (UAT round
// 4): ONE wizard type serves both background-send and stress starts —
// "the option for background and for stress testing probably should be
// in a form of wizard rather than on empty pane two options as now".
// The wizard is tx ▸ rate/params ▸ run: a scrollable 5-row transaction
// list (multi-select for stress, single-select for bgsend), labeled
// inline parameter rows, and a summary run step whose Enter emits the
// start message. The start legs (App StressStart/WorkerStart) stay
// root-side; the page owns presentation and the resolved params.
//
// The bounds/validation strings moved here from the retired §N2
// ConnectDialog forms (root_stress_form.go / root_workers_form.go) so
// the wizard's inline validation and the root's start-leg re-check
// share ONE source: the stress strings mirror the PAR-306 shim's own
// validateStressNumbers texts, the bgsend strings the App's own bounds.
package pages

import (
	"strconv"
	"strings"
	"time"
)

// Wizard modes (the WorkersOpenFormMsg.Kind values the page emits).
const (
	WorkerModeStress = "stress"
	WorkerModeBg     = "bgsend"
)

// Wizard step indices: stress reads "1 tx ▸ 2 rate ▸ 3 run" on the rail,
// bgsend "1 tx ▸ 2 params ▸ 3 run" (the step names differ, the machine
// does not).
const (
	WorkerStepTx     = 0 // 1 transaction list
	WorkerStepParams = 1 // 2 rate (stress) / params (bgsend)
	WorkerStepRun    = 2 // 3 summary + start
	WorkerStepCount  = 3
)

// workerStepNames are the rail labels per mode.
var workerStepNames = map[string][]string{
	WorkerModeStress: {"tx", "rate", "run"},
	WorkerModeBg:     {"tx", "params", "run"},
}

// WorkerTxVisibleRows is the transaction list's window height (UAT round
// 4: "a subwindow in modal for list of transaction with only 5 visible
// but window is scrollable"). Cursor movement auto-scrolls the window;
// the "▴ n above" / "v n below" marker lines make the scrollability
// obvious.
const WorkerTxVisibleRows = 5

// Parameter keys of the step-2 inline rows.
const (
	WorkerParamTps      = "tps"
	WorkerParamRamp     = "ramp"
	WorkerParamDuration = "duration"
	WorkerParamWorkers  = "workers"
	WorkerParamCount    = "count"
	WorkerParamInterval = "interval"
)

// Prefills mirror the SAME sources the retired forms used: the stress
// defaults are the PAR-306 `jiso stress` flag defaults
// (internal/cli/cmd/stress.go: tps 10, ramp 30s, duration 1m,
// workers 1); the bgsend defaults are the REPL bgsend survey defaults
// (interval "1s", count "1").
const (
	WorkerDefaultTps      = "10"
	WorkerDefaultRamp     = "30s"
	WorkerDefaultDuration = "1m"
	WorkerDefaultWorkers  = "1"

	WorkerDefaultInterval = "1s"
	WorkerDefaultCount    = "1"
)

// WorkerRun carries one wizard's resolved start parameters (the
// stress shape fills the first group, the bgsend shape the second).
type WorkerRun struct {
	Mode     string
	Names    []string
	Tps      int
	Ramp     time.Duration
	Duration time.Duration
	Workers  int

	Name     string
	Count    int
	Interval time.Duration
}

// Inline validation strings (verbatim from the retired §N2 forms).
const (
	workerErrSelectTx      = "select at least one transaction"
	workerErrNotNumber     = "please enter a valid number"
	workerErrNotDuration   = "please enter a valid duration"
	workerErrIntervalBound = "interval must be greater than 0"
	workerErrCountBound    = "count must be a number greater than 0"
)

// StressBoundsError mirrors the PAR-306 shim's validateStressNumbers
// texts ("" when the values are in bounds); the wizard's step-2 Enter
// and the root's start leg both gate on it, so an invalid run never
// reaches the App.
func StressBoundsError(tps, workers int, ramp, duration time.Duration) string {
	switch {
	case tps <= 0:
		return "TPS must be greater than 0"
	case tps > 100000:
		return "TPS cannot exceed 100000"
	case workers <= 0:
		return "workers must be greater than 0"
	case workers > 50:
		return "workers cannot exceed 50"
	case ramp < 0:
		return "ramp must not be negative, got " + ramp.String()
	case duration <= 0:
		return "duration must be greater than 0, got " + duration.String()
	}

	return ""
}

// ResolveStressRun validates the raw step-2 strings in the retired
// form's exact order (names, tps, workers, ramp, duration, bounds) and
// returns the resolved run plus "" — or a zero run plus the inline
// error text.
func ResolveStressRun(names []string, tpsS, rampS, durationS, workersS string) (WorkerRun, string) {
	if len(names) == 0 {
		return WorkerRun{}, workerErrSelectTx
	}
	tps, err := strconv.Atoi(strings.TrimSpace(tpsS))
	if err != nil {
		return WorkerRun{}, workerErrNotNumber
	}
	workers, err := strconv.Atoi(strings.TrimSpace(workersS))
	if err != nil {
		return WorkerRun{}, workerErrNotNumber
	}
	ramp, err := time.ParseDuration(strings.TrimSpace(rampS))
	if err != nil {
		return WorkerRun{}, workerErrNotDuration
	}
	duration, err := time.ParseDuration(strings.TrimSpace(durationS))
	if err != nil {
		return WorkerRun{}, workerErrNotDuration
	}
	if msg := StressBoundsError(tps, workers, ramp, duration); msg != "" {
		return WorkerRun{}, msg
	}

	return WorkerRun{
		Mode: WorkerModeStress, Names: names,
		Tps: tps, Ramp: ramp, Duration: duration, Workers: workers,
	}, ""
}

// ResolveBgRun validates the bgsend shape in the retired form's exact
// order (interval, then count).
func ResolveBgRun(name, intervalS, countS string) (WorkerRun, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return WorkerRun{}, workerErrSelectTx
	}
	interval, err := time.ParseDuration(strings.TrimSpace(intervalS))
	if err != nil || interval <= 0 {
		return WorkerRun{}, workerErrIntervalBound
	}
	count, err := strconv.Atoi(strings.TrimSpace(countS))
	if err != nil || count < 1 {
		return WorkerRun{}, workerErrCountBound
	}

	return WorkerRun{
		Mode: WorkerModeBg, Name: name,
		Interval: interval, Count: count,
	}, ""
}

// WorkerWizardState is the root-pushed snapshot: the loaded transaction
// names (the repository's ListNames, the retired forms' option source),
// the start-leg in-flight stamp, and the root-side lines (Progress
// while the leg runs, Error when it or the tx-file load failed).
type WorkerWizardState struct {
	TxItems  []WizardItem
	InFlight bool
	Progress string
	Error    string
}

// WorkerWizardStartMsg is Enter on the run step: the root runs the
// start leg with the resolved params (the same StressStart/WorkerStart
// entries the CLI shims drive).
type WorkerWizardStartMsg struct{ Run WorkerRun }

// WorkerWizardCloseMsg is Esc on the first step: the root drops the
// modal (a start leg in flight keeps reporting into the guarded result
// seam, exactly like the retired forms' Esc).
type WorkerWizardCloseMsg struct{}

// WorkerWizardBrowseMsg is [f] on the transaction step: open the shared
// file picker over .json tx files (the AnalyzeBrowseMsg pattern; the
// pick refreshes the candidate list and stays on step 1).
type WorkerWizardBrowseMsg struct{}
