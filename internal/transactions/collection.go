package transactions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	cfg "jiso/internal/config"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
)

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
		switch item.Type {
		case "", "transaction":
			t := Transaction{
				Name:        item.Name,
				Description: item.Description,
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
			s := Scenario{
				Name:        item.Name,
				Description: item.Description,
				DatasetName: item.DatasetName,
				Steps:       item.Steps,
			}
			tc.scenarios[item.Name] = &s
		case "mock_route":
			r := cfg.MockRouteConfig{
				Name:           item.Name,
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
