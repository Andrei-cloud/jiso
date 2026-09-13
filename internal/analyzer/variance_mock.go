package analyzer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"

	"jiso/internal/config"
	"jiso/internal/utils"
)

func (ve *VarianceEngine) analyzeGeneralFlow(flow *CapturedFlow) ([]*VarianceResult, error) {
	fieldValues, fieldStructuredValues := ve.collectFieldValues(flow)

	templateFields := make(map[string]any)
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
			templateFields[fieldKey] = utils.KeywordAuto
			continue
		}

		// A field constant across every message in the flow carries its captured
		// value; a field that varies gets a dataset placeholder and is collected
		// into the generated dataset.
		if valuesInvariant(values) {
			templateFields[fieldKey] = ve.invariantValue(flow, fieldID, values[0])

			continue
		}
		templateFields[fieldKey] = varyingValue(fieldID, fieldStructuredValues[fieldID])
		varyingFieldIDs = append(varyingFieldIDs, fieldID)
	}

	fieldsJSON, err := json.Marshal(templateFields)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal template fields: %w", err)
	}

	flowKey := flowKeyOf(flow)

	txName := fmt.Sprintf("Captured Flow %s", flowKey)
	dsName := fmt.Sprintf("dataset_%s", flowKey)

	txItem := config.Item{
		Type:        config.TypeTransaction,
		Name:        txName,
		Description: fmt.Sprintf("Auto-generated from PCAP flow %s", flowKey),
		Fields:      fieldsJSON,
	}

	dsItem := config.Item{}
	if len(varyingFieldIDs) == 0 {
		txItem.DatasetName = ""

		return []*VarianceResult{{Transaction: txItem, Dataset: dsItem}}, nil
	}
	txItem.DatasetName = dsName
	dsItem = config.Item{
		Type: config.TypeDataset,
		Name: dsName,
		Data: ve.buildFlowDataset(flow, varyingFieldIDs),
	}

	return []*VarianceResult{{Transaction: txItem, Dataset: dsItem}}, nil
}

// AnalyzeFlowToMockRoutes turns one captured response flow into mock-route config
// items: it derives the request match fields and the present echo fields, then
// dispatches 08XX network-management flows to networkManagementMockRoutes and
// every other response flow to generalMockRoutes.
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

	matchFields := map[string]any{"0": reqMTI}
	if flow.DE3 != "" {
		matchFields["3"] = ve.formatFieldValue(3, flow.DE3)
	}
	if flow.DE22 != "" {
		matchFields["22"] = ve.formatFieldValue(22, flow.DE22)
	}

	echoFields := presentEchoFields(flow)
	echoSet := make(map[int]bool, len(echoFields))
	for _, id := range echoFields {
		echoSet[id] = true
	}

	// 08XX network-management responses have a response shape of their own.
	if strings.HasPrefix(flow.MTI, "08") {
		return ve.networkManagementMockRoutes(flow, matchFields, echoFields, echoSet), nil
	}

	return ve.generalMockRoutes(flow, matchFields, echoFields, echoSet), nil
}

// presentEchoFields returns the sorted echo-capable fields actually present in
// the flow's messages, falling back to a standard set when none were captured.
func presentEchoFields(flow *CapturedFlow) []int {
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

	return echoFields
}

// networkManagementMockRoutes builds mock routes for 08XX network-management
// responses: each distinct response shape (keyed by its non-echo fields, with
// DE 38 emitted as the auth-code keyword and DE 70 promoted into the match)
// becomes one route.
func (ve *VarianceEngine) networkManagementMockRoutes(flow *CapturedFlow, matchFields map[string]any, echoFields []int, echoSet map[int]bool) []*VarianceResult {
	type uniqueMsg struct {
		respFields map[string]any
		f70Val     string
		key        string
	}

	seenKeys := make(map[string]bool)
	uniqueList := make([]uniqueMsg, 0)

	for _, msg := range flow.Messages {
		rf, key, f70Val := ve.messageResponseShape(msg, echoSet, true)
		if !seenKeys[key] {
			seenKeys[key] = true
			uniqueList = append(uniqueList, uniqueMsg{respFields: rf, f70Val: f70Val, key: key})
		}
	}

	results := make([]*VarianceResult, 0, len(uniqueList))
	for idx, u := range uniqueList {
		mf := make(map[string]any)
		for k, v := range matchFields {
			mf[k] = v
		}
		if u.f70Val != "" {
			mf["70"] = ve.formatFieldValue(70, u.f70Val)
		}

		txName := fmt.Sprintf("Mock Network Route %s #%d", flow.MTI, idx+1)
		txItem := config.Item{
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
			Dataset:     config.Item{},
		})
	}
	return results
}

