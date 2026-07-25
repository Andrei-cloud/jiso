package analyzer

import (
	json "github.com/goccy/go-json"
	"fmt"

	"jiso/internal/config"
)

// VarianceResult holds the generated base transaction template and extracted dataset rows
type VarianceResult struct {
	Transaction config.ConfigItem
	Dataset     config.ConfigItem
}

// VarianceEngine performs variance analysis on captured message flows
type VarianceEngine struct{}

// NewVarianceEngine creates a new VarianceEngine instance
func NewVarianceEngine() *VarianceEngine {
	return &VarianceEngine{}
}

// AnalyzeFlow inspects messages in a CapturedFlow and generates a base transaction template + dataset
func (ve *VarianceEngine) AnalyzeFlow(flow *CapturedFlow) (*VarianceResult, error) {
	if flow == nil || len(flow.Messages) == 0 {
		return nil, fmt.Errorf("flow is empty")
	}

	fieldValues := make(map[int][]string)
	for _, msg := range flow.Messages {
		for i, f := range msg.GetFields() {
			if f == nil {
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

	for fieldID, values := range fieldValues {
		fieldKey := fmt.Sprintf("%d", fieldID)

		// System fields (DE 7, DE 11, DE 37) map to "auto"
		if fieldID == 7 || fieldID == 11 || fieldID == 37 {
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
			templateFields[fieldKey] = firstVal
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

	txName := fmt.Sprintf("Captured Flow %s_%s", flow.MTI, flow.DE3)
	dsName := fmt.Sprintf("dataset_%s_%s", flow.MTI, flow.DE3)

	txItem := config.ConfigItem{
		Type:        config.TypeTransaction,
		Name:        txName,
		Description: fmt.Sprintf("Auto-generated from PCAP flow MTI %s DE3 %s", flow.MTI, flow.DE3),
		Fields:      fieldsJSON,
		DatasetName: dsName,
	}

	// Build dataset matrix rows
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

	dsItem := config.ConfigItem{
		Type: config.TypeDataset,
		Name: dsName,
		Data: datasetRows,
	}

	return &VarianceResult{
		Transaction: txItem,
		Dataset:     dsItem,
	}, nil
}
