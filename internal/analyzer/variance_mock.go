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
				templateFields[fieldKey] = extracted
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

	// Standard ISO8583 response echo fields:
	// DE 7 (DateTime), DE 11 (STAN), DE 25 (POS Condition), DE 32 (Acquiring Inst ID),
	// DE 37 (RRN), DE 41 (Terminal ID), DE 42 (Merchant ID), DE 63 (Network ID), DE 115 (Trace Data)
	echoFields := []int{7, 11, 25, 32, 37, 41, 42, 63, 115}
	echoSet := make(map[int]bool)
	for _, id := range echoFields {
		echoSet[id] = true
	}

	// Handle 08XX Network Management responses
	if strings.HasPrefix(flow.MTI, "08") {
		type uniqueMsg struct {
			respFields map[string]string
			key        string
		}

		seenKeys := make(map[string]bool)
		uniqueList := make([]uniqueMsg, 0)

		for _, msg := range flow.Messages {
			rf := make(map[string]string)
			var keyParts []string

			var fIDs []int
			for i, f := range msg.GetFields() {
				if f == nil || i == 0 || i == 1 || echoSet[i] {
					continue
				}
				val, err := f.String()
				if err != nil || val == "" {
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
				val, err := f.String()
				if err != nil || val == "" {
					continue
				}
				fieldKey := fmt.Sprintf("%d", i)

				if i == 38 {
					rf[fieldKey] = "auth_code"
					keyParts = append(keyParts, "38=auth_code")
				} else {
					rf[fieldKey] = val
					keyParts = append(keyParts, fmt.Sprintf("%d=%s", i, val))
				}
			}

			key := strings.Join(keyParts, "|")
			if !seenKeys[key] {
				seenKeys[key] = true
				uniqueList = append(uniqueList, uniqueMsg{respFields: rf, key: key})
			}
		}

		results := make([]*VarianceResult, 0, len(uniqueList))
		for idx, u := range uniqueList {
			mf := make(map[string]interface{})
			for k, v := range matchFields {
				mf[k] = v
			}
			if f70Val, ok := u.respFields["70"]; ok && f70Val != "" {
				mf["70"] = ve.formatFieldValue(70, f70Val)
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
	responseFields := make(map[string]string)
	fieldValues := make(map[int][]string)

	for _, msg := range flow.Messages {
		for i, f := range msg.GetFields() {
			if f == nil || i == 0 || i == 1 || echoSet[i] {
				continue
			}
			val, err := f.String()
			if err != nil || val == "" {
				continue
			}
			fieldValues[i] = append(fieldValues[i], val)
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
