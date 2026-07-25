package analyzer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"jiso/internal/config"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"
)

// VarianceResult holds the generated base transaction template and extracted dataset rows
type VarianceResult struct {
	Transaction config.ConfigItem
	Dataset     config.ConfigItem
}

// VarianceEngine performs variance analysis on captured message flows
type VarianceEngine struct {
	spec *iso8583.MessageSpec
}

// NewVarianceEngine creates a new VarianceEngine instance
func NewVarianceEngine(spec ...*iso8583.MessageSpec) *VarianceEngine {
	ve := &VarianceEngine{}
	if len(spec) > 0 && spec[0] != nil {
		ve.spec = spec[0]
	}
	return ve
}

func isNumericField(spec *iso8583.MessageSpec, fieldID int) bool {
	if spec == nil || spec.Fields == nil {
		return false
	}
	f, exists := spec.Fields[fieldID]
	if !exists || f == nil {
		return false
	}
	_, ok := f.(*field.Numeric)
	return ok
}

// AnalyzeFlow inspects messages in a CapturedFlow and generates base transaction template(s) + dataset
func (ve *VarianceEngine) AnalyzeFlow(flow *CapturedFlow) ([]*VarianceResult, error) {
	if flow == nil || len(flow.Messages) == 0 {
		return nil, fmt.Errorf("flow is empty")
	}

	// Handle Network Management Messages (08XX MTI)
	if strings.HasPrefix(flow.MTI, "08") {
		return ve.analyzeNetworkManagementFlow(flow)
	}

	return ve.analyzeGeneralFlow(flow)
}

func (ve *VarianceEngine) analyzeNetworkManagementFlow(flow *CapturedFlow) ([]*VarianceResult, error) {
	// Deduplicate unique 08XX messages based on field content
	type uniqueMsg struct {
		fields map[string]interface{}
		key    string
	}

	seenKeys := make(map[string]bool)
	uniqueList := make([]uniqueMsg, 0)

	for _, msg := range flow.Messages {
		tf := make(map[string]interface{})
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
			val, err := f.String()
			if err != nil || val == "" {
				continue
			}
			fieldKey := fmt.Sprintf("%d", i)

			if i == 7 || i == 11 || i == 37 || i == 38 {
				tf[fieldKey] = "auto"
				keyParts = append(keyParts, fmt.Sprintf("%d=auto", i))
			} else {
				if isNumericField(ve.spec, i) && i != 0 {
					if num, pErr := strconv.ParseInt(val, 10, 64); pErr == nil {
						tf[fieldKey] = num
					} else {
						tf[fieldKey] = val
					}
				} else {
					tf[fieldKey] = val
				}
				keyParts = append(keyParts, fmt.Sprintf("%d=%s", i, val))
			}
		}

		key := strings.Join(keyParts, "|")
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
		txItem := config.ConfigItem{
			Type:        config.TypeTransaction,
			Name:        txName,
			Description: fmt.Sprintf("Auto-generated network management transaction MTI %s", flow.MTI),
			Fields:      fieldsJSON,
			DatasetName: "",
		}

		results = append(results, &VarianceResult{
			Transaction: txItem,
			Dataset:     config.ConfigItem{},
		})
	}

	return results, nil
}

func (ve *VarianceEngine) analyzeGeneralFlow(flow *CapturedFlow) ([]*VarianceResult, error) {
	fieldValues := make(map[int][]string)
	for _, msg := range flow.Messages {
		for i, f := range msg.GetFields() {
			if f == nil || i == 1 { // Skip DE 1 (Bitmap)
				continue
			}
			val, err := f.String()
			if err != nil || val == "" {
				continue
			}
			fieldValues[i] = append(fieldValues[i], val)
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
			templateFields[fieldKey] = fmt.Sprintf("{{data.DE_%d}}", fieldID)
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
					if val, err := f.String(); err == nil {
						row[fmt.Sprintf("DE_%d", fieldID)] = val
					}
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
