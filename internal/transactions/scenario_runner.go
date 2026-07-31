package transactions

import (
	"time"

	"jiso/internal/service"
)

type TestReport struct {
	ScenarioName string       `json:"scenario_name"`
	Description  string       `json:"description"`
	Success      bool         `json:"success"`
	StartTime    time.Time    `json:"start_time"`
	EndTime      time.Time    `json:"end_time"`
	DurationMs   int64        `json:"duration_ms"`
	Steps        []StepResult `json:"steps"`
}

type StepResult struct {
	StepName         string            `json:"step_name"`
	Success          bool              `json:"success"`
	LatencyMs        int64             `json:"latency_ms"`
	RequestPayload   string            `json:"request_payload,omitempty"`
	ResponsePayload  string            `json:"response_payload,omitempty"`
	Error            string            `json:"error,omitempty"`
	ValidationErrors []ValidationError `json:"validation_errors,omitempty"`
}

type ValidationError struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Message  string `json:"message"`
}

type ScenarioRunner struct {
	svc              *service.Service
	tc               *TransactionCollection
	sessionState     map[string]string
	selectedDatasets map[string]map[string]string
}

func NewScenarioRunner(svc *service.Service, tc *TransactionCollection) *ScenarioRunner {
	return &ScenarioRunner{
		svc:              svc,
		tc:               tc,
		sessionState:     make(map[string]string),
		selectedDatasets: make(map[string]map[string]string),
	}
}

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
	for _, step := range scenario.Steps {
		res := sr.runStep(step, scenario.DatasetName)
		report.Steps = append(report.Steps, res)

		if !res.Success {
			allSuccess = false
			break // Fail-fast on scenario assertion errors
		}
	}

	endTime := time.Now()
	report.EndTime = endTime
	report.DurationMs = endTime.Sub(startTime).Milliseconds()
	report.Success = allSuccess
	return report, nil
}
