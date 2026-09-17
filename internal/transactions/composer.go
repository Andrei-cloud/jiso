package transactions

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/moov-io/iso8583"

	"jiso/internal/utils"
)

// ensureParsed parses the transaction fields JSON once and returns the
// resolved static/auto field maps. The parse error is cached so every
// concurrent (and later) caller observes the same failure instead of
// silently proceeding with an empty template.
func (tc *TransactionCollection) ensureParsed(t *Transaction) (map[int]any, map[int]string, error) {
	tc.parseMu.Lock()
	if t.parsedCache == nil {
		t.parsedCache = &transactionParsedCache{}
	}
	cache := t.parsedCache
	tc.parseMu.Unlock()

	cache.mu.Lock()
	defer cache.mu.Unlock()

	if !cache.done {
		cache.done = true
		cache.parseFields(t.Fields)
	}
	return cache.staticFields, cache.autoFields, cache.err
}

// parseFields parses the transaction fields JSON into the cache's field/static/
// auto maps, recording any unmarshal error so later callers observe the same
// failure.
func (c *transactionParsedCache) parseFields(fields json.RawMessage) {
	if len(fields) == 0 {
		return
	}

	fieldMap := make(map[int]any)
	if err := json.Unmarshal(fields, &fieldMap); err != nil {
		c.err = fmt.Errorf("json unmarshal error: %w", err)

		return
	}
	c.fieldMap = fieldMap

	staticFields := make(map[int]any)
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

	c.staticFields = staticFields
	c.autoFields = autoFields
}

