// worker_wizard_state.go holds the §H worker-wizard contract: ONE
// wizard type serves both background-send and stress starts (tx ▸
// rate/params ▸ run). The start legs stay root-side; the validation
// strings here are shared by the wizard's inline checks and the root's
// start-leg re-check so they cannot drift apart.
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

// WorkerTxVisibleRows is the transaction list's window height; cursor
// movement auto-scrolls the window and the marker lines above/below make
// the scrollability obvious.
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

// Prefills mirror the CLI `jiso stress` flag defaults (stress) and the
// REPL bgsend defaults.
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

// Inline validation strings.
const (
	workerErrSelectTx      = "select at least one transaction"
	workerErrNotNumber     = "please enter a valid number"
	workerErrNotDuration   = "please enter a valid duration"
	workerErrIntervalBound = "interval must be greater than 0"
	workerErrCountBound    = "count must be a number greater than 0"
)

// StressBoundsError returns the first out-of-bounds message for the
// stress numbers ("" when all are in bounds); the wizard's step-2 Enter
// and the root's start leg both gate on it.
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

// ResolveStressRun validates the raw step-2 strings in fixed order
// (names, tps, workers, ramp, duration, bounds) and returns the resolved
// run, or a zero run plus the inline error text.
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

// ResolveBgRun validates the bgsend shape in fixed order (interval, then
// count).
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
// names, the start-leg in-flight stamp, and the root-side progress and
// error lines.
type WorkerWizardState struct {
	TxItems  []WizardItem
	InFlight bool
	Progress string
	Error    string
}

// WorkerWizardStartMsg is Enter on the run step: the root runs the
// start leg with the resolved params.
type WorkerWizardStartMsg struct{ Run WorkerRun }

// WorkerWizardCloseMsg is Esc on the first step: the root drops the
// modal; a start leg in flight keeps reporting into the guarded seam.
type WorkerWizardCloseMsg struct{}

// WorkerWizardBrowseMsg is [f] on the transaction step: open the shared
// file picker over .json tx files; the pick refreshes the candidate list
// and stays on step 1.
type WorkerWizardBrowseMsg struct{}
