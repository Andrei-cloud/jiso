package transactions

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (tc *TransactionCollection) Validate() error {
	if tc == nil {
		return fmt.Errorf("transaction collection is nil")
	}

	if len(tc.transactions) == 0 && len(tc.scenarios) == 0 && len(tc.mockRoutes) == 0 {
		return fmt.Errorf("no transactions, scenarios, or mock routes found in collection")
	}

	// Track seen names for uniqueness validation
	seenNames := make(map[string]bool)

	for i, transaction := range tc.transactions {
		// Validate transaction name
		if transaction.Name == "" {
			return fmt.Errorf("transaction at index %d has empty name", i)
		}
		if len(transaction.Name) > 50 {
			return fmt.Errorf(
				"transaction name '%s' is too long (max 50 characters)",
				transaction.Name,
			)
		}
		if seenNames[transaction.Name] {
			return fmt.Errorf("duplicate transaction name: %s", transaction.Name)
		}
		seenNames[transaction.Name] = true

		// Validate transaction description
		if len(transaction.Description) > 200 {
			return fmt.Errorf(
				"transaction '%s' description is too long (max 200 characters)",
				transaction.Name,
			)
		}

		// Validate fields
		if err := tc.validateTransactionFields(transaction); err != nil {
			return fmt.Errorf("transaction '%s': %w", transaction.Name, err)
		}

		// Validate dataset
		if err := tc.validateTransactionDataset(transaction); err != nil {
			return fmt.Errorf("transaction '%s': %w", transaction.Name, err)
		}
	}

	// Validate scenarios
	for name, scenario := range tc.scenarios {
		if scenario.Name == "" {
			return fmt.Errorf("scenario has empty name")
		}
		if seenNames[scenario.Name] {
			return fmt.Errorf("duplicate scenario name: %s", scenario.Name)
		}
		seenNames[scenario.Name] = true

		if len(scenario.Steps) == 0 {
			return fmt.Errorf("scenario '%s' has no steps", name)
		}
		for i, step := range scenario.Steps {
			if step.Name == "" {
				return fmt.Errorf("scenario '%s' step %d has empty name", name, i)
			}
			if step.UseTransactionId == "" && len(step.Fields) == 0 {
				return fmt.Errorf("scenario '%s' step '%s' must specify use_transaction_id or fields", name, step.Name)
			}
		}
	}

	return nil
}


func (tc *TransactionCollection) validateTransactionFields(t Transaction) error {
	fieldMap := make(map[int]interface{})
	if err := json.Unmarshal(t.Fields, &fieldMap); err != nil {
		return fmt.Errorf("invalid JSON in fields: %w", err)
	}

	for fieldID, value := range fieldMap {
		// Validate field ID range (ISO8583 fields are 0-128, where 0=MTI, 1=bitmap, 2-128=data)
		if fieldID < 0 || fieldID > 128 {
			return fmt.Errorf("field ID %d is out of valid range (0-128)", fieldID)
		}

		// Validate field value based on type
		switch v := value.(type) {
		case string:
			if v == "auto" || v == "random" {
				// These are valid special values
				continue
			}
			// Skip validation for values containing placeholders as their real length
			// will be resolved at runtime during variable injection.
			if strings.Contains(v, "{{") && strings.Contains(v, "}}") {
				continue
			}
			// For string values, check length against spec if available
			if tc.spec != nil && tc.spec.Fields != nil {
				if fieldSpec := tc.spec.Fields[fieldID]; fieldSpec != nil {
					maxLen := fieldSpec.Spec().Length
					if len(v) > maxLen {
						return fmt.Errorf("field %d value '%s' exceeds maximum length %d", fieldID, v, maxLen)
					}
				}
			}
		case float64:
			// Numeric fields are valid
			continue
		default:
			return fmt.Errorf("field %d has unsupported value type: %T", fieldID, v)
		}
	}

	return nil
}

// validateTransactionDataset validates the dataset of a single transaction
func (tc *TransactionCollection) validateTransactionDataset(t Transaction) error {
	if len(t.Dataset) == 0 {
		// Empty dataset is valid (no random values needed)
		return nil
	}

	for i, entry := range t.Dataset {
		if entry == nil {
			return fmt.Errorf("dataset entry at index %d is nil", i)
		}

		for fieldID, value := range entry {
			// Validate field ID range
			if fieldID < 0 || fieldID > 128 {
				return fmt.Errorf(
					"dataset entry %d has invalid field ID %d (must be 0-128)",
					i,
					fieldID,
				)
			}

			// Validate value is not empty
			if value == "" {
				return fmt.Errorf("dataset entry %d field %d has empty value", i, fieldID)
			}

			// Check length against spec if available
			if tc.spec != nil && tc.spec.Fields != nil {
				if fieldSpec := tc.spec.Fields[fieldID]; fieldSpec != nil {
					maxLen := fieldSpec.Spec().Length
					if len(value) > maxLen {
						return fmt.Errorf(
							"dataset entry %d field %d value '%s' exceeds maximum length %d",
							i,
							fieldID,
							value,
							maxLen,
						)
					}
				}
			}
		}
	}

	return nil
}
