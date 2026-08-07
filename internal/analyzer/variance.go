package analyzer

import (
	"fmt"
	"strconv"
	"strings"

	"jiso/internal/config"

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

func (ve *VarianceEngine) formatFieldValue(fieldID int, val string) interface{} {
	if isNumericField(ve.spec, fieldID) && fieldID != 0 {
		if num, err := strconv.ParseInt(val, 10, 64); err == nil {
			return num
		}
	}
	return val
}

// AnalyzeFlowToMockRoutes generates Mock Server Route items from a captured response flow
