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

func (ve *VarianceEngine) analyzeNetworkManagementFlow(flow *CapturedFlow) ([]*VarianceResult, error) {
	// Deduplicate unique 08XX messages based on field content
	type uniqueMsg struct {
		fields map[string]any
		key    string
	}

	seenKeys := make(map[string]bool)
	uniqueList := make([]uniqueMsg, 0)
	for _, msg := range flow.Messages {
		tf, key := ve.networkMessageTemplate(msg)
		if !seenKeys[key] {
			seenKeys[key] = true
			uniqueList = append(uniqueList, uniqueMsg{fields: tf, key: key})
		}
	}

	results := make([]*VarianceResult, 0, len(uniqueList))
	for idx, u := range uniqueList {
		fieldsJSON, err := json.Marshal(u.fields)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal 08XX fields: %w", err)
		}

		txName := fmt.Sprintf("Captured Network %s #%d", flow.MTI, idx+1)
		txItem := config.Item{
			Type:        config.TypeTransaction,
			Name:        txName,
			Description: fmt.Sprintf("Auto-generated network management transaction MTI %s", flow.MTI),
			Fields:      fieldsJSON,
			DatasetName: "",
		}
		results = append(results, &VarianceResult{Transaction: txItem, Dataset: config.Item{}})
	}

	return results, nil
}

// networkMessageTemplate builds a 08XX message's template fields and a stable
// key for de-duplication: responder-generated fields (7/11/37/38) map to "auto",
// composites are carried through, numeric fields parsed, the rest kept as
// captured.
func (ve *VarianceEngine) networkMessageTemplate(msg *iso8583.Message) (tf map[string]any, key string) {
	tf = make(map[string]any)
	var keyParts []string

	// Sort field IDs for deterministic key fingerprinting
	var fIDs []int
	for i, f := range msg.GetFields() {
		if f == nil || i == 1 { // Skip DE 1 (Bitmap)
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
		extracted, ok := extractFieldValueForTemplate(f)
		if !ok {
			continue
		}
		extracted = AnonymizeFieldValue(i, extracted, ve.unsecure)

		val := ""
		if asString, isString := extracted.(string); isString {
			val = asString
		} else if serialized, sErr := json.Marshal(extracted); sErr == nil {
			val = string(serialized)
		}
		if val == "" {
			continue
		}
		fieldKey := fmt.Sprintf("%d", i)

		if i == 7 || i == 11 || i == 37 || i == 38 {
			tf[fieldKey] = utils.KeywordAuto
			keyParts = append(keyParts, fmt.Sprintf("%d=auto", i))

			continue
		}
		tf[fieldKey] = ve.networkFieldValue(extracted, val, i)
		keyParts = append(keyParts, fmt.Sprintf("%d=%s", i, val))
	}

	return tf, strings.Join(keyParts, "|")
}

// networkFieldValue resolves one non-auto template field: a composite is carried
// through, a numeric field is parsed, the rest kept as the captured string.
func (ve *VarianceEngine) networkFieldValue(extracted any, val string, i int) any {
	if extractedMap, isMap := extracted.(map[string]any); isMap {
		return extractedMap
	}
	if isNumericField(ve.spec, i) && i != 0 {
		if num, pErr := strconv.ParseInt(val, 10, 64); pErr == nil {
			return num
		}
	}

	return val
}
