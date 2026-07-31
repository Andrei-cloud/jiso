package analyzer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"jiso/internal/config"

	json "github.com/goccy/go-json"
)

// VarianceResult holds the generated base transaction template and extracted dataset rows

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

