package transactions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/moov-io/iso8583"

	cfg "jiso/internal/config"
	"jiso/internal/utils"
)

// GetMockRoutes returns the mock routes the transaction file declares. It is nil
// safe because the TUI asks a collection that failed to load, where the mock
// page has to show an empty state instead of panicking.
func (tc *TransactionCollection) GetMockRoutes() []cfg.MockRouteConfig {
	if tc == nil {
		return nil
	}
	return tc.mockRoutes
}

// SetSpec replaces the spec messages are composed against, nil-safe for the same
// reason as GetMockRoutes.
func (tc *TransactionCollection) SetSpec(spec *iso8583.MessageSpec) {
	if tc != nil {
		tc.spec = spec
	}
}

// NewTransactionCollection loads the transaction file at filename against specs.
// An empty filename gives an empty collection rather than an error: running with
// a --spec and no transaction file is valid, it simply has nothing to send.
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
		if errLegacy := json.Unmarshal(data, &legacyTx); errLegacy != nil {
			return nil, fmt.Errorf("failed to unmarshal data: %w", err)
		}
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
		tc.addItem(item)
	}

	if len(tc.transactions) == 0 && len(tc.scenarios) == 0 && len(tc.mockRoutes) == 0 {
		return nil, errors.New("no transactions, scenarios, or mock routes found in the file")
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
	_ = tc.SetPersistenceDirectory(utils.GetPersistenceDirectory())

	// Load saved state
	err = tc.loadState()
	if err != nil {
		outputf("Warning: Failed to load transaction state: %v\n", err)
	}

	return tc, nil
}

