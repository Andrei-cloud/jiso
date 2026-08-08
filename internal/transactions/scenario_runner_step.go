package transactions

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"
)

var (
	dataRegex    = regexp.MustCompile(`\{\{\s*data\.(\w+)\s*\}\}`)
	cardRegex    = regexp.MustCompile(`\{\{\s*card\.(\w+)\s*\}\}`)
	contextRegex = regexp.MustCompile(`\{\{\s*context\.(\w+)\s*\}\}`)
)

func (sr *ScenarioRunner) runStep(step ScenarioStep, scenarioDatasetName string) StepResult {
	// Re-initialize selectedDatasets map on every step run to ensure random selection per step
	sr.selectedDatasets = make(map[string]map[string]string)

	// Resolve dataset name for this step
	var datasetName string
	if step.UseTransactionId != "" {
		if t, err := sr.tc.findTransaction(step.UseTransactionId); err == nil {
			datasetName = t.DatasetName
		}
	}
	if datasetName == "" {
		datasetName = scenarioDatasetName
	}
	if datasetName == "" && len(sr.tc.datasets) > 0 {
		if _, ok := sr.tc.datasets["card_pool"]; ok {
			datasetName = "card_pool"
		} else {
			for name := range sr.tc.datasets {
				datasetName = name
				break
			}
		}
	}

	if datasetName != "" {
		dataset, err := sr.tc.GetDataset(datasetName)
		if err == nil && len(dataset.Data) > 0 {
			randomIndex := rand.Intn(len(dataset.Data))
			sr.selectedDatasets[datasetName] = dataset.Data[randomIndex]
		}
	}

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
		msg, err = sr.tc.ComposeRaw(step.UseTransactionId)
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
			if isReservedAutoKeywordString(val) {
				sr.tc.handleAutoFieldsWithKeyword(fieldID, reqMsg, val)
			} else {
				interpolated := sr.injectVariables(val, datasetName)
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
			expectedInterp := sr.injectVariables(assertion.Expect, datasetName)
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
			regexInterp := sr.injectVariables(assertion.Regex, datasetName)
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

func (sr *ScenarioRunner) injectVariables(val string, datasetName string) string {
	replaceDatasetVar := func(m string, match []string) string {
		if len(match) > 1 {
			key := match[1]

			// Check if we already have selected an item for this dataset
			selectedRow, ok := sr.selectedDatasets[datasetName]
			if !ok && datasetName != "" {
				// Retrieve dataset and choose a random row
				dataset, err := sr.tc.GetDataset(datasetName)
				if err == nil && len(dataset.Data) > 0 {
					randomIndex := rand.Intn(len(dataset.Data))
					selectedRow = dataset.Data[randomIndex]
					sr.selectedDatasets[datasetName] = selectedRow
					ok = true
				}
			}

			if ok {
				if v, exist := selectedRow[key]; exist {
					return v
				}
			}
		}
		return m
	}

	val = dataRegex.ReplaceAllStringFunc(val, func(m string) string {
		return replaceDatasetVar(m, dataRegex.FindStringSubmatch(m))
	})
	val = cardRegex.ReplaceAllStringFunc(val, func(m string) string {
		return replaceDatasetVar(m, cardRegex.FindStringSubmatch(m))
	})
	val = contextRegex.ReplaceAllStringFunc(val, func(m string) string {
		match := contextRegex.FindStringSubmatch(m)
		if len(match) > 1 {
			key := match[1]
			if v, ok := sr.sessionState[key]; ok && strings.TrimSpace(v) != "" {
				return v
			}
			switch key {
			case "AuthId", "auth_code":
				return "000000"
			case "OrigMTI":
				return "0100"
			case "OrigSTAN":
				return "000001"
			case "OrigDateTime":
				return time.Now().Format("0102150405")
			case "OrigAcquirer", "OrigForwarder":
				return "000000"
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
