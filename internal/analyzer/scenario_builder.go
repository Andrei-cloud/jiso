package analyzer

import (
	"fmt"
	"sort"
	"strconv"

	json "github.com/goccy/go-json"

	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
)

// ScenarioScaffoldOptions specifies parameters for scenario scaffolding
type ScenarioScaffoldOptions struct {
	ScenarioName       string
	IncludeReversals   map[int]bool // Pair index -> whether to include reversal step
	GenerateMockRoutes bool
	Unsecure           bool
}

// ScenarioScaffoldResult contains generated config items
type ScenarioScaffoldResult struct {
	Transactions []config.ConfigItem
	Datasets     []config.ConfigItem
	Scenario     config.ConfigItem
	MockRoutes   []config.ConfigItem
}

// ScenarioBuilder constructs test scenario scaffolds from correlated request-response pairs
type ScenarioBuilder struct {
	spec           *iso8583.MessageSpec
	varianceEngine *VarianceEngine
	unsecure       bool
}

// NewScenarioBuilder creates a new ScenarioBuilder instance
func NewScenarioBuilder(spec *iso8583.MessageSpec, unsecure ...bool) *ScenarioBuilder {
	unsec := false
	if len(unsecure) > 0 {
		unsec = unsecure[0]
	}
	return &ScenarioBuilder{
		spec:           spec,
		varianceEngine: NewVarianceEngine(spec, unsec),
		unsecure:       unsec,
	}
}

