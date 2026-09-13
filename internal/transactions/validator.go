package transactions

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/moov-io/iso8583"
	isofield "github.com/moov-io/iso8583/field"

	"jiso/internal/utils"
)

// Validate checks the collection's transactions, scenarios and mock routes for
// required names, uniqueness, field ranges and spec-conforming field values.
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
		if err := tc.validateScenario(name, scenario, seenNames); err != nil {
			return err
		}
	}

	return nil
}

// validateScenario checks a scenario's name uniqueness and step definitions.
func (tc *TransactionCollection) validateScenario(name string, scenario *Scenario, seenNames map[string]bool) error {
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
		if step.UseTransactionID == "" && len(step.Fields) == 0 {
			return fmt.Errorf("scenario '%s' step '%s' must specify use_transaction_id or fields", name, step.Name)
		}
	}

	return nil
}

func (tc *TransactionCollection) validateTransactionFields(t Transaction) error {
	fieldMap := make(map[int]any)
	if err := json.Unmarshal(t.Fields, &fieldMap); err != nil {
		return fmt.Errorf("invalid JSON in fields: %w", err)
	}

	targetSpec := utils.ResolveSpec(t.Spec, tc.spec)

	for fieldID, value := range fieldMap {
		// Validate field ID range (ISO8583 fields are 0-128, where 0=MTI, 1=bitmap, 2-128=data)
		if fieldID < 0 || fieldID > 128 {
			return fmt.Errorf("field ID %d is out of valid range (0-128)", fieldID)
		}

		// Validate field value based on type
		switch v := value.(type) {
		case string:
			if err := validateStringFieldLength(fieldID, v, targetSpec); err != nil {
				return err
			}
		case float64:
			// Numeric fields are valid
			continue
		case map[string]any:
			if err := validateCompositeField(fieldID, targetSpec); err != nil {
				return err
			}
		default:
			return fmt.Errorf("field %d has unsupported value type: %T", fieldID, v)
		}
	}

	return nil
}

// validateStringFieldLength checks a string field value against the spec's maximum
// length, skipping special keywords and runtime placeholders. Fixed-prefix
// fields without padding are ALSO checked for undersize: moov's prefixer
// refuses EncodeLength when dataLen != fixLen, so a short value is a
// guaranteed Pack failure — catching it at load names the real problem
// instead of letting it resurface mid-run as a misleading send error
// (UAT round 5).
func validateStringFieldLength(fieldID int, v string, targetSpec *iso8583.MessageSpec) error {
	if v == utils.KeywordAuto || v == utils.KeywordRandom {
		return nil // These are valid special values
	}
	// Skip validation for values containing placeholders as their real length
	// will be resolved at runtime during variable injection.
	if strings.Contains(v, "{{") && strings.Contains(v, "}}") {
		return nil
	}
	if targetSpec == nil || targetSpec.Fields == nil {
		return nil
	}
	fieldSpec := targetSpec.Fields[fieldID]
	if fieldSpec == nil {
		return nil
	}
	fs := fieldSpec.Spec()
	gotLen := validationLength(fieldSpec, v)
	if gotLen > fs.Length {
		return fmt.Errorf("field %d value '%s' exceeds maximum length %d", fieldID, v, fs.Length)
	}

	return validateFixedLengthUndersize(fieldID, v, fieldSpec, fs, gotLen)
}

// validateFixedLengthUndersize rejects a String value shorter than a
// fixed-prefix field's length: moov's prefixer refuses EncodeLength when
// dataLen != fixLen, so such a value is a guaranteed Pack failure that
// used to resurface mid-run as a misleading "network send failed" (UAT
// round 5). Compose-time keywords and padded fields are exempt: their
// runtime length is not the literal's length.
func validateFixedLengthUndersize(fieldID int, v string, fieldSpec isofield.Field, fs *isofield.Spec, gotLen int) error {
	if gotLen >= fs.Length || fs.Pad != nil || !isFixedPrefix(fs) {
		return nil
	}
	if _, isString := fieldSpec.(*isofield.String); !isString {
		return nil
	}
	switch v {
	case utils.KeywordAuto, utils.KeywordRandom, utils.KeywordAuthCode,
		utils.KeywordSTAN, utils.KeywordRRN, utils.KeywordDateTime:
		return nil // dynamic values size themselves at compose time
	}

	return fmt.Errorf(
		"field %d value '%s' is %d characters but spec prefix %s requires exactly %d (add padding to the value or the spec)",
		fieldID, v, gotLen, fs.Pref.Inspect(), fs.Length,
	)
}

// isFixedPrefix reports whether a field spec's length prefixer is a
// fixed-length one (moov's Inspect tokens end in ".Fixed", e.g.
// "ASCII.Fixed").
func isFixedPrefix(fs *isofield.Spec) bool {
	return fs.Pref != nil && strings.HasSuffix(fs.Pref.Inspect(), ".Fixed")
}

// validateCompositeField checks that an object field value is defined in the spec
// and configured with subfields.
func validateCompositeField(fieldID int, targetSpec *iso8583.MessageSpec) error {
	if targetSpec == nil || targetSpec.Fields == nil || targetSpec.Fields[fieldID] == nil {
		return fmt.Errorf("field %d composite value provided but field is not defined in spec", fieldID)
	}
	if len(targetSpec.Fields[fieldID].Spec().Subfields) == 0 {
		return fmt.Errorf("field %d has object value but is not configured with subfields in spec", fieldID)
	}

	return nil
}

// validateTransactionDataset validates the dataset of a single transaction
func (tc *TransactionCollection) validateTransactionDataset(t Transaction) error {
	if len(t.Dataset) == 0 {
		// Empty dataset is valid (no random values needed)
		return nil
	}

	targetSpec := utils.ResolveSpec(t.Spec, tc.spec)

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
			if targetSpec != nil && targetSpec.Fields != nil {
				if fieldSpec := targetSpec.Fields[fieldID]; fieldSpec != nil {
					maxLen := fieldSpec.Spec().Length
					if validationLength(fieldSpec, value) > maxLen {
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

func validationLength(fieldSpec isofield.Field, value string) int {
	if _, ok := fieldSpec.(*isofield.Binary); ok {
		if decoded, err := hex.DecodeString(value); err == nil {
			return len(decoded)
		}
	}

	return len(value)
}
