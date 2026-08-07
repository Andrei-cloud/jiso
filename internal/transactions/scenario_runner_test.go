package transactions

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
)

func TestInjectVariables(t *testing.T) {
	runner := &ScenarioRunner{
		selectedDatasets: map[string]map[string]string{
			"card_pool": {
				"2":  "1234567890123456",
				"14": "2512",
			},
		},
		sessionState: map[string]string{
			"AuthId": "998877",
		},
	}

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "PAN: {{data.2}}",
			expected: "PAN: 1234567890123456",
		},
		{
			input:    "EXP: {{data.14}}",
			expected: "EXP: 2512",
		},
		{
			input:    "AUTH: {{context.AuthId}}",
			expected: "AUTH: 998877",
		},
		{
			input:    "Combined: {{data.2}} - {{context.AuthId}}",
			expected: "Combined: 1234567890123456 - 998877",
		},
		{
			input:    "No variables here",
			expected: "No variables here",
		},
		{
			input:    "Spaces: {{ data.2 }} and {{ context.AuthId }}",
			expected: "Spaces: 1234567890123456 and 998877",
		},
	}

	for _, tc := range tests {
		result := runner.injectVariables(tc.input, "card_pool")
		assert.Equal(t, tc.expected, result)
	}
}

func TestLoadUnifiedConfig(t *testing.T) {
	configData := `[
		{
			"type": "transaction",
			"name": "Sign On",
			"description": "Network Sign On",
			"fields": {
				"0": "0800",
				"7": "auto"
			}
		},
		{
			"type": "dataset",
			"name": "card_pool",
			"data": [
				{
					"2": "1111222233334444",
					"14": "2912"
				}
			]
		},
		{
			"type": "scenario",
			"name": "Sign On and Purchase",
			"description": "E2E Test Scenario",
			"dataset_name": "card_pool",
			"steps": [
				{
					"name": "Sign On Step",
					"use_transaction_id": "Sign On",
					"validate": [
						{
							"field": "39",
							"expect": "00"
						}
					]
				}
			]
		}
	]`

	tmpFile, err := os.CreateTemp("", "unified_config.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configData)
	if err != nil {
		t.Fatalf("failed to write config data: %v", err)
	}
	tmpFile.Close()

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(tmpFile.Name(), spec)
	if err != nil {
		t.Fatalf("failed to load transaction collection: %v", err)
	}

	// Verify loaded components
	assert.Len(t, tc.transactions, 1)
	assert.Contains(t, tc.cache, "Sign On")

	assert.Len(t, tc.datasets, 1)
	assert.Contains(t, tc.datasets, "card_pool")
	assert.Equal(t, "1111222233334444", tc.datasets["card_pool"].Data[0]["2"])

	assert.Len(t, tc.scenarios, 1)
	assert.Contains(t, tc.scenarios, "Sign On and Purchase")
	assert.Equal(t, "card_pool", tc.scenarios["Sign On and Purchase"].DatasetName)
	assert.Len(t, tc.scenarios["Sign On and Purchase"].Steps, 1)
	assert.Equal(t, "Sign On Step", tc.scenarios["Sign On and Purchase"].Steps[0].Name)
}

func TestValidationAssertions(t *testing.T) {
	// Create mock response message
	spec := iso8583.Spec87
	msg := iso8583.NewMessage(spec)
	msg.Field(39, "00")
	msg.Field(38, "123456")

	runner := NewScenarioRunner(nil, nil)

	// Test case 1: Expect exact match (success)
	step := ScenarioStep{
		Validate: []Assertion{
			{
				Field:  "39",
				Expect: "00",
			},
		},
	}
	result := runner.runAssertionsOnMessage(msg, step.Validate)
	assert.True(t, result.Success)
	assert.Len(t, result.ValidationErrors, 0)

	// Test case 2: Expect exact match (failure)
	step = ScenarioStep{
		Validate: []Assertion{
			{
				Field:  "39",
				Expect: "01",
			},
		},
	}
	result = runner.runAssertionsOnMessage(msg, step.Validate)
	assert.False(t, result.Success)
	assert.Len(t, result.ValidationErrors, 1)
	assert.Equal(t, "01", result.ValidationErrors[0].Expected)
	assert.Equal(t, "00", result.ValidationErrors[0].Actual)

	// Test case 3: Regex match (success)
	step = ScenarioStep{
		Validate: []Assertion{
			{
				Field: "38",
				Regex: "^[0-9]{6}$",
			},
		},
	}
	result = runner.runAssertionsOnMessage(msg, step.Validate)
	assert.True(t, result.Success)

	// Test case 4: Exists checks
	existsTrue := true
	existsFalse := false

	step = ScenarioStep{
		Validate: []Assertion{
			{
				Field:  "38",
				Exists: &existsTrue,
			},
			{
				Field:  "41",
				Exists: &existsFalse,
			},
		},
	}
	result = runner.runAssertionsOnMessage(msg, step.Validate)
	assert.True(t, result.Success)
}

// helper to run assertions directly for unit testing
func (sr *ScenarioRunner) runAssertionsOnMessage(respMsg *iso8583.Message, assertions []Assertion) StepResult {
	result := StepResult{
		Success: true,
	}

	for _, assertion := range assertions {
		var fieldID int
		fmt.Sscanf(assertion.Field, "%d", &fieldID)
		fieldObj := respMsg.GetField(fieldID)

		if assertion.Exists != nil {
			exists := hasValue(fieldObj)
			if exists != *assertion.Exists {
				result.Success = false
				result.ValidationErrors = append(result.ValidationErrors, ValidationError{
					Field:    assertion.Field,
					Expected: fmt.Sprintf("exists=%t", *assertion.Exists),
					Actual:   fmt.Sprintf("exists=%t", exists),
				})
				continue
			}
		}

		if fieldObj == nil {
			if assertion.Expect != "" || assertion.Regex != "" {
				result.Success = false
				result.ValidationErrors = append(result.ValidationErrors, ValidationError{
					Field:  assertion.Field,
					Actual: "nil",
				})
			}
			continue
		}

		actualValue, _ := fieldObj.String()

		if assertion.Expect != "" {
			if actualValue != assertion.Expect {
				result.Success = false
				result.ValidationErrors = append(result.ValidationErrors, ValidationError{
					Field:    assertion.Field,
					Expected: assertion.Expect,
					Actual:   actualValue,
				})
			}
		}

		if assertion.Regex != "" {
			re, _ := regexp.Compile(assertion.Regex)
			if !re.MatchString(actualValue) {
				result.Success = false
				result.ValidationErrors = append(result.ValidationErrors, ValidationError{
					Field:    assertion.Field,
					Expected: assertion.Regex,
					Actual:   actualValue,
				})
			}
		}
	}

	return result
}

func TestComposeInterpolation(t *testing.T) {
	configData := `[
		{
			"type": "transaction",
			"name": "Purchase Template",
			"description": "Purchase transaction with placeholders",
			"dataset_name": "card_pool",
			"fields": {
				"0": "0200",
				"2": "{{data.2}}"
			}
		},
		{
			"type": "dataset",
			"name": "card_pool",
			"data": [
				{
					"2": "9999888877776666"
				}
			]
		}
	]`

	tmpFile, err := os.CreateTemp("", "test_compose.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configData)
	if err != nil {
		t.Fatalf("failed to write data: %v", err)
	}
	tmpFile.Close()

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(tmpFile.Name(), spec)
	if err != nil {
		t.Fatalf("failed to load transaction collection: %v", err)
	}

	// Composing via Compose should interpolate the placeholders using default card dataset
	msg, err := tc.Compose("Purchase Template")
	if err != nil {
		t.Fatalf("failed to compose: %v", err)
	}

	val, err := msg.GetField(2).String()
	assert.NoError(t, err)
	assert.Equal(t, "9999888877776666", val)

	// Composing via ComposeRaw should keep the placeholders intact
	msgRaw, err := tc.ComposeRaw("Purchase Template")
	if err != nil {
		t.Fatalf("failed to compose raw: %v", err)
	}

	valRaw, err := msgRaw.GetField(2).String()
	assert.NoError(t, err)
	assert.Equal(t, "{{data.2}}", valRaw)
}

func TestComposeInterpolationRandom(t *testing.T) {
	configData := `[
		{
			"type": "transaction",
			"name": "Purchase Template",
			"description": "Purchase transaction with placeholders",
			"dataset_name": "card_pool",
			"fields": {
				"0": "0200",
				"2": "{{data.2}}"
			}
		},
		{
			"type": "dataset",
			"name": "card_pool",
			"data": [
				{"2": "1111"},
				{"2": "2222"},
				{"2": "3333"},
				{"2": "4444"},
				{"2": "5555"}
			]
		}
	]`

	tmpFile, err := os.CreateTemp("", "test_compose_random.json")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(configData)
	if err != nil {
		t.Fatalf("failed to write data: %v", err)
	}
	tmpFile.Close()

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(tmpFile.Name(), spec)
	if err != nil {
		t.Fatalf("failed to load transaction collection: %v", err)
	}

	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		msg, err := tc.Compose("Purchase Template")
		if err != nil {
			t.Fatalf("failed to compose: %v", err)
		}
		val, err := msg.GetField(2).String()
		assert.NoError(t, err)
		seen[val] = true
	}

	// Verify that we saw multiple different randomly-selected values
	assert.Greater(t, len(seen), 1, "Should select different items randomly across 50 invocations")
}