// generalMockRoutes builds mock routes for a general response flow: messages
// sharing a response shape (their non-echo fields) group together and collect
// distinct cards, and each group becomes one route keyed by response code.
func (ve *VarianceEngine) generalMockRoutes(flow *CapturedFlow, matchFields map[string]any, echoFields []int, echoSet map[int]bool) []*VarianceResult {
	// General response flow mock route generation
	type uniqueRespGroup struct {
		responseFields map[string]any
		cards          []string
		seenCards      map[string]bool
		key            string
	}

	seenGroupKeys := make(map[string]int)
	var groups []*uniqueRespGroup

	for _, msg := range flow.Messages {
		rf, groupKey, _ := ve.messageResponseShape(msg, echoSet, false)
		cardVal := ""
		if f2 := msg.GetField(2); f2 != nil {
			if s, err := f2.String(); err == nil && s != "" {
				cardVal = ve.anonymizer.AnonymizePAN(s)
			}
		}

		if idx, found := seenGroupKeys[groupKey]; found {
			grp := groups[idx]
			if cardVal != "" && !grp.seenCards[cardVal] {
				grp.seenCards[cardVal] = true
				grp.cards = append(grp.cards, cardVal)
			}
		} else {
			grp := &uniqueRespGroup{
				responseFields: rf,
				cards:          nil,
				seenCards:      make(map[string]bool),
				key:            groupKey,
			}
			if cardVal != "" {
				grp.seenCards[cardVal] = true
				grp.cards = append(grp.cards, cardVal)
			}
			seenGroupKeys[groupKey] = len(groups)
			groups = append(groups, grp)
		}
	}

	flowKey := flowKeyOf(flow)

	results := make([]*VarianceResult, 0, len(groups))
	for idx, grp := range groups {
		mf := generalRouteMatchFields(matchFields, grp.cards, len(groups))
		txName := generalRouteName(flowKey, grp.responseFields, idx, len(groups))

		txItem := config.Item{
			Type:           config.TypeMockRoute,
			Name:           txName,
			Description:    fmt.Sprintf("Auto-generated mock route for response flow %s", flowKey),
			MatchFields:    mf,
			EchoFields:     echoFields,
			ResponseMTI:    flow.MTI,
			ResponseFields: grp.responseFields,
			LatencyMs:      10,
			JitterMs:       5,
		}

		results = append(results, &VarianceResult{
			Transaction: txItem,
			Dataset:     config.Item{},
		})
	}

	return results
}

// valuesInvariant reports whether every captured value for one field is equal.
func valuesInvariant(values []string) bool {
	firstVal := values[0]
	for _, v := range values[1:] {
		if v != firstVal {
			return false
		}
	}

	return true
}

// invariantValue resolves a field that is constant across the flow to its
// template value: the anonymized structured capture when it extracts, else the
// first value parsed as a number for a numeric field, else the raw first value.
func (ve *VarianceEngine) invariantValue(flow *CapturedFlow, fieldID int, firstVal string) any {
	firstField := flow.Messages[0].GetField(fieldID)
	if extracted, ok := extractFieldValueForTemplate(firstField); ok {
		return ve.anonymizer.AnonymizeFieldValue(fieldID, extracted)
	}
	if isNumericField(ve.spec, fieldID) && fieldID != 0 {
		if num, pErr := strconv.ParseInt(firstVal, 10, 64); pErr == nil {
			return num
		}
	}

	return firstVal
}

// varyingValue resolves a field that varies across the flow to its template
// placeholder: a merged composite placeholder when the structured values are
// maps, else a flat data reference keyed by the field number.
func varyingValue(fieldID int, structuredValues []any) any {
	fieldKey := fmt.Sprintf("DE_%d", fieldID)
	if len(structuredValues) == 0 {
		return fmt.Sprintf("{{data.DE_%d}}", fieldID)
	}
	firstStructured, isMap := structuredValues[0].(map[string]any)
	if !isMap {
		return fmt.Sprintf("{{data.DE_%d}}", fieldID)
	}
	merged := make(map[string]any)
	for _, raw := range structuredValues {
		if structuredMap, ok := raw.(map[string]any); ok {
			merged = mergeStructuredValues(merged, structuredMap)
		}
	}
	if len(merged) > 0 {
		return buildPlaceholderValue(fieldKey, merged)
	}

	return buildPlaceholderValue(fieldKey, firstStructured)
}

