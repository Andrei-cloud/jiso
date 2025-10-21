package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	stanFilePath = "stan.json" // File to store STAN value
)

type counter struct {
	value uint32
}

// PersistentData holds data that should be persisted between program runs
type PersistentData struct {
	StanValue uint32 `json:"stan_value"`
}

var (
	counterInstance *counter
	once            sync.Once
	persistLock     sync.Mutex
	persistenceDir  string
	persistChan     chan uint32
)

// SetPersistenceDirectory sets the directory where persistent data will be stored
func SetPersistenceDirectory(dir string) error {
	// Create directory if it doesn't exist
	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create persistence directory: %w", err)
	}
	persistenceDir = dir
	return nil
}

// GetPersistenceDirectory returns the current persistence directory
func GetPersistenceDirectory() string {
	if persistenceDir == "" {
		// Set default directory if not already set
		defaultDir := filepath.Join(os.TempDir(), "jiso")
		if err := SetPersistenceDirectory(defaultDir); err != nil {
			fmt.Printf("Warning: Failed to set default persistence directory: %v\n", err)
			return ""
		}
	}
	return persistenceDir
}

// GetPersistencePath returns the full path to the stan file
func getPersistencePath() string {
	return filepath.Join(persistenceDir, stanFilePath)
}

func loadPersistedData() (PersistentData, error) {
	data := PersistentData{}

	// If persistence directory not set, use default temp directory
	if persistenceDir == "" {
		persistenceDir = filepath.Join(os.TempDir(), "jiso")
		if err := SetPersistenceDirectory(persistenceDir); err != nil {
			return data, err
		}
	}

	filePath := getPersistencePath()

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// File doesn't exist, return default data
		return data, nil
	}

	// Read file
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return data, fmt.Errorf("failed to read persisted data: %w", err)
	}

	// Unmarshal data
	err = json.Unmarshal(fileData, &data)
	if err != nil {
		return data, fmt.Errorf("failed to unmarshal persisted data: %w", err)
	}

	return data, nil
}

func persistData(data PersistentData) error {
	persistLock.Lock()
	defer persistLock.Unlock()

	// If persistence directory not set, use default
	if persistenceDir == "" {
		persistenceDir = filepath.Join(os.TempDir(), "jiso")
		if err := SetPersistenceDirectory(persistenceDir); err != nil {
			return err
		}
	}

	// Marshal data
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data for persistence: %w", err)
	}

	// Write to file
	err = os.WriteFile(getPersistencePath(), jsonData, 0o644)
	if err != nil {
		return fmt.Errorf("failed to persist data: %w", err)
	}

	return nil
}

func GetCounter() *counter {
	once.Do(func() {
		// Load persisted data
		data, err := loadPersistedData()
		if err != nil {
			// If we can't load, start from 0 but log the error
			fmt.Printf("Warning: Could not load persisted STAN value: %v\n", err)
			counterInstance = &counter{value: 0}
			return
		}

		// Initialize counter with the loaded value
		counterInstance = &counter{value: data.StanValue}
		fmt.Printf("STAN counter initialized with persisted value: %d\n", data.StanValue)

		// Start persistence goroutine
		persistChan = make(chan uint32, 1)
		go persistWorker()
	})
	return counterInstance
}

func (c *counter) GetStan() string {
	var val uint32
	for {
		val = atomic.AddUint32(&c.value, 1) % 1000000
		if val != 0 {
			break
		}
		// If val is 0, we decrement the counter to -1, so that the next increment will set it to 0 again.
		atomic.AddUint32(&c.value, ^uint32(0))
	}

	// Send to persistence worker (non-blocking)
	select {
	case persistChan <- c.value:
	default:
		// Channel full, skip this update
	}

	return fmt.Sprintf("%06d", val)
}

func persistWorker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastValue uint32
	for {
		select {
		case val := <-persistChan:
			lastValue = val
		case <-ticker.C:
			if lastValue != 0 {
				err := persistData(PersistentData{StanValue: lastValue})
				if err != nil {
					fmt.Printf("Warning: Failed to persist STAN value: %v\n", err)
				}
			}
		}
	}
}
