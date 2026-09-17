package transactions

import (
	"time"

	"jiso/internal/service"
)

// TestReport is the result of one scenario run: per-step outcome with the request
// and response payloads, timings, and the validation failures behind a failed
// step. It is the shape --json prints, so these field names are operator-facing.
type TestReport struct {
	ScenarioName string       `json:"scenario_name"`
	Description  string       `json:"description"`
	Success      bool         `json:"success"`
	StartTime    time.Time    `json:"start_time"`
	EndTime      time.Time    `json:"end_time"`
	DurationMs   int64        `json:"duration_ms"`
	Steps        []StepResult `json:"steps"`
}

// StepResult is one step of a TestReport. The payloads are the wire dumps an
// operator reads when a step goes wrong, carried only when there is one to read.
type StepResult struct {
	StepName         string            `json:"step_name"`
	Success          bool              `json:"success"`
	LatencyMs        int64             `json:"latency_ms"`
	RequestPayload   string            `json:"request_payload,omitempty"`
	ResponsePayload  string            `json:"response_payload,omitempty"`
	Error            string            `json:"error,omitempty"`
	ValidationErrors []ValidationError `json:"validation_errors,omitempty"`
}

// addValidationFailure records a validation error and marks the step failed.
func (r *StepResult) addValidationFailure(e ValidationError) {
	r.Success = false
	r.ValidationErrors = append(r.ValidationErrors, e)
}

// ValidationError is one assertion that did not hold, naming the field and what
// was expected, so a failed step says which check failed rather than only that
// it failed.
type ValidationError struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Message  string `json:"message"`
}

// StepProgress is one per-step progress event emitted by RunScenario
// when the runner has an Observe hook installed (the TUI
// scenarios page streams live step state). Started=true fires before the
// step runs; Started=false fires right after it returns, carrying the
// finished StepResult and the session values this step newly wrote via
// its extract map (values already present before the step, or unchanged
// by it, are not reported again). A nil observer — every CLI path —
// changes nothing about execution order, results, or reports.
type StepProgress struct {
	Index     int // 1-based, matches TestReport.Steps order
	StepName  string
	Started   bool
	Result    *StepResult // set when Started == false
	Extracted map[string]string
}

// ScenarioRunner runs scenarios over the application's one connection, carrying
// session state -- extracted values and the dataset rows chosen -- between steps,
// so a scenario is a conversation rather than a list of unrelated sends.
type ScenarioRunner struct {
	svc              *service.Service
	tc               *TransactionCollection
	sessionState     map[string]string
	selectedDatasets map[string]map[string]string

	// Observe, when non-nil, receives StepProgress events around every
	// step of RunScenario (see StepProgress). The CLI never sets it.
	Observe func(StepProgress)
}

// observe fires the hook when installed; nil-safe by design so the
// RunScenario loop stays readable.
func (sr *ScenarioRunner) observe(p StepProgress) {
	if sr.Observe != nil {
		sr.Observe(p)
	}
}

// snapshotSession copies the current session (extract) state so the
// post-step progress event can diff out only the values this step wrote.
func (sr *ScenarioRunner) snapshotSession() map[string]string {
	snap := make(map[string]string, len(sr.sessionState))
	for k, v := range sr.sessionState {
		snap[k] = v
	}

	return snap
}

// extractSince returns the session entries added or changed since snap
// (nil when nothing changed — the JSON omits it).
func (sr *ScenarioRunner) extractSince(snap map[string]string) map[string]string {
	var out map[string]string
	for k, v := range sr.sessionState {
		if old, ok := snap[k]; !ok || old != v {
			if out == nil {
				out = make(map[string]string)
			}
			out[k] = v
		}
	}

	return out
}

// NewScenarioRunner returns a runner with empty session state; every run starts
// with no extracted values, so two runs of a scenario do not inherit each other.
func NewScenarioRunner(svc *service.Service, tc *TransactionCollection) *ScenarioRunner {
	return &ScenarioRunner{
		svc:              svc,
		tc:               tc,
		sessionState:     make(map[string]string),
		selectedDatasets: make(map[string]map[string]string),
	}
}

// RunScenario runs the named scenario to completion and returns its report: steps
// whose assertions failed are in the report, marked failed, because seeing the
// remaining steps is the point of writing them. A run that cannot proceed (a
// missing scenario, a send that could not be attempted) returns no report at all,
// only the error, since there would be nothing in it yet.
func (sr *ScenarioRunner) RunScenario(name string) (*TestReport, error) {
	scenario, err := sr.tc.GetScenario(name)
	if err != nil {
		return nil, err
	}

	startTime := time.Now()
	report := &TestReport{
		ScenarioName: name,
		Description:  scenario.Description,
		StartTime:    startTime,
		Steps:        make([]StepResult, 0, len(scenario.Steps)),
	}

	allSuccess := true
	for i, step := range scenario.Steps {
		sr.observe(StepProgress{Index: i + 1, StepName: step.Name, Started: true})
		before := sr.snapshotSession()

		res := sr.runStep(step, scenario.DatasetName)

		sr.observe(StepProgress{
			Index:     i + 1,
			StepName:  step.Name,
			Result:    &res,
			Extracted: sr.extractSince(before),
		})
		report.Steps = append(report.Steps, res)

		if !res.Success {
			allSuccess = false
		}
	}

	endTime := time.Now()
	report.EndTime = endTime
	report.DurationMs = endTime.Sub(startTime).Milliseconds()
	report.Success = allSuccess
	return report, nil
}