// collectFieldValues gathers each field's captured values across the flow's
// messages: a string view (for the invariant check) and a structured view (the
// raw value for a composite, its string for a string).
func (ve *VarianceEngine) collectFieldValues(flow *CapturedFlow) (map[int][]string, map[int][]any) {
	fieldValues := make(map[int][]string)
	fieldStructuredValues := make(map[int][]any)
	for _, msg := range flow.Messages {
		for i, f := range msg.GetFields() {
			if f == nil || i == 1 { // Skip DE 1 (Bitmap)
				continue
			}
			val, ok := extractFieldValueForTemplate(f)
			if !ok {
				continue
			}
			val = ve.anonymizer.AnonymizeFieldValue(i, val)

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

	return fieldValues, fieldStructuredValues
}

// flowKeyOf names a flow by MTI, DE3, and -- when present -- DE22.
func flowKeyOf(flow *CapturedFlow) string {
	if flow.DE22 != "" {
		return fmt.Sprintf("%s_%s_%s", flow.MTI, flow.DE3, flow.DE22)
	}

	return fmt.Sprintf("%s_%s", flow.MTI, flow.DE3)
}

// buildFlowDataset collects each message's varying fields into one dataset row,
// anonymized and flattened by DE key.
func (ve *VarianceEngine) buildFlowDataset(flow *CapturedFlow, varyingFieldIDs []int) []map[string]string {
	rows := make([]map[string]string, len(flow.Messages))
	for msgIdx, msg := range flow.Messages {
		row := make(map[string]string)
		for _, fieldID := range varyingFieldIDs {
			f := msg.GetField(fieldID)
			if f == nil {
				continue
			}
			extracted, ok := extractFieldValueForTemplate(f)
			if !ok {
				continue
			}
			extracted = ve.anonymizer.AnonymizeFieldValue(fieldID, extracted)
			flattenValueForDataset(fmt.Sprintf("DE_%d", fieldID), extracted, row)
		}
		rows[msgIdx] = row
	}

	return rows
}

// messageResponseShape collects a message's non-echo response fields into rf
// (DE 38 emitted as the auth-code keyword, the rest as their anonymized capture)
// and returns a stable key built from them for de-duplication. wantF70 also
// captures field 70, which a network-management route promotes into its match.
func (ve *VarianceEngine) messageResponseShape(msg *iso8583.Message, echoSet map[int]bool, wantF70 bool) (rf map[string]any, key, f70Val string) {
	rf = make(map[string]any)
	var keyParts []string

	var fIDs []int
	for i, f := range msg.GetFields() {
		if f == nil || i == 0 || i == 1 {
			continue
		}
		if _, ok := extractFieldValueForTemplate(f); !ok {
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
		extracted = ve.anonymizer.AnonymizeFieldValue(i, extracted)

		if wantF70 && i == 70 {
			f70Val = fmt.Sprintf("%v", extracted)
		}
		if echoSet[i] {
			continue
		}

		fieldKey := fmt.Sprintf("%d", i)
		if i == 38 {
			rf[fieldKey] = utils.KeywordAuthCode
			keyParts = append(keyParts, "38=auth_code")

			continue
		}
		rf[fieldKey] = extracted
		if strVal, isStr := extracted.(string); isStr {
			keyParts = append(keyParts, fmt.Sprintf("%d=%s", i, strVal))
		} else {
			jsonBytes, _ := json.Marshal(extracted)
			keyParts = append(keyParts, fmt.Sprintf("%d=%s", i, string(jsonBytes)))
		}
	}

	return rf, strings.Join(keyParts, "|"), f70Val
}

// generalRouteMatchFields copies the request match fields and adds the group's
// card as the DE 2 match when a lone card disambiguates the group, or the card
// set when a group collected several.
func generalRouteMatchFields(matchFields map[string]any, cards []string, groupCount int) map[string]any {
	mf := make(map[string]any, len(matchFields))
	for k, v := range matchFields {
		mf[k] = v
	}
	if len(cards) == 1 && groupCount > 1 {
		mf["2"] = cards[0]
	} else if len(cards) > 1 {
		mf["2"] = cards
	}

	return mf
}

// generalRouteName names a general mock route, keying by response code and index
// when a flow split into several groups.
func generalRouteName(flowKey string, responseFields map[string]any, idx, groupCount int) string {
	if groupCount <= 1 {
		return fmt.Sprintf("Mock Route %s", flowKey)
	}
	if rc, ok := responseFields["39"].(string); ok && rc != "" {
		return fmt.Sprintf("Mock Route %s RC=%s #%d", flowKey, rc, idx+1)
	}

	return fmt.Sprintf("Mock Route %s #%d", flowKey, idx+1)
}
