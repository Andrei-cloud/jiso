package analyzer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	json "github.com/goccy/go-json"

	"jiso/internal/config"
	"jiso/internal/utils"
)

// VarianceResult holds the generated base transaction template and extracted dataset rows

func (ve *VarianceEngine) analyzeGeneralFlow(flow *CapturedFlow) ([]*VarianceResult, error) {
	fieldValues := make(map[int][]string)
	fieldStructuredValues := make(map[int][]interface{})
	for _, msg := range flow.Messages {
		for i, f := range msg.GetFields() {
			if f == nil || i == 1 { // Skip DE 1 (Bitmap)
				continue
			}
			val, ok := extractFieldValueForTemplate(f)
			if !ok {
				continue
			}
			val = AnonymizeFieldValue(i, val, ve.unsecure)

			if strVal, isString := val.(string); isString {
				fieldValues[i] = append(fieldValues[i], strVal)
				fieldStructuredValues[i] = append(fieldStructuredValues[i], strVal)
				continue
			}

			serialized, sErr := json.Marshal(val)
			if sErr != nil {
				continue
			}
			fieldValues[i] = append(fieldValues[i], string(serialized))
			fieldStructuredValues[i] = append(fieldStructuredValues[i], val)
		}
	}

	templateFields := make(map[string]interface{})
	varyingFieldIDs := make([]int, 0)

	// Sort field IDs for deterministic ordering
	fieldIDs := make([]int, 0, len(fieldValues))
	for fID := range fieldValues {
		fieldIDs = append(fieldIDs, fID)
	}
	sort.Ints(fieldIDs)

	for _, fieldID := range fieldIDs {
		values := fieldValues[fieldID]
		fieldKey := fmt.Sprintf("%d", fieldID)

		// System fields (DE 7, DE 11, DE 37, DE 38) map to "auto"
		if fieldID == 7 || fieldID == 11 || fieldID == 37 || fieldID == 38 {
			templateFields[fieldKey] = "auto"
			continue
		}

		// Check if field value is invariant across all messages in flow
		allSame := true
		firstVal := values[0]
		for _, v := range values[1:] {
			if v != firstVal {
				allSame = false
				break
			}
		}

		if allSame {
			firstField := flow.Messages[0].GetField(fieldID)
			extracted, ok := extractFieldValueForTemplate(firstField)
			if ok {
				templateFields[fieldKey] = AnonymizeFieldValue(fieldID, extracted, ve.unsecure)
				continue
			}

			if isNumericField(ve.spec, fieldID) && fieldID != 0 {
				if num, pErr := strconv.ParseInt(firstVal, 10, 64); pErr == nil {
					templateFields[fieldKey] = num
				} else {
					templateFields[fieldKey] = firstVal
				}
			} else {
				templateFields[fieldKey] = firstVal
			}
		} else {
			// Varying field -> add placeholder in template & extract to dataset
			if structuredValues := fieldStructuredValues[fieldID]; len(structuredValues) > 0 {
				if firstStructured, isMap := structuredValues[0].(map[string]interface{}); isMap {
					merged := make(map[string]interface{})
					for _, raw := range structuredValues {
						structuredMap, ok := raw.(map[string]interface{})
						if !ok {
							continue
						}
						merged = mergeStructuredValues(merged, structuredMap)
					}
					if len(merged) > 0 {
						templateFields[fieldKey] = buildPlaceholderValue(fmt.Sprintf("DE_%d", fieldID), merged)
					} else {
						templateFields[fieldKey] = buildPlaceholderValue(fmt.Sprintf("DE_%d", fieldID), firstStructured)
					}
				} else {
					templateFields[fieldKey] = fmt.Sprintf("{{data.DE_%d}}", fieldID)
				}
			} else {
				templateFields[fieldKey] = fmt.Sprintf("{{data.DE_%d}}", fieldID)
			}
			varyingFieldIDs = append(varyingFieldIDs, fieldID)
		}
	}

	fieldsJSON, err := json.Marshal(templateFields)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal template fields: %w", err)
	}

	var flowKey string
	if flow.DE22 != "" {
		flowKey = fmt.Sprintf("%s_%s_%s", flow.MTI, flow.DE3, flow.DE22)
	} else {
		flowKey = fmt.Sprintf("%s_%s", flow.MTI, flow.DE3)
	}

	txName := fmt.Sprintf("Captured Flow %s", flowKey)
	dsName := fmt.Sprintf("dataset_%s", flowKey)

	txItem := config.ConfigItem{
		Type:        config.TypeTransaction,
		Name:        txName,
		Description: fmt.Sprintf("Auto-generated from PCAP flow %s", flowKey),
		Fields:      fieldsJSON,
	}

	dsItem := config.ConfigItem{}

	// Only build dataset if there are varying fields
	if len(varyingFieldIDs) > 0 {
		txItem.DatasetName = dsName
		datasetRows := make([]map[string]string, len(flow.Messages))
		for msgIdx, msg := range flow.Messages {
			row := make(map[string]string)
			for _, fieldID := range varyingFieldIDs {
				if f := msg.GetField(fieldID); f != nil {
					extracted, ok := extractFieldValueForTemplate(f)
					if !ok {
						continue
					}
					extracted = AnonymizeFieldValue(fieldID, extracted, ve.unsecure)
					flattenValueForDataset(fmt.Sprintf("DE_%d", fieldID), extracted, row)
				}
			}
			datasetRows[msgIdx] = row
		}

		dsItem = config.ConfigItem{
			Type: config.TypeDataset,
			Name: dsName,
			Data: datasetRows,
		}
	} else {
		txItem.DatasetName = ""
	}

	return []*VarianceResult{
		{
			Transaction: txItem,
			Dataset:     dsItem,
		},
	}, nil
}

