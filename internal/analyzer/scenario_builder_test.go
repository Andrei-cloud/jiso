package analyzer

import (
	"testing"

	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"

	json "github.com/goccy/go-json"

	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScenarioBuilder_BasicScaffold(t *testing.T) {
	spec := utils.GetDefaultSpec()

	rawPAN := "4532012345678912" // 16 digit PAN
	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0200")
	reqMsg.Field(2, rawPAN)
	reqMsg.Field(3, "000000")
	reqMsg.Field(4, "1000")
	reqMsg.Field(11, "000001")

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0210")
	respMsg.Field(3, "000000")
	respMsg.Field(11, "000001")
	respMsg.Field(38, "AUTH01")
	respMsg.Field(39, "00")

	pair := &CorrelatedPair{
		Request:  &AnnotatedMessage{Message: reqMsg, Direction: DirectionRequest},
		Response: &AnnotatedMessage{Message: respMsg, Direction: DirectionResponse},
		Label:    "Test Pair",
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:     "Test Scaffold Scenario",
		IncludeReversals: map[int]bool{0: false},
	}

	result, err := builder.Build([]*CorrelatedPair{pair}, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify generated Transaction template
	require.Len(t, result.Transactions, 1)
	tx := result.Transactions[0]
	assert.Equal(t, config.TypeTransaction, tx.Type)

	var txFields map[string]interface{}
	err = json.Unmarshal(tx.Fields, &txFields)
	require.NoError(t, err)

	// Check PAN anonymization: BIN prefix '45320123' preserved, positions 14-16 '000'
	anonymizedPAN, ok := txFields["2"].(string)
	require.True(t, ok)
	assert.Equal(t, 16, len(anonymizedPAN))
	assert.Equal(t, "45320123", anonymizedPAN[:8])
	assert.Equal(t, "000", anonymizedPAN[13:16])
	assert.NotEqual(t, rawPAN, anonymizedPAN)

	// Verify System Fields set to "auto"
	assert.Equal(t, "auto", txFields["11"])

	// Verify generated Scenario ConfigItem
	assert.Equal(t, config.TypeScenario, result.Scenario.Type)
	assert.Equal(t, "Test Scaffold Scenario", result.Scenario.Name)

	var steps []transactions.ScenarioStep
	err = json.Unmarshal(result.Scenario.Steps, &steps)
	require.NoError(t, err)
	require.Len(t, steps, 1)

	assert.Equal(t, tx.Name, steps[0].UseTransactionId)
	require.Len(t, steps[0].Validate, 1)
	assert.Equal(t, "39", steps[0].Validate[0].Field)
	assert.Equal(t, "00", steps[0].Validate[0].Expect)
}

func TestScenarioBuilder_ReversalStep(t *testing.T) {
	spec := utils.GetDefaultSpec()

	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0200")
	reqMsg.Field(2, "4111111111111111")
	reqMsg.Field(3, "000000")
	reqMsg.Field(11, "000001")

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0210")
	respMsg.Field(3, "000000")
	respMsg.Field(11, "000001")
	respMsg.Field(38, "AUTH01")
	respMsg.Field(39, "00")

	pair := &CorrelatedPair{
		Request:  &AnnotatedMessage{Message: reqMsg, Direction: DirectionRequest},
		Response: &AnnotatedMessage{Message: respMsg, Direction: DirectionResponse},
		Label:    "Test Pair",
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:     "Reversal Test Scenario",
		IncludeReversals: map[int]bool{0: true},
	}

	result, err := builder.Build([]*CorrelatedPair{pair}, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// 2 transaction templates: Request + Reversal
	require.Len(t, result.Transactions, 2)

	// Scenario should have 2 steps: Request + Reversal
	var steps []transactions.ScenarioStep
	err = json.Unmarshal(result.Scenario.Steps, &steps)
	require.NoError(t, err)
	require.Len(t, steps, 2)

	// First step must extract context variables needed for reversal
	assert.NotNil(t, steps[0].Extract)
	assert.Equal(t, "38", steps[0].Extract["AuthId"])
	assert.Equal(t, "11", steps[0].Extract["OrigSTAN"])
	assert.Equal(t, "7", steps[0].Extract["OrigDateTime"])

	// Second step is reversal
	assert.Contains(t, steps[1].Name, "Reversal")

	// Verify Reversal Transaction template fields
	revTx := result.Transactions[1]
	var revFields map[string]interface{}
	err = json.Unmarshal(revTx.Fields, &revFields)
	require.NoError(t, err)

	assert.Equal(t, "0400", revFields["0"])
	assert.Equal(t, "{{context.AuthId}}", revFields["38"])
	assert.Contains(t, revFields["90"], "{{context.OrigMTI}}{{context.OrigSTAN}}{{context.OrigDateTime}}")
}

func TestScenarioBuilder_MockRoutes(t *testing.T) {
	spec := utils.GetDefaultSpec()

	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0200")
	reqMsg.Field(3, "000000")
	reqMsg.Field(11, "000001")

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0210")
	respMsg.Field(3, "000000")
	respMsg.Field(11, "000001")
	respMsg.Field(38, "AUTH01")
	respMsg.Field(39, "00")

	pair := &CorrelatedPair{
		Request:  &AnnotatedMessage{Message: reqMsg, Direction: DirectionRequest},
		Response: &AnnotatedMessage{Message: respMsg, Direction: DirectionResponse},
		Label:    "Test Pair",
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:       "Mock Route Test Scenario",
		GenerateMockRoutes: true,
	}

	result, err := builder.Build([]*CorrelatedPair{pair}, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.MockRoutes, 1)
	mr := result.MockRoutes[0]
	assert.Equal(t, config.TypeMockRoute, mr.Type)
	assert.Equal(t, "0210", mr.ResponseMTI)
}

func TestScenarioBuilder_MockRoutesWithReversal(t *testing.T) {
	spec := utils.GetDefaultSpec()

	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0100")
	reqMsg.Field(3, "000000")
	reqMsg.Field(11, "000001")

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0110")
	respMsg.Field(3, "000000")
	respMsg.Field(11, "000001")
	respMsg.Field(38, "AUTH01")
	respMsg.Field(39, "00")

	pair := &CorrelatedPair{
		Request:  &AnnotatedMessage{Message: reqMsg, Direction: DirectionRequest},
		Response: &AnnotatedMessage{Message: respMsg, Direction: DirectionResponse},
		Label:    "Test Pair",
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:       "Mock Route Reversal Scenario",
		IncludeReversals:   map[int]bool{0: true},
		GenerateMockRoutes: true,
	}

	result, err := builder.Build([]*CorrelatedPair{pair}, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should generate 2 mock routes: Primary Request Response (0110) + Reversal Response (0410)
	require.Len(t, result.MockRoutes, 2)
	assert.Equal(t, "0110", result.MockRoutes[0].ResponseMTI)
	assert.Equal(t, "0410", result.MockRoutes[1].ResponseMTI)
	assert.Equal(t, "0400", result.MockRoutes[1].MatchFields["0"])
}
