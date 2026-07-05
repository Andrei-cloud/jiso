package transactions

import (
	"fmt"
	"regexp"
	"time"

	"jiso/internal/service"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"
)

var (
	cardRegex    = regexp.MustCompile(`\{\{\s*card\.(\w+)\s*\}\}`)
	contextRegex = regexp.MustCompile(`\{\{\s*context\.(\w+)\s*\}\}`)
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
	svc          *service.Service
	tc           *TransactionCollection
	sessionState map[string]string
	activeCard   map[string]string
}

func NewScenarioRunner(svc *service.Service, tc *TransactionCollection) *ScenarioRunner {
	return &ScenarioRunner{
		svc:          svc,
		tc:           tc,
		sessionState: make(map[string]string),
		activeCard:   make(map[string]string),
	}
}

func (sr *ScenarioRunner) RunScenario(name string) (*TestReport, error) {
	scenario, err := sr.tc.GetScenario(name)
	if err != nil {
		return nil, err
	}

	report := &TestReport{
		ScenarioName: scenario.Name,
		Description:  scenario.Description,
		Success:      true,
		StartTime:    time.Now(),
		Steps:        make([]StepResult, 0),
	}

	// Load active card dataset row if configured
	if scenario.DatasetName != "" {
		dataset, err := sr.tc.GetDataset(scenario.DatasetName)
		if err != nil {
			return nil, fmt.Errorf("failed to load dataset '%s': %w", scenario.DatasetName, err)
		}
		if len(dataset.Data) > 0 {
			// Defaults to first card/record in the pool for this runner instance
			sr.activeCard = dataset.Data[0]
		}
	}

	// Run steps sequentially
	for _, step := range scenario.Steps {
		stepResult := sr.runStep(step)
		report.Steps = append(report.Steps, stepResult)
		if !stepResult.Success {
			report.Success = false
			break // Fail-fast: stop execution on first step failure
		}
	}

	report.EndTime = time.Now()
	report.DurationMs = report.EndTime.Sub(report.StartTime).Milliseconds()
	return report, nil
}

