package transactions

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cfg "jiso/internal/config"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
)

const (
	transactionCacheFile = "transaction_cache.json"
)

type Transaction struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Fields      json.RawMessage  `json:"fields"`
	Dataset     []map[int]string `json:"dataset"`
	DatasetName string           `json:"dataset_name"`
}

type Dataset struct {
	Name string              `json:"name"`
	Data []map[string]string `json:"data"`
}

type Scenario struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	DatasetName string         `json:"dataset_name"`
	Steps       []ScenarioStep `json:"steps"`
}

type ScenarioStep struct {
	Name             string                 `json:"name"`
	UseTransactionId string                 `json:"use_transaction_id"`
	Fields           map[string]interface{} `json:"fields"`
	Extract          map[string]string      `json:"extract"`
	Validate         []Assertion            `json:"validate"`
}

type Assertion struct {
	Field  string `json:"field"`
	Expect string `json:"expect,omitempty"`
	Regex  string `json:"regex,omitempty"`
	Exists *bool  `json:"exists,omitempty"`
}

type ConfigItem struct {
	Type           string                 `json:"type"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Fields         json.RawMessage        `json:"fields,omitempty"`
	Dataset        []map[int]string       `json:"dataset,omitempty"`
	Data           []map[string]string    `json:"data,omitempty"`
	DatasetName    string                 `json:"dataset_name,omitempty"`
	Steps          []ScenarioStep         `json:"steps,omitempty"`
	MatchFields    map[string]interface{} `json:"match_fields,omitempty"`
	RequiredFields []string               `json:"required_fields,omitempty"`
	EchoFields     []int                  `json:"echo_fields,omitempty"`
	ResponseMTI    string                 `json:"response_mti,omitempty"`
	ResponseFields map[string]string      `json:"response_fields,omitempty"`
	DelayMs        int                    `json:"delay_ms,omitempty"`
	DropConnection bool                   `json:"drop_connection,omitempty"`
}

// TransactionState stores information about transaction state
type TransactionState struct {
	LastUsedDataset map[string]int   `json:"last_used_dataset"` // Maps transaction names to last used dataset index
	TransactionLogs []TransactionLog `json:"transaction_logs"`  // Store recent transaction logs
}

// TransactionLog tracks usage of transactions
type TransactionLog struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
	Success   bool      `json:"success"`
}

// Add a cache for quick transaction lookups
type TransactionCollection struct {
	spec         *iso8583.MessageSpec
	transactions []Transaction
	cache        map[string]*Transaction // Add transaction cache
	datasets     map[string]*Dataset
	scenarios    map[string]*Scenario

	mockRoutes []cfg.MockRouteConfig

	// State management
	state         TransactionState
	stateLock     sync.RWMutex
	saveLock      sync.Mutex // Protects against concurrent saves
	persistDir    string
	lastSavedUnix int64
}

func (tc *TransactionCollection) GetMockRoutes() []cfg.MockRouteConfig {
	if tc == nil {
		return nil
	}
	return tc.mockRoutes
}

func NewTransactionCollection(
	filename string,
	specs *iso8583.MessageSpec,
) (*TransactionCollection, error) {
	if filename == "" {
		return &TransactionCollection{
			spec:      specs,
			cache:     make(map[string]*Transaction),
			datasets:  make(map[string]*Dataset),
			scenarios: make(map[string]*Scenario),
		}, nil
	}

	if isInvalidFilename(filename) {
		return nil, errors.New("invalid filename")
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var items []ConfigItem
	if err := json.Unmarshal(data, &items); err != nil {
		// Attempt parsing as legacy list of transactions directly
		var legacyTx []Transaction
		if errLegacy := json.Unmarshal(data, &legacyTx); errLegacy == nil {
			items = make([]ConfigItem, len(legacyTx))
			for i, lt := range legacyTx {
				items[i] = ConfigItem{
					Type:        "transaction",
					Name:        lt.Name,
					Description: lt.Description,
					Fields:      lt.Fields,
					Dataset:     lt.Dataset,
				}
			}
		} else {
			return nil, fmt.Errorf("failed to unmarshal data: %w", err)
		}
	}

	tc := &TransactionCollection{
		transactions: make([]Transaction, 0),
		spec:         specs,
		cache:        make(map[string]*Transaction),
		datasets:     make(map[string]*Dataset),
		scenarios:    make(map[string]*Scenario),
		mockRoutes:   make([]cfg.MockRouteConfig, 0),
		state: TransactionState{
			LastUsedDataset: make(map[string]int),
			TransactionLogs: make([]TransactionLog, 0, 100),
		},
	}

	for _, item := range items {
		if item.Type == "" || item.Type == "transaction" {
			t := Transaction{
				Name:        item.Name,
				Description: item.Description,
				Fields:      item.Fields,
				Dataset:     item.Dataset,
				DatasetName: item.DatasetName,
			}
			tc.transactions = append(tc.transactions, t)
		} else if item.Type == "dataset" {
			d := Dataset{
				Name: item.Name,
				Data: item.Data,
			}
			tc.datasets[item.Name] = &d
		} else if item.Type == "scenario" {
			s := Scenario{
				Name:        item.Name,
				Description: item.Description,
				DatasetName: item.DatasetName,
				Steps:       item.Steps,
			}
			tc.scenarios[item.Name] = &s
		} else if item.Type == "mock_route" {
			r := cfg.MockRouteConfig{
				Name:           item.Name,
				MatchFields:    item.MatchFields,
				RequiredFields: item.RequiredFields,
				EchoFields:     item.EchoFields,
				ResponseMTI:    item.ResponseMTI,
				ResponseFields: item.ResponseFields,
				DelayMs:        item.DelayMs,
				DropConnection: item.DropConnection,
			}
			tc.mockRoutes = append(tc.mockRoutes, r)
		}
	}

	if len(tc.transactions) == 0 && len(tc.scenarios) == 0 {
		return nil, errors.New("no transactions or scenarios found in the file")
	}

	// Pre-populate cache
	for i := range tc.transactions {
		tc.cache[tc.transactions[i].Name] = &tc.transactions[i]
	}

	// Validate the transaction collection
	if err := tc.Validate(); err != nil {
		return nil, fmt.Errorf("transaction validation failed: %w", err)
	}

	// Set the persistence directory to the same as used by the STAN counter
	tc.SetPersistenceDirectory(utils.GetPersistenceDirectory())

	// Load saved state
	err = tc.loadState()
	if err != nil {
		fmt.Printf("Warning: Failed to load transaction state: %v\n", err)
	}

	return tc, nil
}

// SetPersistenceDirectory sets directory for transaction state persistence
func (tc *TransactionCollection) SetPersistenceDirectory(dir string) error {
	tc.stateLock.Lock()
	defer tc.stateLock.Unlock()

	// Create directory if it doesn't exist
	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create transaction persistence directory: %w", err)
	}

	tc.persistDir = dir
	return nil
}

// SaveState persists transaction state to disk
func (tc *TransactionCollection) SaveState() error {
	tc.saveLock.Lock()
	defer tc.saveLock.Unlock()

	tc.stateLock.RLock()
	defer tc.stateLock.RUnlock()

	if tc.persistDir == "" {
		// If persistence directory not set, use default temp directory
		persistDir := filepath.Join(os.TempDir(), "jiso")
		if err := tc.SetPersistenceDirectory(persistDir); err != nil {
			return err
		}
	}

	filePath := filepath.Join(tc.persistDir, transactionCacheFile)

	// Marshal data
	jsonData, err := json.MarshalIndent(tc.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal transaction state: %w", err)
	}

	// Write atomically
	tempFile := filePath + ".tmp"
	if err := os.WriteFile(tempFile, jsonData, 0o644); err != nil {
		return fmt.Errorf("failed to write transaction state to temp file: %w", err)
	}

	if err := os.Rename(tempFile, filePath); err != nil {
		return fmt.Errorf("failed to rename transaction temp file: %w", err)
	}

	return nil
}

// loadState loads transaction state from disk
func (tc *TransactionCollection) loadState() error {
	tc.stateLock.Lock()
	defer tc.stateLock.Unlock()

	if tc.persistDir == "" {
		// If persistence directory not set, use default temp directory
		persistDir := filepath.Join(os.TempDir(), "jiso")
		if err := tc.SetPersistenceDirectory(persistDir); err != nil {
			return err
		}
	}

	filePath := filepath.Join(tc.persistDir, transactionCacheFile)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// File doesn't exist, nothing to load
		return nil
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read transaction state: %w", err)
	}

	// Unmarshal data
	var state TransactionState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to unmarshal transaction state: %w", err)
	}

	// Update state
	tc.state = state

	return nil
}

// LogTransaction records a transaction and saves state periodically
func (tc *TransactionCollection) LogTransaction(name string, success bool) {
	tc.stateLock.Lock()

	// Add to transaction logs
	tc.state.TransactionLogs = append(tc.state.TransactionLogs, TransactionLog{
		Name:      name,
		Timestamp: time.Now(),
		Success:   success,
	})

	// Trim logs if they get too large
	if len(tc.state.TransactionLogs) > 1000 {
		tc.state.TransactionLogs = tc.state.TransactionLogs[len(tc.state.TransactionLogs)-1000:]
	}

	tc.stateLock.Unlock()

	// Save state periodically (rate-limited to at most once every 5 seconds)
	now := time.Now().Unix()
	lastSaved := atomic.LoadInt64(&tc.lastSavedUnix)
	if now-lastSaved >= 5 {
		if atomic.CompareAndSwapInt64(&tc.lastSavedUnix, lastSaved, now) {
			go func() {
				_ = tc.SaveState()
			}()
		}
	}
}

// GetTransactionHistory returns recent transaction logs
func (tc *TransactionCollection) GetTransactionHistory(limit int) []TransactionLog {
	tc.stateLock.RLock()
	defer tc.stateLock.RUnlock()

	if limit <= 0 || limit > len(tc.state.TransactionLogs) {
		limit = len(tc.state.TransactionLogs)
	}

	start := len(tc.state.TransactionLogs) - limit
	if start < 0 {
		start = 0
	}

	return tc.state.TransactionLogs[start:]
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
	msg, err := tc.ComposeRaw(name)
	if err != nil {
		return nil, err
	}

	t, err := tc.findTransaction(name)
	if err != nil {
		return msg, nil
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

	tc.interpolateMessageFieldsWithData(msg, datasetName)
	return msg, nil
}

func (tc *TransactionCollection) ComposeRaw(name string) (*iso8583.Message, error) {
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

func (tc *TransactionCollection) interpolateMessageFieldsWithData(msg *iso8583.Message, datasetName string) {
	dataRegex := regexp.MustCompile(`\{\{\s*data\.(\w+)\s*\}\}`)
	contextRegex := regexp.MustCompile(`\{\{\s*context\.(\w+)\s*\}\}`)

	var selectedRow map[string]string
	var ok bool

	for i, f := range msg.GetFields() {
		if f == nil {
			continue
		}
		val, err := f.String()
		if err != nil || val == "" {
			continue
		}

		if strings.Contains(val, "{{") && strings.Contains(val, "}}") {
			val = dataRegex.ReplaceAllStringFunc(val, func(m string) string {
				match := dataRegex.FindStringSubmatch(m)
				if len(match) > 1 {
					key := match[1]

					if !ok && datasetName != "" {
						if ds, exist := tc.datasets[datasetName]; exist && len(ds.Data) > 0 {
							randomIndex := rand.Intn(len(ds.Data))
							selectedRow = ds.Data[randomIndex]
							ok = true
						}
					}

					if ok {
						if v, exist := selectedRow[key]; exist {
							return v
						}
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

			msg.Field(i, val)
		}
	}
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
	fieldMap map[int]interface{},
	t *Transaction,
) {
	for i, v := range fieldMap {
		if i < 2 {
			continue
		}

		switch v := v.(type) {
		case string:
			cleanVal := strings.TrimSpace(strings.ToLower(v))
			if isReservedAutoKeywordString(cleanVal) {
				if cleanVal == "random" || cleanVal == "$random" {
					tc.handleRandomFields(msg, t)
				} else {
					tc.handleAutoFieldsWithKeyword(i, msg, cleanVal)
				}
			}
		}
	}
}

func (tc *TransactionCollection) setStaticFields(msg *iso8583.Message, dummyMsg *iso8583.Message) {
	for i, f := range dummyMsg.GetFields() {
		if v, err := f.Bytes(); err == nil {
			// Skip fields with reserved auto keywords as they are handled dynamically
			if !isReservedAutoKeyword(v) {
				msg.BinaryField(i, v)
			}
		}
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

// Validate performs comprehensive validation of the transaction collection
func (tc *TransactionCollection) Validate() error {
	if tc == nil {
		return fmt.Errorf("transaction collection is nil")
	}

	if len(tc.transactions) == 0 && len(tc.scenarios) == 0 {
		return fmt.Errorf("no transactions or scenarios found in collection")
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

func (tc *TransactionCollection) ListScenarios() []string {
	names := make([]string, 0, len(tc.scenarios))
	for name := range tc.scenarios {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (tc *TransactionCollection) GetScenario(name string) (*Scenario, error) {
	s, ok := tc.scenarios[name]
	if !ok {
		return nil, fmt.Errorf("scenario not found: %s", name)
	}
	return s, nil
}

func (tc *TransactionCollection) GetDataset(name string) (*Dataset, error) {
	d, ok := tc.datasets[name]
	if !ok {
		return nil, fmt.Errorf("dataset not found: %s", name)
	}
	return d, nil
}

// validateTransactionFields validates the fields of a single transaction
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
