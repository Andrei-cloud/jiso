package transactions

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"
)

func (t *Transaction) ensureParsed(spec *iso8583.MessageSpec) error {
	if t.parsedCache == nil {
		t.parsedCache = &transactionParsedCache{}
	}
	var parseErr error
	t.parsedCache.once.Do(func() {
		if len(t.Fields) == 0 {
			return
		}

		fieldMap := make(map[int]interface{})
		if err := json.Unmarshal(t.Fields, &fieldMap); err != nil {
			parseErr = fmt.Errorf("json unmarshal error: %w", err)
			return
		}
		t.parsedCache.fieldMap = fieldMap

		staticFields := make(map[int]interface{})
		autoFields := make(map[int]string)

		for k, v := range fieldMap {
			if strVal, ok := v.(string); ok {
				cleanVal := strings.TrimSpace(strings.ToLower(strVal))
				if isReservedAutoKeywordString(cleanVal) {
					autoFields[k] = cleanVal
					continue
				}
				staticFields[k] = strVal
			} else if v != nil {
				staticFields[k] = v
			}
		}

		t.parsedCache.staticFields = staticFields
		t.parsedCache.autoFields = autoFields
	})
	return parseErr
}

func (tc *TransactionCollection) Compose(name string) (*iso8583.Message, error) {
	t, err := tc.findTransaction(name)
	if err != nil {
		return nil, err
	}

	datasetName := t.DatasetName
	if datasetName == "" && len(tc.datasets) > 0 {
		if _, ok := tc.datasets["card_pool"]; ok {
			datasetName = "card_pool"
		} else {
			for name := range tc.datasets {
				datasetName = name
				break
			}
		}
	}

	targetSpec := utils.ResolveSpec(t.Spec, tc.spec)
	if err := t.ensureParsed(targetSpec); err != nil {
		return nil, err
	}

	msg := iso8583.NewMessage(targetSpec)
	tc.setAutoFields(msg, t.parsedCache.autoFields, t)

	var selectedRow map[string]string
	if datasetName != "" {
		selectedRow = tc.selectDatasetRow(datasetName)
	}

	for fieldID, rawValue := range t.parsedCache.staticFields {
		resolvedValue, keep := resolveFieldValueWithData(rawValue, selectedRow)
		if !keep {
			continue
		}
		if err := tc.setFieldValue(msg, targetSpec, fieldID, resolvedValue); err != nil {
			return nil, err
		}
	}

	tc.applyRandomValues(msg, t.Dataset)
	return msg, nil
}

func (tc *TransactionCollection) ComposeRaw(name string) (*iso8583.Message, error) {
	t, err := tc.findTransaction(name)
	if err != nil {
		return nil, err
	}

	targetSpec := utils.ResolveSpec(t.Spec, tc.spec)
	msg := iso8583.NewMessage(targetSpec)
	err = tc.populateFields(msg, t)
	if err != nil {
		return nil, err
	}

	return msg, nil
}

func (tc *TransactionCollection) interpolateMessageFieldsWithData(msg *iso8583.Message, datasetName string) {
	var selectedRow map[string]string
	var ok bool
	ensureSelectedRow := func() {
		if ok || datasetName == "" {
			return
		}
		if ds, exist := tc.datasets[datasetName]; exist && len(ds.Data) > 0 {
			randomIndex := rand.Intn(len(ds.Data))
			selectedRow = ds.Data[randomIndex]
			ok = true
		}
	}

	for i, f := range msg.GetFields() {
		if f == nil {
			continue
		}

		if composite, isComposite := f.(*field.Composite); isComposite {
			ensureSelectedRow()
			if ok {
				_ = tc.interpolateCompositeFieldWithData(composite, "", selectedRow)
			}
			continue
		}

		val, err := f.String()
		if err != nil || val == "" {
			continue
		}

		if strings.Contains(val, "{{") && strings.Contains(val, "}}") {
			ensureSelectedRow()
			val = interpolateStringWithData(val, selectedRow)

			msg.Field(i, val)
		}
	}
}

