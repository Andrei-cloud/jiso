package transactions

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"

	"jiso/internal/config"
	"jiso/internal/db"
	"jiso/internal/utils"
)

var (
	dataRegex    = regexp.MustCompile(`\{\{\s*data\.(\w+)\s*\}\}`)
	cardRegex    = regexp.MustCompile(`\{\{\s*card\.(\w+)\s*\}\}`)
	contextRegex = regexp.MustCompile(`\{\{\s*context\.(\w+)\s*\}\}`)
)

func (sr *ScenarioRunner) runStep(step ScenarioStep, scenarioDatasetName string) StepResult {
	datasetName := sr.resolveStepDataset(step, scenarioDatasetName)

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
	if step.UseTransactionID != "" {
		var err error
		msg, err = sr.tc.ComposeRaw(step.UseTransactionID)
		if err != nil {
			result.Success = false
			result.Error = fmt.Errorf("failed to compose template '%s': %w", step.UseTransactionID, err).Error()
			return result
		}
	} else {
		msg = iso8583.NewMessage(sr.svc.GetSpec())
	}

	// 2-3. Merge template + step fields, interpolate variables and resolve auto
	// fields into a fresh request message.
	reqMsg := sr.buildStepRequest(msg, step, datasetName)

	// Prove the request packs BEFORE touching the network:
	// a template value that does not fit the loaded spec (length/prefix
	// mismatch) used to be swallowed here, and the identical error
	// resurfaced from Send as a misleading "network send failed" while
	// the mock server was running and the client connected. Failing the
	// step with the true cause also populates the reporting payload.
	packed, packErr := sr.packStepRequest(reqMsg)
	if packErr != nil {
		return sr.failStep(result, step, reqMsg, packErr)
	}
	result.RequestPayload = packed

	// 4. Send over network and wait for response
	startTime := time.Now()
	respMsg, err := sr.svc.Send(reqMsg)
	result.LatencyMs = time.Since(startTime).Milliseconds()

	if err != nil {
		return sr.failStep(result, step, reqMsg, fmt.Errorf("network send failed: %w", err))
	}

	if respMsg == nil {
		return sr.failStep(result, step, reqMsg, errors.New("received empty response"))
	}

	// Populate Response Payload for reporting
	if respPacked, err := respMsg.Pack(); err == nil {
		result.ResponsePayload = string(respPacked)
	}

	// 5. Assert validation rules
	sr.runAssertions(respMsg, step, datasetName, &result)

	// 6. Extract fields if response message is available
	sr.extractStepFields(respMsg, step)

	// 7. Log transaction to database and in-memory collection
	sr.logStepToDB(step, reqMsg, respMsg, int(result.LatencyMs), result.Success)

	return result
}

// failStep marks the result failed with cause, logs the step row with
// no response, and returns the result — the three failure legs (pack,
// send error, empty response) shared these statements.
func (sr *ScenarioRunner) failStep(result StepResult, step ScenarioStep, req *iso8583.Message, cause error) StepResult {
	result.Success = false
	result.Error = cause.Error()
	sr.logStepToDB(step, req, nil, int(result.LatencyMs), false)

	return result
}

// packStepRequest packs a step's request message and returns it as the
// reporting payload. A pack error means the composed message does not
// fit the LOADED SPEC (a length/prefix mismatch, not a network fault),
// so the error names the spec — the message that claimed
// "network send failed" while the mock server ran and the client was
// connected was exactly this local pack failure.
func (sr *ScenarioRunner) packStepRequest(reqMsg *iso8583.Message) (string, error) {
	packed, err := reqMsg.Pack()
	if err == nil {
		return string(packed), nil
	}
	specName := ""
	if spec := sr.svc.GetSpec(); spec != nil {
		specName = spec.Name
	}

	return "", fmt.Errorf("message cannot be packed with spec %q: %w", specName, err)
}