func (sr *ScenarioRunner) runStep(step ScenarioStep) StepResult {
	result := StepResult{
		StepName: step.Name,
		Success:  true,
	}

	if sr.svc == nil || !sr.svc.IsConnected() {
		result.Success = false
		result.Error = "connection is offline"
		return result
	}

	// 1. Compose base request message
	var msg *iso8583.Message
	if step.UseTransactionId != "" {
		var err error
		msg, err = sr.tc.Compose(step.UseTransactionId)
		if err != nil {
			result.Success = false
			result.Error = fmt.Errorf("failed to compose template '%s': %w", step.UseTransactionId, err).Error()
			return result
		}
	} else {
		msg = iso8583.NewMessage(sr.svc.GetSpec())
	}

	// 2. Build the combined fields map to support step overrides
	mergedFields := make(map[string]interface{})

	// Retrieve existing fields from the base transaction template
	for i, f := range msg.GetFields() {
		if v, err := f.Bytes(); err == nil && len(v) > 0 {
			mergedFields[fmt.Sprintf("%d", i)] = string(v)
		}
	}

	// Override or add new fields specified in the step configuration
	for k, v := range step.Fields {
		mergedFields[k] = v
	}

	reqMsg := iso8583.NewMessage(sr.svc.GetSpec())

	// 3. Interpolate variables, resolve auto fields, and apply to request message
	for k, v := range mergedFields {
		var fieldID int
		if _, err := fmt.Sscanf(k, "%d", &fieldID); err != nil {
			continue
		}

		switch val := v.(type) {
		case string:
			if val == "auto" {
				sr.tc.handleAutoFields(fieldID, reqMsg)
			} else if val == "random" {
				sr.tc.handleAutoFields(fieldID, reqMsg)
			} else {
				interpolated := sr.injectVariables(val)
				reqMsg.Field(fieldID, interpolated)
			}
		case float64:
			reqMsg.Field(fieldID, fmt.Sprintf("%.0f", val))
		case int:
			reqMsg.Field(fieldID, fmt.Sprintf("%d", val))
		default:
			reqMsg.Field(fieldID, fmt.Sprintf("%v", val))
		}
	}

	// Populate Request Payload for reporting
	reqPacked, err := reqMsg.Pack()
	if err == nil {
		result.RequestPayload = string(reqPacked)
	}

	// 4. Send over network and wait for response
	startTime := time.Now()
	respMsg, err := sr.svc.Send(reqMsg)
	result.LatencyMs = time.Since(startTime).Milliseconds()

	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("network send failed: %w", err).Error()
		return result
	}

	if respMsg == nil {
		result.Success = false
		result.Error = "received empty response"
		return result
	}

	// Populate Response Payload for reporting
	respPacked, err := respMsg.Pack()
	if err == nil {
		result.ResponsePayload = string(respPacked)
	}

	// 5. Assert validation rules
	for _, assertion := range step.Validate {
		var fieldID int
		if _, err := fmt.Sscanf(assertion.Field, "%d", &fieldID); err != nil {
			result.Success = false
			valErr := ValidationError{
				Field:   assertion.Field,
				Message: fmt.Sprintf("invalid field format: %s", assertion.Field),
			}
			result.ValidationErrors = append(result.ValidationErrors, valErr)
			continue
		}

		fieldObj := respMsg.GetField(fieldID)

		// Check existence assertion
		if assertion.Exists != nil {
			exists := hasValue(fieldObj)
			if exists != *assertion.Exists {
				result.Success = false
				valErr := ValidationError{
					Field:    assertion.Field,
					Expected: fmt.Sprintf("exists=%t", *assertion.Exists),
					Actual:   fmt.Sprintf("exists=%t", exists),
					Message:  fmt.Sprintf("Field %d existence assertion failed", fieldID),
				}
				result.ValidationErrors = append(result.ValidationErrors, valErr)
				continue
			}
		}

		if fieldObj == nil {
			if assertion.Expect != "" || assertion.Regex != "" {
				result.Success = false
				valErr := ValidationError{
					Field:    assertion.Field,
					Expected: fmt.Sprintf("expect=%s regex=%s", assertion.Expect, assertion.Regex),
					Actual:   "nil",
					Message:  fmt.Sprintf("Field %d does not exist in response", fieldID),
				}
				result.ValidationErrors = append(result.ValidationErrors, valErr)
			}
			continue
		}

		actualValue, err := fieldObj.String()
		if err != nil {
			result.Success = false
			valErr := ValidationError{
				Field:   assertion.Field,
				Message: fmt.Sprintf("failed to get string value of field %d: %v", fieldID, err),
			}
			result.ValidationErrors = append(result.ValidationErrors, valErr)
			continue
		}

		// Exact match assertion
		if assertion.Expect != "" {
			expectedInterp := sr.injectVariables(assertion.Expect)
			if actualValue != expectedInterp {
				result.Success = false
				valErr := ValidationError{
					Field:    assertion.Field,
					Expected: expectedInterp,
					Actual:   actualValue,
					Message:  fmt.Sprintf("Field %d exact match assertion failed", fieldID),
				}
				result.ValidationErrors = append(result.ValidationErrors, valErr)
				continue
			}
		}

		// Regex match assertion
		if assertion.Regex != "" {
			regexInterp := sr.injectVariables(assertion.Regex)
			re, err := regexp.Compile(regexInterp)
			if err != nil {
				result.Success = false
				valErr := ValidationError{
					Field:   assertion.Field,
					Message: fmt.Sprintf("failed to compile regex '%s': %v", regexInterp, err),
				}
				result.ValidationErrors = append(result.ValidationErrors, valErr)
				continue
			}
			if !re.MatchString(actualValue) {
				result.Success = false
				valErr := ValidationError{
					Field:    assertion.Field,
					Expected: fmt.Sprintf("regex(%s)", regexInterp),
					Actual:   actualValue,
					Message:  fmt.Sprintf("Field %d regex assertion failed", fieldID),
				}
				result.ValidationErrors = append(result.ValidationErrors, valErr)
				continue
			}
		}
	}

	// 6. Extract fields if step succeeded
	if result.Success {
		for varName, fieldStr := range step.Extract {
			var fieldID int
			if _, err := fmt.Sscanf(fieldStr, "%d", &fieldID); err != nil {
				continue
			}
			fieldObj := respMsg.GetField(fieldID)
			if fieldObj != nil {
				val, err := fieldObj.String()
				if err == nil {
					sr.sessionState[varName] = val
				}
			}
		}
	}

	return result
}

func (sr *ScenarioRunner) injectVariables(val string) string {
	val = cardRegex.ReplaceAllStringFunc(val, func(m string) string {
		match := cardRegex.FindStringSubmatch(m)
		if len(match) > 1 {
			key := match[1]
			if v, ok := sr.activeCard[key]; ok {
				return v
			}
		}
		return m
	})
	val = contextRegex.ReplaceAllStringFunc(val, func(m string) string {
		match := contextRegex.FindStringSubmatch(m)
		if len(match) > 1 {
			key := match[1]
			if v, ok := sr.sessionState[key]; ok {
				return v
			}
		}
		return m
	})
	return val
}

func hasValue(f field.Field) bool {
	if f == nil {
		return false
	}
	b, err := f.Bytes()
	return err == nil && len(b) > 0
}
