package analyzer

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

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
	anonymizer     *Anonymizer
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
		anonymizer:     NewAnonymizer(unsec),
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

	anon := sb.anonymizer
	if anon == nil || opts.Unsecure != sb.unsecure {
		anon = NewAnonymizer(opts.Unsecure)
	}

	result := &ScenarioScaffoldResult{}
	var scenarioSteps []transactions.ScenarioStep

	type mockRouteAccumulator struct {
		baseMatchFields map[string]interface{}
		echoFields      []int
		responseMTI     string
		responseFields  map[string]interface{}
		reqMTI          string
		reqDE3          string
		respDE39        string
		cards           []string
		seenCards       map[string]bool
		count           int
	}

	type routeGroup struct {
		groupKey     string
		reqMTI       string
		reqDE3       string
		responseMTI  string
		accumulators []*mockRouteAccumulator
	}

	primaryGroupMap := make(map[string]*routeGroup)
	var primaryGroupOrder []string

	reversalGroupMap := make(map[string]*routeGroup)
	var reversalGroupOrder []string

	for idx, pair := range pairs {
		if pair == nil || pair.Request == nil || pair.Request.Message == nil || pair.Response == nil || pair.Response.Message == nil {
			continue
		}

		reqMsg := pair.Request.Message
		reqMTI, _ := reqMsg.GetMTI()
		reqDE3 := FormatProcCode(getFieldString(reqMsg, 3))
		if reqDE3 == "" {
			reqDE3 = "000000"
		}

		// 1. Build base transaction template for request
		txFields := buildMessageTemplateFields(reqMsg, sb.spec, opts.Unsecure, anon)

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
					extracted = anon.AnonymizeFieldValue(i, extracted)
					fieldKey := fmt.Sprintf("%d", i)

					if i == 7 || i == 11 || i == 37 {
						revFields[fieldKey] = "auto"
					} else if i == 38 {
						revFields[fieldKey] = "{{context.AuthId}}"
					} else if i == 90 {
						revFields[fieldKey] = "{{context.OrigMTI}}{{context.OrigSTAN}}{{context.OrigDateTime}}0000000000000000000000"
					} else if i == 3 {
						revFields[fieldKey] = FormatProcCode(fmt.Sprintf("%v", extracted))
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

		// 4. Collect Primary Mock Routes if requested
		if opts.GenerateMockRoutes && pair.Response != nil && pair.Response.Message != nil {
			reqDE2 := getFieldString(reqMsg, 2)
			respMTI, _ := pair.Response.Message.GetMTI()
			if respMTI == "" {
				respMTI = utils.ResponseMTI(reqMTI)
			}
			respDE39 := getFieldString(pair.Response.Message, 39)
			if respDE39 == "" {
				respDE39 = "00"
			}

			baseMatch := map[string]interface{}{
				"0": reqMTI,
			}
			if reqDE3 != "" {
				baseMatch["3"] = reqDE3
			}
			if reqDE70 := getFieldString(reqMsg, 70); reqDE70 != "" {
				baseMatch["70"] = reqDE70
			}
			if reqDE22 := getFieldString(reqMsg, 22); reqDE22 != "" {
				baseMatch["22"] = reqDE22
			}
			if reqDE25 := getFieldString(reqMsg, 25); reqDE25 != "" {
				baseMatch["25"] = reqDE25
			}

			echoFields, responseFields := extractEchoAndResponseFields(reqMsg, pair.Response.Message, opts.Unsecure, anon)

			sigBytes, _ := json.Marshal([]interface{}{baseMatch, respMTI, responseFields, echoFields})
			sig := string(sigBytes)

			cardVal := ""
			if reqDE2 != "" {
				cardVal = anon.AnonymizePAN(reqDE2)
			}

			groupKey := fmt.Sprintf("%s_%s", reqMTI, reqDE3)
			grp, exists := primaryGroupMap[groupKey]
			if !exists {
				grp = &routeGroup{
					groupKey:    groupKey,
					reqMTI:      reqMTI,
					reqDE3:      reqDE3,
					responseMTI: respMTI,
				}
				primaryGroupMap[groupKey] = grp
				primaryGroupOrder = append(primaryGroupOrder, groupKey)
			}

			var acc *mockRouteAccumulator
			for _, existingAcc := range grp.accumulators {
				existSigBytes, _ := json.Marshal([]interface{}{existingAcc.baseMatchFields, existingAcc.responseMTI, existingAcc.responseFields, existingAcc.echoFields})
				if string(existSigBytes) == sig {
					acc = existingAcc
					break
				}
			}

			if acc == nil {
				acc = &mockRouteAccumulator{
					baseMatchFields: baseMatch,
					echoFields:      echoFields,
					responseMTI:     respMTI,
					responseFields:  responseFields,
					reqMTI:          reqMTI,
					reqDE3:          reqDE3,
					respDE39:        respDE39,
					cards:           make([]string, 0),
					seenCards:       make(map[string]bool),
					count:           0,
				}
				grp.accumulators = append(grp.accumulators, acc)
			}

			acc.count++
			if cardVal != "" && !acc.seenCards[cardVal] {
				acc.seenCards[cardVal] = true
				acc.cards = append(acc.cards, cardVal)
			}
		}

		// 5. Collect Reversal Mock Routes if requested
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
				revDE3 = FormatProcCode(getFieldString(pair.Reversal.Message, 3))
				revDE2 = getFieldString(pair.Reversal.Message, 2)
			}
			if revDE3 == "" && pair.Request != nil && pair.Request.Message != nil {
				revDE3 = FormatProcCode(getFieldString(pair.Request.Message, 3))
			}
			if revDE2 == "" && pair.Request != nil && pair.Request.Message != nil {
				revDE2 = getFieldString(pair.Request.Message, 2)
			}
			if revDE3 == "" {
				revDE3 = "000000"
			}

			baseMatch := map[string]interface{}{
				"0": revReqMTI,
			}
			if revDE3 != "" {
				baseMatch["3"] = revDE3
			}

			var echoFields []int
			var responseFields map[string]interface{}
			respRC := "00"

			if pair.ReversalResp != nil && pair.ReversalResp.Message != nil {
				if mti, _ := pair.ReversalResp.Message.GetMTI(); mti != "" {
					revRespMTI = mti
				}
				if rc := getFieldString(pair.ReversalResp.Message, 39); rc != "" {
					respRC = rc
				}
				var revReqMsg *iso8583.Message
				if pair.Reversal != nil && pair.Reversal.Message != nil {
					revReqMsg = pair.Reversal.Message
				} else {
					revReqMsg = pair.Request.Message
				}
				echoFields, responseFields = extractEchoAndResponseFields(revReqMsg, pair.ReversalResp.Message, opts.Unsecure, anon)
			} else {
				echoFields = []int{2, 3, 4, 7, 11, 14, 22, 25, 32, 33, 37, 38, 41, 42, 49, 90}
				responseFields = map[string]interface{}{
					"39": "00",
				}
			}

			sigBytes, _ := json.Marshal([]interface{}{baseMatch, revRespMTI, responseFields, echoFields})
			sig := string(sigBytes)

			cardVal := ""
			if revDE2 != "" {
				cardVal = anon.AnonymizePAN(revDE2)
			}

			groupKey := fmt.Sprintf("%s_%s", revReqMTI, revDE3)
			grp, exists := reversalGroupMap[groupKey]
			if !exists {
				grp = &routeGroup{
					groupKey:    groupKey,
					reqMTI:      revReqMTI,
					reqDE3:      revDE3,
					responseMTI: revRespMTI,
				}
				reversalGroupMap[groupKey] = grp
				reversalGroupOrder = append(reversalGroupOrder, groupKey)
			}

			var acc *mockRouteAccumulator
			for _, existingAcc := range grp.accumulators {
				existSigBytes, _ := json.Marshal([]interface{}{existingAcc.baseMatchFields, existingAcc.responseMTI, existingAcc.responseFields, existingAcc.echoFields})
				if string(existSigBytes) == sig {
					acc = existingAcc
					break
				}
			}

			if acc == nil {
				acc = &mockRouteAccumulator{
					baseMatchFields: baseMatch,
					echoFields:      echoFields,
					responseMTI:     revRespMTI,
					responseFields:  responseFields,
					reqMTI:          revReqMTI,
					reqDE3:          revDE3,
					respDE39:        respRC,
					cards:           make([]string, 0),
					seenCards:       make(map[string]bool),
					count:           0,
				}
				grp.accumulators = append(grp.accumulators, acc)
			}

			acc.count++
			if cardVal != "" && !acc.seenCards[cardVal] {
				acc.seenCards[cardVal] = true
				acc.cards = append(acc.cards, cardVal)
			}
		}
	}

	// Process Primary Mock Routes with Smart Reusability & Specificity
	for _, gKey := range primaryGroupOrder {
		grp := primaryGroupMap[gKey]
		if grp == nil || len(grp.accumulators) == 0 {
			continue
		}

		if len(grp.accumulators) == 1 {
			// Single behavior -> clean generic route (no card filter needed)
			acc := grp.accumulators[0]
			mf := make(map[string]interface{})
			for k, v := range acc.baseMatchFields {
				mf[k] = v
			}

			mrName := fmt.Sprintf("Mock Route %s DE3=%s", acc.responseMTI, acc.reqDE3)
			if acc.respDE39 != "" && acc.respDE39 != "00" {
				mrName = fmt.Sprintf("Mock Route %s DE3=%s RC=%s", acc.responseMTI, acc.reqDE3, acc.respDE39)
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
		} else {
			// Multiple behaviors -> Sort accumulators: "00" first, then by frequency
			sort.SliceStable(grp.accumulators, func(i, j int) bool {
				if grp.accumulators[i].respDE39 == "00" && grp.accumulators[j].respDE39 != "00" {
					return true
				}
				if grp.accumulators[i].respDE39 != "00" && grp.accumulators[j].respDE39 == "00" {
					return false
				}
				return grp.accumulators[i].count > grp.accumulators[j].count
			})

			for idx, acc := range grp.accumulators {
				mf := make(map[string]interface{})
				for k, v := range acc.baseMatchFields {
					mf[k] = v
				}
				if len(acc.cards) == 1 {
					mf["2"] = acc.cards[0]
				} else if len(acc.cards) > 1 {
					mf["2"] = acc.cards
				}

				var mrName string
				if acc.respDE39 != "" {
					mrName = fmt.Sprintf("Mock Route %s DE3=%s RC=%s #%d", acc.responseMTI, acc.reqDE3, acc.respDE39, idx+1)
				} else {
					mrName = fmt.Sprintf("Mock Route %s DE3=%s #%d", acc.responseMTI, acc.reqDE3, idx+1)
				}

				mrItem := config.ConfigItem{
					Type:           config.TypeMockRoute,
					Name:           mrName,
					Description:    fmt.Sprintf("Auto-generated mock route for response flow %s DE3 %s (RC: %s)", acc.responseMTI, acc.reqDE3, acc.respDE39),
					MatchFields:    mf,
					EchoFields:     acc.echoFields,
					ResponseMTI:    acc.responseMTI,
					ResponseFields: acc.responseFields,
					LatencyMs:      10,
					JitterMs:       5,
				}
				result.MockRoutes = append(result.MockRoutes, mrItem)
			}
		}
	}

	// Process Reversal Mock Routes
	for _, gKey := range reversalGroupOrder {
		grp := reversalGroupMap[gKey]
		if grp == nil || len(grp.accumulators) == 0 {
			continue
		}

		if len(grp.accumulators) == 1 {
			acc := grp.accumulators[0]
			mf := make(map[string]interface{})
			for k, v := range acc.baseMatchFields {
				mf[k] = v
			}

			revReqMTI := fmt.Sprintf("%v", acc.baseMatchFields["0"])
			mrName := fmt.Sprintf("Mock Reversal Route %s DE3=%s", revReqMTI, acc.reqDE3)
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
		} else {
			for idx, acc := range grp.accumulators {
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

func extractEchoAndResponseFields(reqMsg, respMsg *iso8583.Message, unsecure bool, anon ...*Anonymizer) ([]int, map[string]interface{}) {
	responseFields := make(map[string]interface{})
	if respMsg == nil {
		return nil, responseFields
	}

	var a *Anonymizer
	if len(anon) > 0 && anon[0] != nil {
		a = anon[0]
	} else {
		a = NewAnonymizer(unsecure)
	}

	echoSet := make(map[int]bool)
	var echoFields []int

	if reqMsg != nil {
		for i := 2; i <= 128; i++ {
			reqF := reqMsg.GetField(i)
			respF := respMsg.GetField(i)

			if reqF == nil || respF == nil {
				continue
			}

			reqVal, ok1 := extractFieldValueForTemplate(reqF)
			respVal, ok2 := extractFieldValueForTemplate(respF)

			if !ok1 || !ok2 {
				continue
			}

			if valuesEqual(reqVal, respVal) {
				echoFields = append(echoFields, i)
				echoSet[i] = true
			}
		}
	}
	sort.Ints(echoFields)

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
		extracted = a.AnonymizeFieldValue(i, extracted)
		fieldKey := fmt.Sprintf("%d", i)

		if i == 38 {
			responseFields[fieldKey] = "auth_code"
		} else if i == 11 && !echoSet[11] {
			responseFields[fieldKey] = "stan"
		} else if i == 37 && !echoSet[37] {
			responseFields[fieldKey] = "rrn"
		} else if i == 7 && !echoSet[7] {
			responseFields[fieldKey] = "datetime"
		} else {
			responseFields[fieldKey] = extracted
		}
	}

	if rc := getFieldString(respMsg, 39); rc != "" && !echoSet[39] {
		responseFields["39"] = rc
	}
	if f38 := respMsg.GetField(38); f38 != nil && !echoSet[38] {
		if _, has38 := responseFields["38"]; !has38 {
			responseFields["38"] = "auth_code"
		}
	}

	return echoFields, responseFields
}

func valuesEqual(v1, v2 interface{}) bool {
	if v1 == nil && v2 == nil {
		return true
	}
	if v1 == nil || v2 == nil {
		return false
	}
	s1 := fmt.Sprintf("%v", v1)
	s2 := fmt.Sprintf("%v", v2)
	if s1 == s2 || strings.TrimSpace(s1) == strings.TrimSpace(s2) {
		return true
	}
	b1, err1 := json.Marshal(v1)
	b2, err2 := json.Marshal(v2)
	if err1 == nil && err2 == nil && bytes.Equal(b1, b2) {
		return true
	}
	return false
}