func (ve *VarianceEngine) AnalyzeFlowToMockRoutes(flow *CapturedFlow) ([]*VarianceResult, error) {
	if flow == nil || len(flow.Messages) == 0 {
		return nil, fmt.Errorf("flow is empty")
	}

	// Mock routes are generated ONLY for response messages (e.g. 0210, 0410, 0810)
	if !utils.IsResponseMTI(flow.MTI) {
		return nil, nil
	}

	reqMTI := utils.RequestMTI(flow.MTI)
	if reqMTI == "" {
		reqMTI = flow.MTI
	}

	matchFields := make(map[string]interface{})
	matchFields["0"] = reqMTI
	if flow.DE3 != "" {
		matchFields["3"] = ve.formatFieldValue(3, flow.DE3)
	}
	if flow.DE22 != "" {
		matchFields["22"] = ve.formatFieldValue(22, flow.DE22)
	}

	// Standard candidate echo fields in ISO8583 response flows:
	// DE 2 (PAN), DE 3 (ProcCode), DE 4 (Amount), DE 7 (DateTime), DE 11 (STAN),
	// DE 14 (Expiration), DE 22 (POS Entry Mode), DE 23 (Card Seq), DE 25 (POS Condition),
	// DE 32 (Acquiring ID), DE 33 (Forwarding ID), DE 35 (Track 2), DE 37 (RRN),
	// DE 41 (Terminal ID), DE 42 (Merchant ID), DE 43 (Merchant Name/Loc), DE 45 (Track 1),
	// DE 49 (Currency), DE 63 (Network Data), DE 70 (Network Mgmt Code), DE 115 (Trace Data)
	standardEchoIDs := []int{2, 3, 4, 7, 11, 14, 22, 23, 25, 32, 33, 35, 37, 41, 42, 43, 45, 49, 63, 70, 115}
	presentEchoMap := make(map[int]bool)
	for _, msg := range flow.Messages {
		for _, fID := range standardEchoIDs {
			if f := msg.GetField(fID); f != nil {
				if val, err := f.String(); err == nil && val != "" {
					presentEchoMap[fID] = true
				}
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

	echoSet := make(map[int]bool)
	for _, id := range echoFields {
		echoSet[id] = true
	}

	// Handle 08XX Network Management responses
	if strings.HasPrefix(flow.MTI, "08") {
		type uniqueMsg struct {
			respFields map[string]interface{}
			f70Val     string
			key        string
		}

		seenKeys := make(map[string]bool)
		uniqueList := make([]uniqueMsg, 0)

		for _, msg := range flow.Messages {
			rf := make(map[string]interface{})
			var keyParts []string
			var f70ValStr string

			var fIDs []int
			for i, f := range msg.GetFields() {
				if f == nil || i == 0 || i == 1 {
					continue
				}
				_, ok := extractFieldValueForTemplate(f)
				if !ok {
					continue
				}
				fIDs = append(fIDs, i)
			}
			sort.Ints(fIDs)

			for _, i := range fIDs {
				f := msg.GetField(i)
				if f == nil {
					continue
				}
				extracted, ok := extractFieldValueForTemplate(f)
				if !ok {
					continue
				}
				extracted = AnonymizeFieldValue(i, extracted, ve.unsecure)
				fieldKey := fmt.Sprintf("%d", i)

				if i == 70 {
					f70ValStr = fmt.Sprintf("%v", extracted)
				}

				if echoSet[i] {
					continue
				}

				if i == 38 {
					rf[fieldKey] = "auth_code"
					keyParts = append(keyParts, "38=auth_code")
				} else {
					rf[fieldKey] = extracted
					if strVal, isStr := extracted.(string); isStr {
						keyParts = append(keyParts, fmt.Sprintf("%d=%s", i, strVal))
					} else {
						jsonBytes, _ := json.Marshal(extracted)
						keyParts = append(keyParts, fmt.Sprintf("%d=%s", i, string(jsonBytes)))
					}
				}
			}

			key := strings.Join(keyParts, "|")
			if !seenKeys[key] {
				seenKeys[key] = true
				uniqueList = append(uniqueList, uniqueMsg{respFields: rf, f70Val: f70ValStr, key: key})
			}
		}

		results := make([]*VarianceResult, 0, len(uniqueList))
		for idx, u := range uniqueList {
			mf := make(map[string]interface{})
			for k, v := range matchFields {
				mf[k] = v
			}
			if u.f70Val != "" {
				mf["70"] = ve.formatFieldValue(70, u.f70Val)
			}

			txName := fmt.Sprintf("Mock Network Route %s #%d", flow.MTI, idx+1)
			txItem := config.ConfigItem{
				Type:           config.TypeMockRoute,
				Name:           txName,
				Description:    fmt.Sprintf("Auto-generated mock route for network management response MTI %s", flow.MTI),
				MatchFields:    mf,
				EchoFields:     echoFields,
				ResponseMTI:    flow.MTI,
				ResponseFields: u.respFields,
				LatencyMs:      10,
				JitterMs:       5,
			}

			results = append(results, &VarianceResult{
				Transaction: txItem,
				Dataset:     config.ConfigItem{},
			})
		}
		return results, nil
	}

	// General response flow mock route generation
	responseFields := make(map[string]interface{})
	fieldValues := make(map[int][]interface{})

	for _, msg := range flow.Messages {
		for i, f := range msg.GetFields() {
			if f == nil || i == 0 || i == 1 || echoSet[i] {
				continue
			}
			extracted, ok := extractFieldValueForTemplate(f)
			if !ok {
				continue
			}
			extracted = AnonymizeFieldValue(i, extracted, ve.unsecure)
			fieldValues[i] = append(fieldValues[i], extracted)
		}
	}

	fieldIDs := make([]int, 0, len(fieldValues))
	for fID := range fieldValues {
		fieldIDs = append(fieldIDs, fID)
	}
	sort.Ints(fieldIDs)

	for _, fieldID := range fieldIDs {
		values := fieldValues[fieldID]
		fieldKey := fmt.Sprintf("%d", fieldID)

		if fieldID == 38 {
			responseFields[fieldKey] = "auth_code"
			continue
		}

		// Use actual first captured value (never use dataset {{data.DE_X}} template placeholders in mock routes)
		responseFields[fieldKey] = values[0]
	}

	var flowKey string
	if flow.DE22 != "" {
		flowKey = fmt.Sprintf("%s_%s_%s", flow.MTI, flow.DE3, flow.DE22)
	} else {
		flowKey = fmt.Sprintf("%s_%s", flow.MTI, flow.DE3)
	}

	txName := fmt.Sprintf("Mock Route %s", flowKey)
	txItem := config.ConfigItem{
		Type:           config.TypeMockRoute,
		Name:           txName,
		Description:    fmt.Sprintf("Auto-generated mock route for response flow %s", flowKey),
		MatchFields:    matchFields,
		EchoFields:     echoFields,
		ResponseMTI:    flow.MTI,
		ResponseFields: responseFields,
		LatencyMs:      10,
		JitterMs:       5,
	}

	return []*VarianceResult{
		{
			Transaction: txItem,
			Dataset:     config.ConfigItem{},
		},
	}, nil
}
