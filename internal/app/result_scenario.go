package app

import (
	"time"

	"jiso/internal/transactions"
)

// ScenarioReport is the JSON-serializable view of one scenario execution.
// It mirrors what the legacy `run-scenario` terminal report printed
// (internal/command RunScenarioCommand.printReport) plus the totals the
// JSON report file (RunScenarioCommand.saveReport) exposed, so CLI --json
// and the future TUI render the same data from one shape.
type ScenarioReport struct {
	ScenarioName string             `json:"scenario_name"`
	Description  string             `json:"description"`
	Success      bool               `json:"success"`
	StartTime    time.Time          `json:"start_time"`
	EndTime      time.Time          `json:"end_time"`
	Duration     time.Duration      `json:"duration"`
	TotalSteps   int                `json:"total_steps"`
	PassedSteps  int                `json:"passed_steps"`
	FailedSteps  int                `json:"failed_steps"`
	Steps        []ScenarioStepView `json:"steps"`
}

// ScenarioStepView is one executed scenario step. Status is the rendered
// form of Success ("passed"/"failed") kept alongside the boolean so text
// frontends do not re-derive it.
type ScenarioStepView struct {
	Index            int                      `json:"index"`
	StepName         string                   `json:"step_name"`
	Success          bool                     `json:"success"`
	Status           string                   `json:"status"`
	Latency          time.Duration            `json:"latency"`
	RequestPayload   string                   `json:"request_payload,omitempty"`
	ResponsePayload  string                   `json:"response_payload,omitempty"`
	Error            string                   `json:"error,omitempty"`
	ValidationErrors []ScenarioValidationView `json:"validation_errors,omitempty"`
}

// ScenarioValidationView is one failed assertion of a scenario step.
type ScenarioValidationView struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Message  string `json:"message"`
}

// NewScenarioReport copies a transactions.TestReport (produced by
// transactions.ScenarioRunner.RunScenario) into plain fields. A nil report
// yields nil.
func NewScenarioReport(report *transactions.TestReport) *ScenarioReport {
	if report == nil {
		return nil
	}

	view := &ScenarioReport{
		ScenarioName: report.ScenarioName,
		Description:  report.Description,
		Success:      report.Success,
		StartTime:    report.StartTime,
		EndTime:      report.EndTime,
		Duration:     time.Duration(report.DurationMs) * time.Millisecond,
		TotalSteps:   len(report.Steps),
		Steps:        make([]ScenarioStepView, 0, len(report.Steps)),
	}

	for i, step := range report.Steps {
		status := "passed"
		if !step.Success {
			status = "failed"
			view.FailedSteps++
		} else {
			view.PassedSteps++
		}

		stepView := ScenarioStepView{
			Index:           i + 1,
			StepName:        step.StepName,
			Success:         step.Success,
			Status:          status,
			Latency:         time.Duration(step.LatencyMs) * time.Millisecond,
			RequestPayload:  step.RequestPayload,
			ResponsePayload: step.ResponsePayload,
			Error:           step.Error,
		}

		if len(step.ValidationErrors) > 0 {
			stepView.ValidationErrors = make([]ScenarioValidationView, 0, len(step.ValidationErrors))
			for _, valErr := range step.ValidationErrors {
				stepView.ValidationErrors = append(stepView.ValidationErrors, ScenarioValidationView{
					Field:    valErr.Field,
					Expected: valErr.Expected,
					Actual:   valErr.Actual,
					Message:  valErr.Message,
				})
			}
		}

		view.Steps = append(view.Steps, stepView)
	}

	return view
}