// Build generates transaction templates, datasets, scenario steps, and optional mock routes from correlated pairs
func (sb *ScenarioBuilder) Build(pairs []*CorrelatedPair, opts ScenarioScaffoldOptions) (*ScenarioScaffoldResult, error) {
	if len(pairs) == 0 {
		return nil, fmt.Errorf("no correlated pairs selected for scenario building")
	}

	scenName := opts.ScenarioName
	if scenName == "" {
		scenName = "Scaffolded PCAP Test Scenario"
	}

	result := &ScenarioScaffoldResult{}
	var scenarioSteps []transactions.ScenarioStep

	for idx, pair := range pairs {
		if pair == nil || pair.Request == nil || pair.Request.Message == nil {
			continue
		}

		reqMsg := pair.Request.Message
		reqMTI, _ := reqMsg.GetMTI()
		reqDE3 := getFieldString(reqMsg, 3)

		// 1. Build base transaction template for request
		txFields := make(map[string]interface{})
		var fIDs []int
		for i, f := range reqMsg.GetFields() {
			if f == nil || i == 1 { // Skip DE 1 (Bitmap)
				continue
			}
			val, ok := extractFieldValueForTemplate(f)
			if !ok {
				continue
			}
			fIDs = append(fIDs, i)
			_ = val
		}
		sort.Ints(fIDs)

		for _, i := range fIDs {
			f := reqMsg.GetField(i)
			if f == nil {
				continue
			}
			extracted, ok := extractFieldValueForTemplate(f)
			if !ok {
				continue
			}
			extracted = AnonymizeFieldValue(i, extracted, opts.Unsecure)
			fieldKey := fmt.Sprintf("%d", i)

			if i == 7 || i == 11 || i == 37 || i == 38 {
				txFields[fieldKey] = "auto"
			} else if isNumericField(sb.spec, i) && i != 0 {
				if strVal, isStr := extracted.(string); isStr {
					if num, err := strconv.ParseInt(strVal, 10, 64); err == nil {
						txFields[fieldKey] = num
					} else {
						txFields[fieldKey] = strVal
					}
				} else {
					txFields[fieldKey] = extracted
				}
			} else {
				txFields[fieldKey] = extracted
			}
		}

		txName := fmt.Sprintf("Tx %s DE3=%s #%d", reqMTI, reqDE3, idx+1)
		txFieldsBytes, err := json.Marshal(txFields)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal fields for '%s': %w", txName, err)
		}

		txItem := config.ConfigItem{
			Type:        config.TypeTransaction,
			Name:        txName,
			Description: fmt.Sprintf("Scaffolded transaction template for MTI %s DE3 %s", reqMTI, reqDE3),
			Fields:      txFieldsBytes,
		}
		result.Transactions = append(result.Transactions, txItem)

		// 2. Build Request Step
		respCode := "00"
		if pair.Response != nil && pair.Response.Message != nil {
			if rc := getFieldString(pair.Response.Message, 39); rc != "" {
				respCode = rc
			}
		}

		includeRev := opts.IncludeReversals[idx]
		stepName := fmt.Sprintf("%s DE3=%s (Step #%d)", reqMTI, reqDE3, idx+1)

		reqStep := transactions.ScenarioStep{
			Name:             stepName,
			UseTransactionId: txName,
			Validate: []transactions.Assertion{
				{
					Field:  "39",
					Expect: respCode,
				},
			},
		}

		if includeRev {
			reqStep.Extract = map[string]string{
				"AuthId":       "38",
				"OrigMTI":      "0",
				"OrigSTAN":     "11",
				"OrigDateTime": "7",
				"OrigAcquirer": "32",
				"OrigForwarder": "33",
			}
		}
		scenarioSteps = append(scenarioSteps, reqStep)

		// 3. Build Reversal Template and Step if requested
		if includeRev {
			revTxName := fmt.Sprintf("Reversal for %s DE3=%s #%d", reqMTI, reqDE3, idx+1)
			revFields := make(map[string]interface{})

			if pair.Reversal != nil && pair.Reversal.Message != nil {
				// Copy fields from captured reversal message
				revMsg := pair.Reversal.Message
				for i, f := range revMsg.GetFields() {
					if f == nil || i == 1 {
						continue
					}
					extracted, ok := extractFieldValueForTemplate(f)
					if !ok {
						continue
					}
					extracted = AnonymizeFieldValue(i, extracted, opts.Unsecure)
					fieldKey := fmt.Sprintf("%d", i)

					if i == 7 || i == 11 || i == 37 {
						revFields[fieldKey] = "auto"
					} else if i == 38 {
						revFields[fieldKey] = "{{context.AuthId}}"
					} else if i == 90 {
						revFields[fieldKey] = "{{context.OrigMTI}}{{context.OrigSTAN}}{{context.OrigDateTime}}0000000000000000000000"
					} else {
						revFields[fieldKey] = extracted
					}
				}
			} else {
				// Construct base 0400 reversal template from request fields
				for k, v := range txFields {
					revFields[k] = v
				}
				revFields["0"] = "0400"
				revFields["7"] = "auto"
				revFields["11"] = "auto"
				revFields["37"] = "auto"
				revFields["38"] = "{{context.AuthId}}"
				revFields["90"] = "{{context.OrigMTI}}{{context.OrigSTAN}}{{context.OrigDateTime}}0000000000000000000000"
			}

			revFieldsBytes, _ := json.Marshal(revFields)
			revTxItem := config.ConfigItem{
				Type:        config.TypeTransaction,
				Name:        revTxName,
				Description: fmt.Sprintf("Scaffolded reversal transaction template for MTI %s", reqMTI),
				Fields:      revFieldsBytes,
			}
			result.Transactions = append(result.Transactions, revTxItem)

			revRespCode := "00"
			if pair.ReversalResp != nil && pair.ReversalResp.Message != nil {
				if rc := getFieldString(pair.ReversalResp.Message, 39); rc != "" {
					revRespCode = rc
				}
			}

			revStep := transactions.ScenarioStep{
				Name:             fmt.Sprintf("Reversal of %s (Step #%d)", stepName, idx+1),
				UseTransactionId: revTxName,
				Validate: []transactions.Assertion{
					{
						Field:  "39",
						Expect: revRespCode,
					},
				},
			}
			scenarioSteps = append(scenarioSteps, revStep)
		}

		// 4. Generate Mock Routes if requested
		if opts.GenerateMockRoutes && pair.Response != nil && pair.Response.Message != nil {
			flow := &CapturedFlow{
				MTI:      getFieldMTI(pair.Response.Message),
				DE3:      reqDE3,
				Messages: []*iso8583.Message{pair.Response.Message},
			}
			if mrResults, err := sb.varianceEngine.AnalyzeFlowToMockRoutes(flow); err == nil {
				for _, res := range mrResults {
					if res.Transaction.ResponseFields == nil {
						res.Transaction.ResponseFields = make(map[string]interface{})
					}
					if _, has38 := res.Transaction.ResponseFields["38"]; !has38 {
						res.Transaction.ResponseFields["38"] = "auth_code"
					}
					result.MockRoutes = append(result.MockRoutes, res.Transaction)
				}
			}
		}

		if opts.GenerateMockRoutes && includeRev {
			if pair.ReversalResp != nil && pair.ReversalResp.Message != nil {
				flow := &CapturedFlow{
					MTI:      getFieldMTI(pair.ReversalResp.Message),
					DE3:      reqDE3,
					Messages: []*iso8583.Message{pair.ReversalResp.Message},
				}
				if mrResults, err := sb.varianceEngine.AnalyzeFlowToMockRoutes(flow); err == nil {
					for _, res := range mrResults {
						result.MockRoutes = append(result.MockRoutes, res.Transaction)
					}
				}
			} else {
				revReqMTI := "0400"
				if pair.Reversal != nil && pair.Reversal.Message != nil {
					if mti := getFieldMTI(pair.Reversal.Message); mti != "" {
						revReqMTI = mti
					}
				}
				revRespMTI := utils.ResponseMTI(revReqMTI)

				matchFields := map[string]interface{}{
					"0": revReqMTI,
				}
				if reqDE3 != "" {
					matchFields["3"] = sb.varianceEngine.formatFieldValue(3, reqDE3)
				}

				echoFields := []int{2, 3, 4, 7, 11, 14, 32, 33, 37, 38, 41, 42, 49, 90}

				mrName := fmt.Sprintf("Mock Reversal Route %s DE3=%s #%d", revReqMTI, reqDE3, idx+1)
				mrItem := config.ConfigItem{
					Type:        config.TypeMockRoute,
					Name:        mrName,
					Description: fmt.Sprintf("Auto-generated mock route for reversal flow MTI %s DE3 %s", revReqMTI, reqDE3),
					MatchFields: matchFields,
					EchoFields:  echoFields,
					ResponseMTI: revRespMTI,
					ResponseFields: map[string]interface{}{
						"39": "00",
					},
					LatencyMs: 10,
					JitterMs:  5,
				}
				result.MockRoutes = append(result.MockRoutes, mrItem)
			}
		}
	}

	stepsBytes, err := json.Marshal(scenarioSteps)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal scenario steps: %w", err)
	}

	result.Scenario = config.ConfigItem{
		Type:        config.TypeScenario,
		Name:        scenName,
		Description: fmt.Sprintf("Scaffolded test scenario containing %d step(s) extracted from PCAP", len(scenarioSteps)),
		Steps:       stepsBytes,
	}

	return result, nil
}
