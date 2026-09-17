package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	json "github.com/goccy/go-json"
)

const (
	stanFilePath = "stan.json" // File to store STAN value

	// StateDirEnv overrides the directory holding state that must
	// survive reboots (STAN counter, tx state, serve state).
	StateDirEnv = "JISO_STATE_DIR"
)

// StateDir returns the persistent state directory: $JISO_STATE_DIR when
// non-empty, else <XDG state home>/jiso per the XDG Base Directory spec
// ($XDG_STATE_HOME when absolute, else $HOME/.local/state). Never
// os.TempDir: the temp dir is reclaimed by the OS, which silently reset
// the STAN counter between runs.
func StateDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv(StateDirEnv)); dir != "" {
		return dir, nil
	}

	base := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving state home (or set $%s): %w", StateDirEnv, err)
		}

		base = filepath.Join(home, ".local", "state")
	}

	return filepath.Join(base, "jiso"), nil
}

// defaultPersistenceDir resolves and creates the persistence directory.
func defaultPersistenceDir() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create persistence directory: %w", err)
	}

	return dir, nil
}

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
	// dirMu guards persistenceDir: Set/Get are exported and any caller
	// (STAN init, RRN fallback, tests) may touch them concurrently.
	dirMu          sync.RWMutex
	persistenceDir string
	persistChan    chan uint32
	quitChan       chan struct{}
	persistDone    chan struct{}
	quitOnce       sync.Once
)

// SetPersistenceDirectory sets the directory where persistent data will be stored
func SetPersistenceDirectory(dir string) error {
	// Create directory if it doesn't exist
	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create persistence directory: %w", err)
	}
	dirMu.Lock()
	persistenceDir = dir
	dirMu.Unlock()

	return nil
}

// GetPersistenceDirectory returns the current persistence directory
func GetPersistenceDirectory() string {
	dirMu.RLock()
	dir := persistenceDir
	dirMu.RUnlock()

	if dir == "" {
		// Set default directory if not already set
		defaultDir, err := defaultPersistenceDir()
		if err != nil {
			outputf("Warning: Failed to set default persistence directory: %v\n", err)
			return ""
		}

		if err := SetPersistenceDirectory(defaultDir); err != nil {
			outputf("Warning: Failed to set default persistence directory: %v\n", err)
			return ""
		}

		return defaultDir
	}

	return dir
}

// GetPersistencePath returns the full path to the stan file
func getPersistencePath() string {
	dirMu.RLock()
	defer dirMu.RUnlock()

	return filepath.Join(persistenceDir, stanFilePath)
}

func loadPersistedData() (PersistentData, error) {
	data := PersistentData{}

	// If persistence directory not set, use the persistent state dir
	if GetPersistenceDirectory() == "" {
		return data, fmt.Errorf("no persistence directory available")
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
	if strings.TrimSpace(string(fileData)) == "" {
		// Empty file can happen after interrupted writes; treat as no persisted value.
		return data, nil
	}

	err = json.Unmarshal(fileData, &data)
	if err != nil {
		return data, fmt.Errorf("failed to unmarshal persisted data: %w", err)
	}

	return data, nil
}

func persistData(data PersistentData) error {
	persistLock.Lock()
	defer persistLock.Unlock()

	// If persistence directory not set, use the persistent state dir
	if GetPersistenceDirectory() == "" {
		return fmt.Errorf("no persistence directory available")
	}

	// Marshal data
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data for persistence: %w", err)
	}

	// Write atomically using temp file + rename to avoid partial JSON files.
	filePath := getPersistencePath()
	tempFile := filePath + ".tmp"

	if err := os.WriteFile(tempFile, jsonData, 0o644); err != nil {
		return fmt.Errorf("failed to write persisted data to temp file: %w", err)
	}

	if err := os.Rename(tempFile, filePath); err != nil {
		// Best-effort cleanup.
		_ = os.Remove(tempFile)
		return fmt.Errorf("failed to rename persisted temp file: %w", err)
	}

	return nil
}

// Counter is the sequence-number generator behind field 11 (STAN). GetCounter is
// the only way to get one and GetStan is the only thing it does, so the interface
// is the whole public surface; the implementation stays unexported because there
// is no reason for a caller to hold or construct it.
type Counter interface {
	GetStan() string
}

// GetCounter returns the process-wide STAN counter, seeded from its persisted
// value so a restart continues the sequence instead of reusing numbers the
// acquirer has already seen.
func GetCounter() Counter {
	once.Do(func() {
		// Load persisted data
		data, err := loadPersistedData()
		initialValue := uint32(0)
		if err != nil {
			// If we can't load, start from 0 but keep the worker active so value can self-heal on next persist.
			outputf("Warning: Could not load persisted STAN value: %v\n", err)
		} else {
			initialValue = data.StanValue
			if data.StanValue != 0 {
				// The init line only carries information when the
				// persisted value is non-zero (noise otherwise; the
				// line goes through the package sink).
				outputf("STAN counter initialized with persisted value: %d\n", data.StanValue)
			}
		}

		// Initialize counter with loaded or fallback value.
		counterInstance = &counter{value: initialValue}

		// Start persistence goroutine
		persistChan = make(chan uint32, 1)
		quitChan = make(chan struct{})
		persistDone = make(chan struct{})
		quitOnce = sync.Once{}
		go persistWorker()
	})
	return counterInstance
}

func (c *counter) GetStan() string {
	const maxStan = 999999

	for {
		current := atomic.LoadUint32(&c.value)
		next := current + 1
		if next > maxStan {
			next = 1
		}

		if atomic.CompareAndSwapUint32(&c.value, current, next) {
			// Send to persistence worker (non-blocking)
			if persistChan != nil {
				select {
				case persistChan <- next:
				default:
					// Channel full, skip this update
				}
			}
			return fmt.Sprintf("%06d", next)
		}
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
					outputf("Warning: Failed to persist STAN value: %v\n", err)
				}
			}
		case <-quitChan:
			// Flush from the in-memory counter (the source of truth)
			// so a restart never recycles STANs; the ticker could be
			// up to 5s behind at shutdown.
			if v := atomic.LoadUint32(&counterInstance.value); v != 0 {
				if err := persistData(PersistentData{StanValue: v}); err != nil {
					outputf("Warning: Failed to persist STAN value: %v\n", err)
				}
			}

			close(persistDone)

			return
		}
	}
}

// StopPersistWorker stops the persistence worker goroutine, flushing
// the latest counter value to disk before returning (bounded wait).
// Safe to call when the counter was never initialized.
func StopPersistWorker() {
	if quitChan == nil {
		return // counter never initialized
	}

	// close (not send) so the signal is level-triggered: a non-blocking
	// send races the worker's startup and is silently dropped when the
	// worker goroutine has not reached its select yet, losing quit
	// forever(STAN never flushed on fast exits).
	quitOnce.Do(func() { close(quitChan) })

	if persistDone != nil {
		select {
		case <-persistDone:
			return
		case <-time.After(2 * time.Second):
			outputf("Warning: Timed out waiting for STAN persistence flush\n")
		}
	}
}