func (tc *TransactionCollection) selectDatasetRow(datasetName string) map[string]string {
	if datasetName == "" {
		return nil
	}
	if ds, exist := tc.datasets[datasetName]; exist && len(ds.Data) > 0 {
		randomIndex := rand.Intn(len(ds.Data))
		return ds.Data[randomIndex]
	}
	return nil
}

func resolveFieldValueWithData(value interface{}, selectedRow map[string]string) (interface{}, bool) {
	switch v := value.(type) {
	case string:
		if !strings.Contains(v, "{{") || !strings.Contains(v, "}}") {
			return v, true
		}
		resolved, missingData := interpolateCompositePlaceholderString(v, selectedRow)
		if missingData {
			return nil, false
		}
		return resolved, true
	case map[string]interface{}:
		resolved := make(map[string]interface{})
		for key, nested := range v {
			resolvedValue, keep := resolveFieldValueWithData(nested, selectedRow)
			if !keep {
				continue
			}
			resolved[key] = resolvedValue
		}
		if len(resolved) == 0 {
			return nil, false
		}
		return resolved, true
	default:
		return value, true
	}
}

func interpolateStringWithData(val string, selectedRow map[string]string) string {
	val = dataRegex.ReplaceAllStringFunc(val, func(m string) string {
		match := dataRegex.FindStringSubmatch(m)
		if len(match) > 1 && selectedRow != nil {
			if v, exist := selectedRow[match[1]]; exist {
				return v
			}
		}
		return m
	})

	val = contextRegex.ReplaceAllStringFunc(val, func(m string) string {
		match := contextRegex.FindStringSubmatch(m)
		if len(match) > 1 {
			return ""
		}
		return m
	})

	return val
}

func (tc *TransactionCollection) interpolateCompositeFieldWithData(
	composite *field.Composite,
	prefix string,
	selectedRow map[string]string,
) error {
	for key, subField := range composite.GetSubfields() {
		if subField == nil {
			continue
		}

		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		if nestedComposite, ok := subField.(*field.Composite); ok {
			if err := tc.interpolateCompositeFieldWithData(nestedComposite, path, selectedRow); err != nil {
				return err
			}
			continue
		}

		val, err := subField.String()
		if err != nil || val == "" || !strings.Contains(val, "{{") || !strings.Contains(val, "}}") {
			continue
		}

		resolved, missingData := interpolateCompositePlaceholderString(val, selectedRow)
		if missingData {
			if err := composite.UnsetPath(path); err != nil {
				return err
			}
			continue
		}

		if err := composite.MarshalPath(path, resolved); err != nil {
			return err
		}
	}

	return nil
}

func interpolateCompositePlaceholderString(val string, selectedRow map[string]string) (string, bool) {
	missingData := false

	val = dataRegex.ReplaceAllStringFunc(val, func(m string) string {
		match := dataRegex.FindStringSubmatch(m)
		if len(match) > 1 && selectedRow != nil {
			if v, exist := selectedRow[match[1]]; exist {
				return v
			}
		}
		missingData = true
		return ""
	})

	val = contextRegex.ReplaceAllStringFunc(val, func(m string) string {
		match := contextRegex.FindStringSubmatch(m)
		if len(match) > 1 {
			return ""
		}
		return m
	})

	return val, missingData
}

func (tc *TransactionCollection) findTransaction(name string) (*Transaction, error) {
	// Check cache first
	if transaction, exists := tc.cache[name]; exists {
		return transaction, nil
	}

	// Fall back to iteration if not in cache
	for i := range tc.transactions {
		if tc.transactions[i].Name == name {
			// Add to cache for future lookups
			tc.cache[name] = &tc.transactions[i]
			return &tc.transactions[i], nil
		}
	}

	return nil, fmt.Errorf("transaction not found: %s", name)
}

func (tc *TransactionCollection) populateFields(msg *iso8583.Message, t *Transaction) error {
	targetSpec := utils.ResolveSpec(t.Spec, tc.spec)
	if err := t.ensureParsed(targetSpec); err != nil {
		return err
	}

	tc.setAutoFields(msg, t.parsedCache.autoFields, t)
	tc.setStaticFields(msg, t.parsedCache.staticFields, targetSpec)
	tc.applyRandomValues(msg, t.Dataset)

	return nil
}