// Compose builds the named transaction's message ready to send. The dataset row
// for this send is drawn here and the dynamic values (sequence numbers, derived
// dates, random fields) are filled in, so two calls on one transaction
// legitimately produce two different messages.
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
	staticFields, autoFields, err := tc.ensureParsed(t)
	if err != nil {
		return nil, err
	}

	msg := iso8583.NewMessage(targetSpec)
	tc.setAutoFields(msg, autoFields, t)

	var selectedRow map[string]string
	if datasetName != "" {
		selectedRow = tc.selectDatasetRow(datasetName)
	}

	for fieldID, rawValue := range staticFields {
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

// ComposeRaw populates the fields the transaction spells out and stops: no
// dataset row is drawn. NOTE the honest caveat: auto-keyword
// fields still flow through setAutoFields, and a $stan field draws its value
// from GetCounter.GetStan — an atomic increment of the persisted global
// counter. So composing a $stan template DOES consume a sequence value and
// previews a STAN the real send will not reuse (it draws the next one). The
// increment is safe and intended; only the old "no sequence number is
// consumed" claim was wrong.
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

func resolveFieldValueWithData(value any, selectedRow map[string]string) (any, bool) {
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
	case map[string]any:
		resolved := make(map[string]any)
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

func interpolateCompositePlaceholderString(val string, selectedRow map[string]string) (string, bool) {
	missingData := false

	val = dataRegex.ReplaceAllStringFunc(val, func(m string) string {
		key := extractPlaceholderKey(m, "data.")
		if key != "" && selectedRow != nil {
			if v, exist := selectedRow[key]; exist {
				return v
			}
		}
		missingData = true
		return ""
	})

	val = contextRegex.ReplaceAllStringFunc(val, func(_ string) string {
		return ""
	})

	return val, missingData
}

func extractPlaceholderKey(m, prefix string) string {
	m = strings.TrimSpace(m)
	m = strings.TrimPrefix(m, "{{")
	m = strings.TrimSuffix(m, "}}")
	m = strings.TrimSpace(m)
	if strings.HasPrefix(m, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(m, prefix))
	}
	return ""
}

func (tc *TransactionCollection) findTransaction(name string) (*Transaction, error) {
	// Check cache first
	tc.cacheMu.RLock()
	transaction, exists := tc.cache[name]
	tc.cacheMu.RUnlock()
	if exists {
		return transaction, nil
	}

	// Fall back to iteration if not in cache
	for i := range tc.transactions {
		if tc.transactions[i].Name == name {
			// Add to cache for future lookups
			tc.cacheMu.Lock()
			tc.cache[name] = &tc.transactions[i]
			tc.cacheMu.Unlock()
			return &tc.transactions[i], nil
		}
	}

	return nil, fmt.Errorf("transaction not found: %s", name)
}

func (tc *TransactionCollection) populateFields(msg *iso8583.Message, t *Transaction) error {
	targetSpec := utils.ResolveSpec(t.Spec, tc.spec)
	staticFields, autoFields, err := tc.ensureParsed(t)
	if err != nil {
		return err
	}

	tc.setAutoFields(msg, autoFields, t)
	tc.setStaticFields(msg, staticFields, targetSpec)
	tc.applyRandomValues(msg, t.Dataset)

	return nil
}

func isReservedAutoKeywordString(s string) bool {
	cleanVal := strings.TrimSpace(strings.ToLower(s))
	switch cleanVal {
	case "auto", "$auto", "stan", "$stan", "gen_stan", "rrn", "$rrn", "gen_rrn", "auth_code", "$auth_code", "gen_auth_code", "datetime", "$datetime", "date", "time", utils.KeywordRandom, "$random":
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
		if cleanVal == utils.KeywordRandom || cleanVal == "$random" {
			tc.handleRandomFields(msg, t)
		} else {
			tc.handleAutoFieldsWithKeyword(i, msg, cleanVal)
		}
	}
}

func (tc *TransactionCollection) setStaticFields(msg *iso8583.Message, staticFields map[int]any, spec *iso8583.MessageSpec) {
	for i, v := range staticFields {
		_ = tc.setFieldValue(msg, spec, i, v)
	}
}

func (tc *TransactionCollection) setFieldValue(msg *iso8583.Message, spec *iso8583.MessageSpec, fieldID int, value any) error {
	if fieldID == 0 {
		if s, ok := value.(string); ok {
			msg.MTI(s)
			return nil
		}
	}
	switch v := value.(type) {
	case string:
		if fieldID == 0 {
			msg.MTI(v)
			return nil
		}
		return msg.Field(fieldID, v)
	case int:
		return msg.Field(fieldID, strconv.Itoa(v))
	case int64:
		return msg.Field(fieldID, strconv.FormatInt(v, 10))
	case float64:
		if v == math.Trunc(v) {
			return msg.Field(fieldID, strconv.FormatInt(int64(v), 10))
		}
		return msg.Field(fieldID, strconv.FormatFloat(v, 'f', -1, 64))
	case bool:
		return msg.Field(fieldID, strconv.FormatBool(v))
	case map[string]any:
		return tc.setCompositeFieldValue(msg, spec, fieldID, v)
	default:
		return msg.Field(fieldID, fmt.Sprintf("%v", v))
	}
}

func (tc *TransactionCollection) setCompositeFieldValue(
	msg *iso8583.Message,
	spec *iso8583.MessageSpec,
	fieldID int,
	value map[string]any,
) error {
	return utils.SetCompositeFieldValue(msg, spec, fieldID, value)
}

func (tc *TransactionCollection) handleAutoFieldsWithKeyword(i int, msg *iso8583.Message, keyword string) {
	cleanKey := strings.TrimSpace(strings.ToLower(keyword))
	switch cleanKey {
	case "stan", "$stan":
		_ = msg.Field(i, utils.GetCounter().GetStan())
		return
	case "rrn", "$rrn":
		_ = msg.Field(i, utils.GetRRNInstance().GetRRN())
		return
	case utils.KeywordAuthCode, "$auth_code":
		_ = msg.Field(i, utils.RandString(6))
		return
	case "datetime", "$datetime":
		_ = msg.Field(i, utils.GetTrxnDateTime())
		return
	case "date":
		_ = msg.Field(i, time.Now().Format("0102"))
		return
	case "time":
		_ = msg.Field(i, time.Now().Format("150405"))
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
		_ = msg.Field(i, utils.GetTrxnDateTime())
	case 11:
		// Field 11: Systems Trace Audit Number (STAN)
		_ = msg.Field(i, utils.GetCounter().GetStan())
	case 12:
		// Field 12: Local Transaction Time (hhmmss format)
		currentTime := time.Now().Format("150405") // hour, minute, second
		_ = msg.Field(i, currentTime)
	case 13:
		// Field 13: Local Transaction Date (MMDD format)
		currentDate := time.Now().Format("0102") // month, day
		_ = msg.Field(i, currentDate)
	case 15:
		// Field 15: Settlement Date (MMDD format)
		currentDate := time.Now().Format("0102") // month, day
		_ = msg.Field(i, currentDate)
	case 17:
		// Field 17: Capture Date (MMDD format)
		currentDate := time.Now().Format("0102") // month, day
		_ = msg.Field(i, currentDate)
	case 37:
		// Field 37: Retrieval Reference Number
		_ = msg.Field(i, utils.GetRRNInstance().GetRRN())
	case 38:
		// Field 38: Authorization Identification Response / Auth Code
		_ = msg.Field(i, utils.RandString(6))
	default:
		// For any other field marked as "auto", the spec's description says which
		// kind of value to generate. The cases are three unrelated predicates over
		// one string, which is what a switch-on-true is for; the value each one
		// produces is the field's own format, not a variation of the previous one.
		switch {
		case strings.Contains(description, "Date"):
			// A date field takes the current date in MMDD format.
			_ = msg.Field(i, time.Now().Format("0102"))
		case strings.Contains(description, "Time"):
			// A time field takes the current time in hhmmss format.
			_ = msg.Field(i, time.Now().Format("150405"))
		default:
			// Neither: a random numeric string matching the field's length.
			fieldLength := fieldSpec.Spec().Length
			_ = msg.Field(i, utils.RandString(fieldLength))
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

	randIndex := rand.Intn(len(dataset))
	randomValues := dataset[randIndex]

	// Apply values
	for fieldID, value := range randomValues {
		if value == "" {
			continue
		}

		if fieldID >= 2 && fieldID <= 128 {
			_ = msg.Field(fieldID, value)
		}
	}
}