// buildStepRequest merges the base template's fields with the step overrides,
// interpolates variables and resolves auto fields, and applies them to a fresh
// request message.
func (sr *ScenarioRunner) buildStepRequest(msg *iso8583.Message, step ScenarioStep, datasetName string) *iso8583.Message {
	// Build the combined fields map to support step overrides
	mergedFields := make(map[string]any)

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

	// Interpolate variables, resolve auto fields, and apply to request message
	for k, v := range mergedFields {
		// Atoi is strict: Sscanf("%d") would silently accept "12abc" as
		// field 12.
		fieldID, err := strconv.Atoi(k)
		if err != nil {
			continue
		}

		switch val := v.(type) {
		case string:
			if isReservedAutoKeywordString(val) {
				sr.tc.handleAutoFieldsWithKeyword(fieldID, reqMsg, val)
			} else {
				interpolated := sr.injectVariables(val, datasetName)
				_ = reqMsg.Field(fieldID, interpolated)
			}
		case float64:
			// Match the composer's float handling: %.0f rounds .5 values
			// to even and corrupts money fields like 100.5.
			if val == math.Trunc(val) {
				_ = reqMsg.Field(fieldID, strconv.FormatInt(int64(val), 10))
			} else {
				_ = reqMsg.Field(fieldID, strconv.FormatFloat(val, 'f', -1, 64))
			}
		case int:
			_ = reqMsg.Field(fieldID, strconv.Itoa(val))
		default:
			_ = reqMsg.Field(fieldID, fmt.Sprintf("%v", val))
		}
	}

	return reqMsg
}

// runAssertions evaluates the step's validation rules against the response,
// recording any failures on result.
func (sr *ScenarioRunner) runAssertions(respMsg *iso8583.Message, step ScenarioStep, datasetName string, result *StepResult) {
	for _, assertion := range step.Validate {
		sr.checkAssertion(respMsg, assertion, datasetName, result)
	}
}

// checkAssertion evaluates one assertion against the response field, recording a
// validation failure (and stopping further checks for this assertion) when it does
// not hold.
func (sr *ScenarioRunner) checkAssertion(respMsg *iso8583.Message, assertion Assertion, datasetName string, result *StepResult) {
	var fieldID int
	if _, err := fmt.Sscanf(assertion.Field, "%d", &fieldID); err != nil {
		result.addValidationFailure(ValidationError{
			Field:   assertion.Field,
			Message: fmt.Sprintf("invalid field format: %s", assertion.Field),
		})

		return
	}

	fieldObj := respMsg.GetField(fieldID)

	// Check existence assertion
	if assertion.Exists != nil {
		exists := hasValue(fieldObj)
		if exists != *assertion.Exists {
			result.addValidationFailure(ValidationError{
				Field:    assertion.Field,
				Expected: fmt.Sprintf("exists=%t", *assertion.Exists),
				Actual:   fmt.Sprintf("exists=%t", exists),
				Message:  fmt.Sprintf("Field %d existence assertion failed", fieldID),
			})

			return
		}
	}

	if fieldObj == nil {
		if assertion.Expect != "" || assertion.Regex != "" {
			result.addValidationFailure(ValidationError{
				Field:    assertion.Field,
				Expected: fmt.Sprintf("expect=%s regex=%s", assertion.Expect, assertion.Regex),
				Actual:   "nil",
				Message:  fmt.Sprintf("Field %d does not exist in response", fieldID),
			})
		}

		return
	}

	actualValue, err := fieldObj.String()
	if err != nil {
		result.addValidationFailure(ValidationError{
			Field:   assertion.Field,
			Message: fmt.Sprintf("failed to get string value of field %d: %v", fieldID, err),
		})

		return
	}

	// Exact match assertion
	if assertion.Expect != "" {
		expectedInterp := sr.injectVariables(assertion.Expect, datasetName)
		if actualValue != expectedInterp {
			result.addValidationFailure(ValidationError{
				Field:    assertion.Field,
				Expected: expectedInterp,
				Actual:   actualValue,
				Message:  fmt.Sprintf("Field %d exact match assertion failed", fieldID),
			})

			return
		}
	}

	// Regex match assertion
	if assertion.Regex != "" {
		sr.checkRegexAssertion(assertion, fieldID, actualValue, datasetName, result)
	}
}

