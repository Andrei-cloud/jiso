package transactions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
)

type Transaction struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Fields      json.RawMessage  `json:"fields"`
	Dataset     []map[int]string `json:"dataset"`
}

// Add a cache for quick transaction lookups
type TransactionCollection struct {
	spec         *iso8583.MessageSpec
	transactions []Transaction
	cache        map[string]*Transaction // Add transaction cache
}

func NewTransactionCollection(
	filename string,
	specs *iso8583.MessageSpec,
) (*TransactionCollection, error) {
	if isInvalidFilename(filename) {
		return nil, errors.New("invalid filename")
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var transactions []Transaction
	if err := json.Unmarshal(data, &transactions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

	if len(transactions) == 0 {
		return nil, errors.New("no transactions found in the file")
	}

	tc := &TransactionCollection{
		transactions: transactions,
		spec:         specs,
		cache:        make(map[string]*Transaction),
	}

	// Pre-populate cache
	for i := range tc.transactions {
		tc.cache[tc.transactions[i].Name] = &tc.transactions[i]
	}

	fmt.Printf("Transactions loaded successfully. Count: %d\n", len(tc.transactions))
	return tc, nil
}

func isInvalidFilename(filename string) bool {
	return strings.Contains(filepath.Clean(filename), "..")
}

func (tc *TransactionCollection) ListNames() []string {
	names := make([]string, len(tc.transactions))
	for i, t := range tc.transactions {
		names[i] = t.Name
	}
	return names
}

func (tc *TransactionCollection) Info(name string) (string, string, string, error) {
	t, err := tc.findTransaction(name)
	if err != nil {
		return "", "", "", err
	}

	fieldsJSON, err := json.MarshalIndent(t.Fields, "", "  ")
	if err != nil {
		return "", "", "", err
	}
	return t.Name, t.Description, string(fieldsJSON), nil
}

func (tc *TransactionCollection) Compose(name string) (*iso8583.Message, error) {
	t, err := tc.findTransaction(name)
	if err != nil {
		return nil, err
	}

	msg := iso8583.NewMessage(tc.spec)
	err = tc.populateFields(msg, t)
	if err != nil {
		return nil, err
	}

	return msg, nil
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
	fieldMap := make(map[int]interface{})
	if err := json.Unmarshal(t.Fields, &fieldMap); err != nil {
		return fmt.Errorf("json unmarshal error: %w", err)
	}

	dummyMsg := iso8583.NewMessage(tc.spec)
	if err := json.Unmarshal(t.Fields, &dummyMsg); err != nil {
		return fmt.Errorf("json unmarshal error: %w", err)
	}

	tc.setAutoFields(msg, fieldMap, t)
	tc.setStaticFields(msg, dummyMsg)
	tc.applyRandomValues(msg, t.Dataset)

	return nil
}

func (tc *TransactionCollection) setAutoFields(
	msg *iso8583.Message,
	fieldMap map[int]interface{},
	t *Transaction,
) {
	for i, v := range fieldMap {
		if i < 2 {
			continue
		}

		switch v := v.(type) {
		case string:
			switch v {
			case "auto":
				tc.handleAutoFields(i, msg)
			case "random":
				tc.handleRandomFields(msg, t)
			}
		}
	}
}

func (tc *TransactionCollection) setStaticFields(msg *iso8583.Message, dummyMsg *iso8583.Message) {
	for i, f := range dummyMsg.GetFields() {
		if v, err := f.Bytes(); err == nil {
			// Skip fields with value "auto" or "random" as they are handled separately
			if !bytes.Equal(v, []byte("auto")) && !bytes.Equal(v, []byte("random")) {
				msg.BinaryField(i, v)
			}
		}
	}
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

func (tc *TransactionCollection) ListFormatted() []string {
	maxNameLen := 0
	for _, t := range tc.transactions {
		if len(t.Name) > maxNameLen {
			maxNameLen = len(t.Name)
		}
	}

	formatted := make([]string, len(tc.transactions))
	for i, t := range tc.transactions {
		formatted[i] = fmt.Sprintf("%-*s - %s", maxNameLen, t.Name, t.Description)
	}
	return formatted
}