// addItem appends a parsed config item to the collection under its type.
func (tc *TransactionCollection) addItem(item ConfigItem) {
	switch item.Type {
	case "", "transaction":
		specPath := item.Spec
		if specPath == "" {
			specPath = item.SpecFile
		}
		t := Transaction{
			Name:        item.Name,
			Description: item.Description,
			Spec:        specPath,
			Fields:      item.Fields,
			Dataset:     item.Dataset,
			DatasetName: item.DatasetName,
		}
		tc.transactions = append(tc.transactions, t)
	case "dataset":
		d := Dataset{
			Name: item.Name,
			Data: item.Data,
		}
		tc.datasets[item.Name] = &d
	case "scenario":
		var steps []ScenarioStep
		if len(item.Steps) > 0 {
			_ = json.Unmarshal(item.Steps, &steps)
		}
		s := Scenario{
			Name:        item.Name,
			Description: item.Description,
			DatasetName: item.DatasetName,
			Steps:       steps,
		}
		tc.scenarios[item.Name] = &s
	case "mock_route":
		r := cfg.MockRouteConfig{
			Name:           item.Name,
			Description:    item.Description,
			MatchFields:    item.MatchFields,
			RequiredFields: item.RequiredFields,
			EchoFields:     item.EchoFields,
			ResponseMTI:    item.ResponseMTI,
			ResponseFields: item.ResponseFields,
			DelayMs:        item.DelayMs,
			LatencyMs:      item.LatencyMs,
			JitterMs:       item.JitterMs,
			DropConnection: item.DropConnection,
		}
		tc.mockRoutes = append(tc.mockRoutes, r)
	}
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

	// Resolve the persistence directory before taking the read lock:
	// SetPersistenceDirectory needs the write lock and would deadlock
	// against an already-held RLock (lock upgrade).
	tc.stateLock.RLock()
	persistDir := tc.persistDir
	tc.stateLock.RUnlock()

	if persistDir == "" {
		// If persistence directory not set, use default temp directory
		persistDir = filepath.Join(os.TempDir(), "jiso")
		if err := tc.SetPersistenceDirectory(persistDir); err != nil {
			return err
		}
	}

	tc.stateLock.RLock()
	defer tc.stateLock.RUnlock()

	filePath := filepath.Join(persistDir, transactionCacheFile)

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

// loadState loads transaction state from disk. Only called during
// construction; the persistence directory must be resolved before taking
// the state write lock (SetPersistenceDirectory acquires it too).
func (tc *TransactionCollection) loadState() error {
	if tc.persistDir == "" {
		// If persistence directory not set, use default temp directory
		persistDir := filepath.Join(os.TempDir(), "jiso")
		if err := tc.SetPersistenceDirectory(persistDir); err != nil {
			return err
		}
	}

	tc.stateLock.Lock()
	defer tc.stateLock.Unlock()

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

	// Save state periodically (rate-limited to at most once every 5 seconds).
	// The save runs synchronously: the CAS guarantees at most one caller per
	// window, and a fire-and-forget goroutine could be lost on process exit.
	now := time.Now().Unix()
	lastSaved := atomic.LoadInt64(&tc.lastSavedUnix)
	if now-lastSaved >= 5 {
		if atomic.CompareAndSwapInt64(&tc.lastSavedUnix, lastSaved, now) {
			_ = tc.SaveState()
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

	// Clone: callers iterate the result after the lock is released, while
	// LogTransaction keeps appending to the shared backing array.
	return slices.Clone(tc.state.TransactionLogs[start:])
}

func isInvalidFilename(filename string) bool {
	return strings.Contains(filepath.Clean(filename), "..")
}

// ListNames returns the transaction names in file order, which is the order the
// send list shows them.
func (tc *TransactionCollection) ListNames() []string {
	names := make([]string, len(tc.transactions))
	for i, t := range tc.transactions {
		names[i] = t.Name
	}
	return names
}

// TransactionInfo is one transaction's detail as the Info accessor and the
// Repository interface report it: its name, description, and fields rendered
// as indented JSON, plus the per-entry spec and dataset the transactions
// table shows. Spec is the path the entry declared ("" when it declared
// none — the fallback spec is never reported as declared); Dataset names the
// entry's own rows "inline" or the referenced dataset, with DatasetRows as
// its row count (0 for an existing empty dataset, -1 for a reference with no
// dataset behind it).
type TransactionInfo struct {
	Name        string
	Description string
	FieldsJSON  string
	Spec        string
	Dataset     string
	DatasetRows int
}

// inlineDataset is the Dataset label for a transaction carrying its own
// dataset rows instead of naming a shared one.
const inlineDataset = "inline"

// datasetFor resolves the display dataset of one transaction. Inline
// rows are applied on every compose, so they label the entry. A dangling
// dataset_name reports -1 rows — the caller renders the name without a
// count rather than panic or invent one.
func (tc *TransactionCollection) datasetFor(t *Transaction) (string, int) {
	if len(t.Dataset) > 0 {
		return inlineDataset, len(t.Dataset)
	}
	if t.DatasetName == "" {
		return "", 0
	}
	if d, ok := tc.datasets[t.DatasetName]; ok {
		return t.DatasetName, len(d.Data)
	}

	return t.DatasetName, -1
}

// Info returns the named transaction's name, description, fields as
// indented JSON, declared spec, and resolved dataset, or an error when the
// collection has no such transaction.
func (tc *TransactionCollection) Info(name string) (TransactionInfo, error) {
	t, err := tc.findTransaction(name)
	if err != nil {
		return TransactionInfo{}, err
	}

	fieldsJSON, err := json.MarshalIndent(t.Fields, "", "  ")
	if err != nil {
		return TransactionInfo{}, err
	}

	dataset, rows := tc.datasetFor(t)

	return TransactionInfo{
		Name:        t.Name,
		Description: t.Description,
		FieldsJSON:  string(fieldsJSON),
		Spec:        t.Spec,
		Dataset:     dataset,
		DatasetRows: rows,
	}, nil
}

// ListFormatted returns the transactions as "name  description" lines padded to
// the longest name, so the CLI list aligns without the caller measuring.
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

// ListScenarios returns the scenario names sorted: they come out of a map, and an
// operator comparing two runs of the list should see one order, not two.
func (tc *TransactionCollection) ListScenarios() []string {
	names := make([]string, 0, len(tc.scenarios))
	for name := range tc.scenarios {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetScenario returns the named scenario, or an error naming what was asked for.
func (tc *TransactionCollection) GetScenario(name string) (*Scenario, error) {
	s, ok := tc.scenarios[name]
	if !ok {
		return nil, fmt.Errorf("scenario not found: %s", name)
	}
	return s, nil
}

// GetDataset returns the named dataset, or an error naming what was asked for.
func (tc *TransactionCollection) GetDataset(name string) (*Dataset, error) {
	d, ok := tc.datasets[name]
	if !ok {
		return nil, fmt.Errorf("dataset not found: %s", name)
	}
	return d, nil
}

// validateTransactionFields validates the fields of a single transaction
