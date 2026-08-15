package analyzer

import (
	"fmt"
	"sort"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"

	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"
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

	type mockRouteAccumulator struct {
		baseMatchFields map[string]interface{}
		echoFields      []int
		responseMTI     string
		responseFields  map[string]interface{}
		reqDE3          string
		respDE39        string
		cards           []string
		seenCards       map[string]bool
	}

	var primaryRoutes []*mockRouteAccumulator
	primaryRouteMap := make(map[string]*mockRouteAccumulator)

	var reversalRoutes []*mockRouteAccumulator
	reversalRouteMap := make(map[string]*mockRouteAccumulator)

	for idx, pair := range pairs {
		if pair == nil || pair.Request == nil || pair.Request.Message == nil {
			continue
		}

		reqMsg := pair.Request.Message
		reqMTI, _ := reqMsg.GetMTI()
		reqDE3 := getFieldString(reqMsg, 3)

		// 1. Build base transaction template for request
		txFields := buildMessageTemplateFields(reqMsg, sb.spec, opts.Unsecure)

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
				"AuthId":        "38",
				"OrigMTI":       "0",
				"OrigSTAN":      "11",
				"OrigDateTime":  "7",
				"OrigAcquirer":  "32",
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

		// 4. Collect Mock Routes if requested
		if opts.GenerateMockRoutes && pair.Response != nil && pair.Response.Message != nil {
			reqDE2 := getFieldString(reqMsg, 2)
			respMTI, _ := pair.Response.Message.GetMTI()
			if respMTI == "" {
				respMTI = utils.ResponseMTI(reqMTI)
			}
			respDE39 := getFieldString(pair.Response.Message, 39)

			baseMatch := map[string]interface{}{
				"0": reqMTI,
			}
			if reqDE3 != "" {
				baseMatch["3"] = sb.varianceEngine.formatFieldValue(3, reqDE3)
			}
			if reqDE70 := getFieldString(reqMsg, 70); reqDE70 != "" {
				baseMatch["70"] = sb.varianceEngine.formatFieldValue(70, reqDE70)
			}
			if reqDE22 := getFieldString(reqMsg, 22); reqDE22 != "" {
				baseMatch["22"] = sb.varianceEngine.formatFieldValue(22, reqDE22)
			}
			if reqDE25 := getFieldString(reqMsg, 25); reqDE25 != "" {
				baseMatch["25"] = sb.varianceEngine.formatFieldValue(25, reqDE25)
			}

			echoFields, echoSet := extractEchoFields(reqMsg)
			responseFields := extractResponseFields(pair.Response.Message, echoSet, respDE39, opts.Unsecure)

			sigBytes, _ := json.Marshal([]interface{}{baseMatch, respMTI, responseFields, echoFields})
			sig := string(sigBytes)

			cardVal := ""
			if reqDE2 != "" {
				cardVal = fmt.Sprintf("%v", AnonymizeFieldValue(2, reqDE2, opts.Unsecure))
			}

			acc, exists := primaryRouteMap[sig]
			if !exists {
				acc = &mockRouteAccumulator{
					baseMatchFields: baseMatch,
					echoFields:      echoFields,
					responseMTI:     respMTI,
					responseFields:  responseFields,
					reqDE3:          reqDE3,
					respDE39:        respDE39,
					cards:           make([]string, 0),
					seenCards:       make(map[string]bool),
				}
				primaryRouteMap[sig] = acc
				primaryRoutes = append(primaryRoutes, acc)
			}

			if cardVal != "" && !acc.seenCards[cardVal] {
				acc.seenCards[cardVal] = true
				acc.cards = append(acc.cards, cardVal)
			}
		}

		if opts.GenerateMockRoutes && includeRev {
			revReqMTI := "0400"
			if pair.Reversal != nil && pair.Reversal.Message != nil {
				if mti, _ := pair.Reversal.Message.GetMTI(); mti != "" {
					revReqMTI = mti
				}
			}
			revRespMTI := utils.ResponseMTI(revReqMTI)
			if revRespMTI == "" {
				revRespMTI = "0410"
			}

			revDE3 := ""
			revDE2 := ""
			if pair.Reversal != nil && pair.Reversal.Message != nil {
				revDE3 = getFieldString(pair.Reversal.Message, 3)
				revDE2 = getFieldString(pair.Reversal.Message, 2)
			}
			if revDE3 == "" && pair.Request != nil && pair.Request.Message != nil {
				revDE3 = getFieldString(pair.Request.Message, 3)
			}
			if revDE2 == "" && pair.Request != nil && pair.Request.Message != nil {
				revDE2 = getFieldString(pair.Request.Message, 2)
			}

			baseMatch := map[string]interface{}{
				"0": revReqMTI,
			}
			if revDE3 != "" {
				baseMatch["3"] = sb.varianceEngine.formatFieldValue(3, revDE3)
			}

			echoFields := []int{2, 3, 4, 7, 11, 14, 22, 25, 32, 33, 37, 38, 41, 42, 49, 90}
			echoSet := make(map[int]bool, len(echoFields))
			for _, id := range echoFields {
				echoSet[id] = true
			}

			responseFields := map[string]interface{}{
				"39": "00",
			}

			respRC := "00"
			if pair.ReversalResp != nil && pair.ReversalResp.Message != nil {
				respMsg := pair.ReversalResp.Message
				if mti, _ := respMsg.GetMTI(); mti != "" {
					revRespMTI = mti
				}
				for i, f := range respMsg.GetFields() {
					if f == nil || i == 0 || i == 1 || echoSet[i] {
						continue
					}
					extracted, ok := extractFieldValueForTemplate(f)
					if !ok {
						continue
					}
					extracted = AnonymizeFieldValue(i, extracted, opts.Unsecure)
					responseFields[fmt.Sprintf("%d", i)] = extracted
				}
				if rc := getFieldString(respMsg, 39); rc != "" {
					responseFields["39"] = rc
					respRC = rc
				}
			}

			sigBytes, _ := json.Marshal([]interface{}{baseMatch, revRespMTI, responseFields, echoFields})
			sig := string(sigBytes)

			cardVal := ""
			if revDE2 != "" {
				cardVal = fmt.Sprintf("%v", AnonymizeFieldValue(2, revDE2, opts.Unsecure))
			}

			acc, exists := reversalRouteMap[sig]
			if !exists {
				acc = &mockRouteAccumulator{
					baseMatchFields: baseMatch,
					echoFields:      echoFields,
					responseMTI:     revRespMTI,
					responseFields:  responseFields,
					reqDE3:          revDE3,
					respDE39:        respRC,
					cards:           make([]string, 0),
					seenCards:       make(map[string]bool),
				}
				reversalRouteMap[sig] = acc
				reversalRoutes = append(reversalRoutes, acc)
			}

			if cardVal != "" && !acc.seenCards[cardVal] {
				acc.seenCards[cardVal] = true
				acc.cards = append(acc.cards, cardVal)
			}
		}
	}

	for idx, acc := range primaryRoutes {
		mf := make(map[string]interface{})
		for k, v := range acc.baseMatchFields {
			mf[k] = v
		}
		if len(acc.cards) == 1 {
			mf["2"] = acc.cards[0]
		} else if len(acc.cards) > 1 {
			mf["2"] = acc.cards
		}

		mrName := fmt.Sprintf("Mock Route %s DE3=%s", acc.responseMTI, acc.reqDE3)
		if acc.respDE39 != "" {
			mrName = fmt.Sprintf("Mock Route %s DE3=%s RC=%s", acc.responseMTI, acc.reqDE3, acc.respDE39)
		}
		if len(primaryRoutes) > 1 {
			mrName = fmt.Sprintf("%s #%d", mrName, idx+1)
		}

		mrItem := config.ConfigItem{
			Type:           config.TypeMockRoute,
			Name:           mrName,
			Description:    fmt.Sprintf("Auto-generated mock route for response flow %s DE3 %s", acc.responseMTI, acc.reqDE3),
			MatchFields:    mf,
			EchoFields:     acc.echoFields,
			ResponseMTI:    acc.responseMTI,
			ResponseFields: acc.responseFields,
			LatencyMs:      10,
			JitterMs:       5,
		}
		result.MockRoutes = append(result.MockRoutes, mrItem)
	}

	for idx, acc := range reversalRoutes {
		mf := make(map[string]interface{})
		for k, v := range acc.baseMatchFields {
			mf[k] = v
		}
		if len(acc.cards) == 1 {
			mf["2"] = acc.cards[0]
		} else if len(acc.cards) > 1 {
			mf["2"] = acc.cards
		}

		revReqMTI := fmt.Sprintf("%v", acc.baseMatchFields["0"])
		mrName := fmt.Sprintf("Mock Reversal Route %s DE3=%s #%d", revReqMTI, acc.reqDE3, idx+1)
		mrItem := config.ConfigItem{
			Type:           config.TypeMockRoute,
			Name:           mrName,
			Description:    fmt.Sprintf("Auto-generated mock route for reversal flow MTI %s DE3 %s", revReqMTI, acc.reqDE3),
			MatchFields:    mf,
			EchoFields:     acc.echoFields,
			ResponseMTI:    acc.responseMTI,
			ResponseFields: acc.responseFields,
			LatencyMs:      10,
			JitterMs:       5,
		}
		result.MockRoutes = append(result.MockRoutes, mrItem)
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

func extractEchoFields(reqMsg *iso8583.Message) ([]int, map[int]bool) {
	standardEchoIDs := []int{2, 3, 4, 7, 11, 14, 22, 23, 25, 32, 33, 35, 37, 41, 42, 43, 45, 49, 63, 70, 90, 115}
	presentEchoMap := make(map[int]bool)
	for _, fID := range standardEchoIDs {
		if f := reqMsg.GetField(fID); f != nil {
			if val, err := f.String(); err == nil && val != "" {
				presentEchoMap[fID] = true
			}
		}
	}
	echoFields := make([]int, 0, len(presentEchoMap))
	for fID := range presentEchoMap {
		echoFields = append(echoFields, fID)
	}
	sort.Ints(echoFields)
	if len(echoFields) == 0 {
		echoFields = []int{7, 11, 25, 32, 37, 41, 42, 63, 115}
	}

	echoSet := make(map[int]bool, len(echoFields))
	for _, id := range echoFields {
		echoSet[id] = true
	}
	return echoFields, echoSet
}

func extractResponseFields(respMsg *iso8583.Message, echoSet map[int]bool, respDE39 string, unsecure bool) map[string]interface{} {
	responseFields := make(map[string]interface{})
	var respFIDs []int
	for i, f := range respMsg.GetFields() {
		if f == nil || i == 0 || i == 1 || echoSet[i] {
			continue
		}
		respFIDs = append(respFIDs, i)
	}
	sort.Ints(respFIDs)

	for _, i := range respFIDs {
		f := respMsg.GetField(i)
		if f == nil {
			continue
		}
		extracted, ok := extractFieldValueForTemplate(f)
		if !ok {
			continue
		}
		extracted = AnonymizeFieldValue(i, extracted, unsecure)
		fieldKey := fmt.Sprintf("%d", i)

		if i == 38 {
			responseFields[fieldKey] = "auth_code"
		} else {
			responseFields[fieldKey] = extracted
		}
	}

	if _, has39 := responseFields["39"]; !has39 && respDE39 != "" {
		responseFields["39"] = respDE39
	}
	if _, has38 := responseFields["38"]; !has38 {
		responseFields["38"] = "auth_code"
	}
	return responseFields
}


