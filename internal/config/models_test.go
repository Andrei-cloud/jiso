package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConfigItems_Polymorphic(t *testing.T) {
	jsonInput := `[
		{
			"name": "Legacy Tx",
			"description": "Legacy transaction without type",
			"fields": {"0": "0800"}
		},
		{
			"type": "transaction",
			"name": "Sign On",
			"fields": {"0": "0800", "70": 1}
		},
		{
			"type": "dataset",
			"name": "cards",
			"data": [{"2": "4111111111111111"}]
		},
		{
			"type": "scenario",
			"name": "Auth Scenario",
			"steps": [{"name": "Step 1", "use_transaction_id": "Sign On"}]
		},
		{
			"type": "mock_route",
			"name": "Approval Route",
			"match_mti": "0100",
			"match_de3": "000000",
			"echo_fields": [11, 37],
			"response_mti": "0110",
			"response_fields": {"39": "00"}
		}
	]`

	items, err := ParseConfigItems([]byte(jsonInput))
	require.NoError(t, err)
	require.Len(t, items, 5)

	assert.Equal(t, TypeTransaction, items[0].GetType())
	assert.Equal(t, "Legacy Tx", items[0].Name)

	assert.Equal(t, TypeTransaction, items[1].GetType())
	assert.Equal(t, "Sign On", items[1].Name)

	assert.Equal(t, TypeDataset, items[2].GetType())
	assert.Equal(t, "cards", items[2].Name)

	assert.Equal(t, TypeScenario, items[3].GetType())
	assert.Equal(t, "Auth Scenario", items[3].Name)

	assert.Equal(t, TypeMockRoute, items[4].GetType())
	assert.Equal(t, "Approval Route", items[4].Name)
	assert.Equal(t, "0100", items[4].MatchMTI)
	assert.Equal(t, []int{11, 37}, items[4].EchoFields)
}