// checkRegexAssertion applies the step's regex (interpolated with dataset and
// context variables) to the response field value, recording a failure when it does
// not match or the pattern is invalid.
func (sr *ScenarioRunner) checkRegexAssertion(assertion Assertion, fieldID int, actualValue, datasetName string, result *StepResult) {
	regexInterp := sr.injectVariables(assertion.Regex, datasetName)
	re, err := regexp.Compile(regexInterp)
	if err != nil {
		result.addValidationFailure(ValidationError{
			Field:   assertion.Field,
			Message: fmt.Sprintf("failed to compile regex '%s': %v", regexInterp, err),
		})

		return
	}

	if !re.MatchString(actualValue) {
		result.addValidationFailure(ValidationError{
			Field:    assertion.Field,
			Expected: fmt.Sprintf("regex(%s)", regexInterp),
			Actual:   actualValue,
			Message:  fmt.Sprintf("Field %d regex assertion failed", fieldID),
		})
	}
}

// extractStepFields copies the step's requested fields from the response into the
// runner's session state for later {{context.*}} interpolation.
func (sr *ScenarioRunner) extractStepFields(respMsg *iso8583.Message, step ScenarioStep) {
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

func (sr *ScenarioRunner) logStepToDB(step ScenarioStep, req, resp *iso8583.Message, processingTimeMs int, success bool) {
	cfg := config.GetConfig()
	if cfg.GetDbPath() == "" {
		return
	}

	sessionID := cfg.GetSessionID()
	if sessionID == "" {
		return
	}

	specPath := cfg.GetSpec()
	specName := filepath.Base(specPath)
	if specPath == "" {
		specName = ""
	}
	txFilePath := cfg.GetFile()
	txFileName := filepath.Base(txFilePath)
	if txFilePath == "" {
		txFileName = ""
	}

	var spec *iso8583.MessageSpec
	if sr.tc != nil {
		spec = sr.tc.spec
	}
	if spec == nil && sr.svc != nil {
		spec = sr.svc.GetSpec()
	}

	var reqJSON string
	var reqRawHex string
	if req != nil {
		reqJSON, reqRawHex = messageJSONOrHex(req, spec)
	}

	var respJSON *string
	var respRawHex *string
	var responseCode string
	if resp != nil {
		responseCode = responseCodeOf(resp)

		jsonStr, hexStr := messageJSONOrHex(resp, spec)
		if jsonStr != "" {
			respJSON = &jsonStr
		}
		if hexStr != "" {
			respRawHex = &hexStr
		}
	}

	txName := step.Name
	if step.UseTransactionID != "" {
		txName = step.UseTransactionID
	}

	db.LogTransactionEnriched(&db.TransactionRecord{
		SessionID:        sessionID,
		TxName:           txName,
		TxFilePath:       txFilePath,
		TxFileName:       txFileName,
		SpecPath:         specPath,
		SpecName:         specName,
		RequestJSON:      reqJSON,
		ResponseJSON:     respJSON,
		RequestRawHEX:    reqRawHex,
		ResponseRawHEX:   respRawHex,
		ResponseCode:     responseCode,
		ProcessingTimeMs: processingTimeMs,
		Success:          success,
	})

	if sr.tc != nil && step.UseTransactionID != "" {
		sr.tc.LogTransaction(step.UseTransactionID, success)
	}
}

// messageJSONOrHex renders a message as JSON via the spec, falling back to a hex
// dump of the packed bytes; exactly one of the two results is non-empty.
func messageJSONOrHex(msg *iso8583.Message, spec *iso8583.MessageSpec) (jsonStr, hexStr string) {
	if j, err := db.MessageToJSONWithSpec(msg, spec); err == nil && j != "" {
		return j, ""
	}

	if packed, err := msg.Pack(); err == nil {
		return "", utils.HexDump(packed)
	}

	return "", ""
}

// responseCodeOf returns the response message's field 39, or "" when absent.
func responseCodeOf(msg *iso8583.Message) string {
	if f := msg.GetField(39); f != nil {
		if s, err := f.String(); err == nil {
			return s
		}
	}

	return ""
}

func hasValue(f field.Field) bool {
	if f == nil {
		return false
	}
	b, err := f.Bytes()
	return err == nil && len(b) > 0
}