func isReservedAutoKeyword(v []byte) bool {
	return isReservedAutoKeywordString(string(v))
}

func isReservedAutoKeywordString(s string) bool {
	cleanVal := strings.TrimSpace(strings.ToLower(s))
	switch cleanVal {
	case "auto", "$auto", "stan", "$stan", "gen_stan", "rrn", "$rrn", "gen_rrn", "auth_code", "$auth_code", "gen_auth_code", "datetime", "$datetime", "date", "time", "random", "$random":
		return true
	default:
		return false
	}
}

func (tc *TransactionCollection) setAutoFields(
	msg *iso8583.Message,
	autoFields map[int]string,
	t *Transaction,
) {
	for i, cleanVal := range autoFields {
		if cleanVal == "random" || cleanVal == "$random" {
			tc.handleRandomFields(msg, t)
		} else {
			tc.handleAutoFieldsWithKeyword(i, msg, cleanVal)
		}
	}
}

func (tc *TransactionCollection) setStaticFields(msg *iso8583.Message, staticFields map[int]interface{}, spec *iso8583.MessageSpec) {
	for i, v := range staticFields {
		_ = tc.setFieldValue(msg, spec, i, v)
	}
}

func (tc *TransactionCollection) setFieldValue(msg *iso8583.Message, spec *iso8583.MessageSpec, fieldID int, value interface{}) error {
	switch v := value.(type) {
	case string:
		if fieldID == 0 {
			msg.MTI(v)
			return nil
		}
		return msg.Field(fieldID, v)
	case float64:
		if math.Mod(v, 1) == 0 {
			return tc.setFieldValue(msg, spec, fieldID, strconv.FormatInt(int64(v), 10))
		}
		return tc.setFieldValue(msg, spec, fieldID, strconv.FormatFloat(v, 'f', -1, 64))
	case int:
		return tc.setFieldValue(msg, spec, fieldID, strconv.Itoa(v))
	case int64:
		return tc.setFieldValue(msg, spec, fieldID, strconv.FormatInt(v, 10))
	case bool:
		return tc.setFieldValue(msg, spec, fieldID, strconv.FormatBool(v))
	case map[string]interface{}:
		return tc.setCompositeFieldValue(msg, spec, fieldID, v)
	default:
		return tc.setFieldValue(msg, spec, fieldID, fmt.Sprintf("%v", v))
	}
}

func (tc *TransactionCollection) setCompositeFieldValue(
	msg *iso8583.Message,
	spec *iso8583.MessageSpec,
	fieldID int,
	value map[string]interface{},
) error {
	if spec == nil || spec.Fields == nil {
		return fmt.Errorf("spec is required for composite field %d", fieldID)
	}

	specField, exists := spec.Fields[fieldID]
	if !exists || specField == nil {
		return fmt.Errorf("field %d is not defined in spec", fieldID)
	}

	instance := field.NewInstanceOf(specField)
	composite, ok := instance.(*field.Composite)
	if !ok {
		return msg.Field(fieldID, fmt.Sprintf("%v", value))
	}

	if err := applyCompositePaths(composite, value, ""); err != nil {
		return fmt.Errorf("failed to apply composite field %d: %w", fieldID, err)
	}

	packed, err := composite.Bytes()
	if err != nil {
		return fmt.Errorf("failed to pack composite field %d: %w", fieldID, err)
	}

	return msg.BinaryField(fieldID, packed)
}

func applyCompositePaths(composite *field.Composite, value map[string]interface{}, prefix string) error {
	for key, raw := range value {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		if nested, ok := raw.(map[string]interface{}); ok {
			if err := applyCompositePaths(composite, nested, path); err != nil {
				return err
			}
			continue
		}

		normalized := normalizeCompositeScalar(raw)
		if err := composite.MarshalPath(path, normalized); err != nil {
			return err
		}
	}

	return nil
}

