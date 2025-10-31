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
	quitChan        chan struct{}
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
		quitChan = make(chan struct{})
		go persistWorker()
	})
	return counterInstance
}

func (c *counter) GetStan() string {
	const maxStan = 999999

	for {
		// Atomically increment the counter
		newVal := atomic.AddUint32(&c.value, 1)

		// Handle wraparound: if we exceed maxStan, wrap to 1
		if newVal > maxStan {
			// Try to reset to 1, but only if we're the one who caused the overflow
			// This prevents race conditions during wraparound
			for {
				current := atomic.LoadUint32(&c.value)
				if current <= maxStan {
					// Someone else already reset it, use the current value
					newVal = current
					break
				}
				// Try to reset to 1
				if atomic.CompareAndSwapUint32(&c.value, current, 1) {
					newVal = 1
					break
				}
				// CAS failed, someone else changed it, retry
			}
		}

		// Ensure we never return 0 (invalid STAN)
		if newVal == 0 {
			// Force it to 1 if somehow we got 0
			atomic.CompareAndSwapUint32(&c.value, 0, 1)
			newVal = 1
		}

		// Send to persistence worker (non-blocking)
		if persistChan != nil {
			select {
			case persistChan <- c.value:
			default:
				// Channel full, skip this update
			}
		}

		return fmt.Sprintf("%06d", newVal)
	}
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
		case <-quitChan:
			return
		}
	}
}

// StopPersistWorker stops the persistence worker goroutine
func StopPersistWorker() {
	select {
	case quitChan <- struct{}{}:
	default:
		// Already stopped or not started
	}
}
