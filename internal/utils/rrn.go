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
	rrnFilePath = "rrn.json" // File to store RRN value
)

type RRN struct {
	value uint32
}

// RRNPersistentData holds RRN data that should be persisted
type RRNPersistentData struct {
	RRNValue uint32 `json:"rrn_value"`
}

var (
	rrnInstance    *RRN
	rrnOnce        sync.Once
	rrnPersistLock sync.Mutex
)

func GetRRNInstance() *RRN {
	rrnOnce.Do(func() {
		// Extend the PersistentData structure to include RRN
		data, err := loadPersistedRRNData()
		if err != nil {
			// If we can't load, start from 0 but log the error
			fmt.Printf("Warning: Could not load persisted RRN value: %v\n", err)
			rrnInstance = &RRN{value: 0}
			return
		}

		// Initialize RRN with the loaded value
		rrnInstance = &RRN{value: data.RRNValue}
		fmt.Printf("RRN counter initialized with persisted value: %d\n", data.RRNValue)
	})
	return rrnInstance
}

// Load RRN data from persistence file
func loadPersistedRRNData() (RRNPersistentData, error) {
	data := RRNPersistentData{}

	// If persistence directory not set, use default temp directory
	if persistenceDir == "" {
		persistenceDir = filepath.Join(os.TempDir(), "jiso")
		if err := SetPersistenceDirectory(persistenceDir); err != nil {
			return data, err
		}
	}

	filePath := filepath.Join(persistenceDir, rrnFilePath)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// File doesn't exist, return default data
		return data, nil
	}

	// Read file
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return data, fmt.Errorf("failed to read persisted RRN data: %w", err)
	}

	// Unmarshal data
	err = json.Unmarshal(fileData, &data)
	if err != nil {
		return data, fmt.Errorf("failed to unmarshal persisted RRN data: %w", err)
	}

	return data, nil
}

// Persist RRN data to file
func persistRRNData(data RRNPersistentData) error {
	rrnPersistLock.Lock()
	defer rrnPersistLock.Unlock()

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
		return fmt.Errorf("failed to marshal RRN data for persistence: %w", err)
	}

	// Write atomically using temp file + rename
	filePath := filepath.Join(persistenceDir, rrnFilePath)
	tempFile := filePath + ".tmp"

	if err := os.WriteFile(tempFile, jsonData, 0o644); err != nil {
		return fmt.Errorf("failed to write RRN data to temp file: %w", err)
	}

	if err := os.Rename(tempFile, filePath); err != nil {
		// Clean up temp file on failure
		os.Remove(tempFile)
		return fmt.Errorf("failed to rename RRN temp file: %w", err)
	}

	return nil
}

func (r *RRN) GetRRN() string {
	t := time.Now()
	y, d := t.Year(), t.YearDay()
	const maxRRNSeq = 9999999 // 7 digits

	var rrn uint32
	for {
		// Atomically increment the counter
		newVal := atomic.AddUint32(&r.value, 1)

		// Handle wraparound: if we exceed maxRRNSeq, wrap to 1
		if newVal > maxRRNSeq {
			// Try to reset to 1, but only if we're the one who caused the overflow
			for {
				current := atomic.LoadUint32(&r.value)
				if current <= maxRRNSeq {
					// Someone else already reset it, use the current value
					newVal = current
					break
				}
				// Try to reset to 1
				if atomic.CompareAndSwapUint32(&r.value, current, 1) {
					newVal = 1
					break
				}
				// CAS failed, someone else changed it, retry
			}
		}

		// Ensure we never return 0 (invalid RRN sequence)
		if newVal == 0 {
			// Force it to 1 if somehow we got 0
			atomic.CompareAndSwapUint32(&r.value, 0, 1)
			newVal = 1
		}

		rrn = newVal
		break // We got a valid value
	}

	// Persist the updated value in a goroutine to avoid blocking
	go func(currentValue uint32) {
		err := persistRRNData(RRNPersistentData{
			RRNValue: currentValue,
		})
		if err != nil {
			fmt.Printf("Warning: Failed to persist RRN value: %v\n", err)
		}
	}(r.value)

	// generate RRN: ydddnnnnnnnn
	return fmt.Sprintf(
		"%02d%03d%07d",
		y%100,
		d,
		rrn,
	) // %02d to keep last two digits of the year, %03d to ensure 3 digits for the day of the year, %07d to ensure 7 digits for the rrn
}