func normalizeCompositeScalar(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if math.Mod(val, 1) == 0 {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case bool:
		return strconv.FormatBool(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func (tc *TransactionCollection) handleAutoFieldsWithKeyword(i int, msg *iso8583.Message, keyword string) {
	cleanKey := strings.TrimSpace(strings.ToLower(keyword))
	switch cleanKey {
	case "stan", "$stan":
		msg.Field(i, utils.GetCounter().GetStan())
		return
	case "rrn", "$rrn":
		msg.Field(i, utils.GetRRNInstance().GetRRN())
		return
	case "auth_code", "$auth_code":
		msg.Field(i, utils.RandString(6))
		return
	case "datetime", "$datetime":
		msg.Field(i, utils.GetTrxnDateTime())
		return
	case "date":
		msg.Field(i, time.Now().Format("0102"))
		return
	case "time":
		msg.Field(i, time.Now().Format("150405"))
		return
	}

	// Default auto logic
	tc.handleAutoFields(i, msg)
}

func (tc *TransactionCollection) handleAutoFields(i int, msg *iso8583.Message) {
	// Get field spec to determine the correct auto value based on field description
	fieldSpec := tc.spec.Fields[i]
	if fieldSpec == nil {
		// Field not found in spec, cannot determine auto value
		return
	}

	// Look at the field description to determine what kind of auto value to generate
	description := fieldSpec.Spec().Description

	switch i {
	case 7:
		// Field 7: Transmission Date & Time (MMDDhhmmss format)
		msg.Field(i, utils.GetTrxnDateTime())
	case 11:
		// Field 11: Systems Trace Audit Number (STAN)
		msg.Field(i, utils.GetCounter().GetStan())
	case 12:
		// Field 12: Local Transaction Time (hhmmss format)
		currentTime := time.Now().Format("150405") // hour, minute, second
		msg.Field(i, currentTime)
	case 13:
		// Field 13: Local Transaction Date (MMDD format)
		currentDate := time.Now().Format("0102") // month, day
		msg.Field(i, currentDate)
	case 15:
		// Field 15: Settlement Date (MMDD format)
		currentDate := time.Now().Format("0102") // month, day
		msg.Field(i, currentDate)
	case 17:
		// Field 17: Capture Date (MMDD format)
		currentDate := time.Now().Format("0102") // month, day
		msg.Field(i, currentDate)
	case 37:
		// Field 37: Retrieval Reference Number
		msg.Field(i, utils.GetRRNInstance().GetRRN())
	case 38:
		// Field 38: Authorization Identification Response / Auth Code
		msg.Field(i, utils.RandString(6))
	default:
		// For any other field marked as "auto", try to make an intelligent decision
		if strings.Contains(description, "Date") {
			// If it's a date field, use current date in MMDD format
			msg.Field(i, time.Now().Format("0102"))
		} else if strings.Contains(description, "Time") {
			// If it's a time field, use current time in hhmmss format
			msg.Field(i, time.Now().Format("150405"))
		} else {
			// Default to using a random numeric string matching the field's length
			fieldLength := fieldSpec.Spec().Length
			msg.Field(i, utils.RandString(fieldLength))
		}
	}
}

func (tc *TransactionCollection) handleRandomFields(msg *iso8583.Message, t *Transaction) {
	// Simply delegate to the consolidated function for random values
	tc.applyRandomValues(msg, t.Dataset)
}

// Consolidated random field handling
func (tc *TransactionCollection) applyRandomValues(msg *iso8583.Message, dataset []map[int]string) {
	if len(dataset) == 0 {
		return
	}

	// Pick a random entry from the dataset using a better RNG
	randSource := rand.New(rand.NewSource(time.Now().UnixNano()))
	randIndex := randSource.Intn(len(dataset))
	randomValues := dataset[randIndex]

	// Apply values
	for fieldID, value := range randomValues {
		if value == "" {
			continue
		}

		// Try to determine correct field type and set accordingly
		if fieldID >= 2 && fieldID <= 128 {
			// Get field definition from spec
			fieldDef := tc.spec.Fields[fieldID]
			if fieldDef != nil {
				// Default case or fallback
				msg.Field(fieldID, value)
			} else {
				// Field not in spec, use default handling
				msg.Field(fieldID, value)
			}
		}
	}
}
