package utils

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSetPersistenceDirectory(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	err := SetPersistenceDirectory(tempDir)
	if err != nil {
		t.Fatalf("SetPersistenceDirectory failed: %v", err)
	}

	if GetPersistenceDirectory() != tempDir {
		t.Errorf("Expected persistence directory %s, got %s", tempDir, GetPersistenceDirectory())
	}

	// Verify directory was created
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		t.Errorf("Persistence directory was not created")
	}
}

func TestGetPersistenceDirectory(t *testing.T) {
	// Reset global state
	originalDir := persistenceDir
	defer func() { persistenceDir = originalDir }()

	persistenceDir = ""

	dir := GetPersistenceDirectory()
	if dir == "" {
		t.Error("GetPersistenceDirectory returned empty string")
	}

	// Should create default directory
	if !strings.Contains(dir, "jiso") {
		t.Errorf("Expected default directory to contain 'jiso', got %s", dir)
	}
}

func TestLoadPersistedData(t *testing.T) {
	// Create a temporary directory
	tempDir := t.TempDir()
	SetPersistenceDirectory(tempDir)

	// Test loading non-existent file
	data, err := loadPersistedData()
	if err != nil {
		t.Fatalf("loadPersistedData failed for non-existent file: %v", err)
	}
	if data.StanValue != 0 {
		t.Errorf("Expected StanValue 0 for non-existent file, got %d", data.StanValue)
	}

	// Create a test file with data
	testData := PersistentData{StanValue: 12345}
	jsonData, _ := json.Marshal(testData)
	filePath := getPersistencePath()
	err = os.WriteFile(filePath, jsonData, 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test loading existing file
	data, err = loadPersistedData()
	if err != nil {
		t.Fatalf("loadPersistedData failed for existing file: %v", err)
	}
	if data.StanValue != 12345 {
		t.Errorf("Expected StanValue 12345, got %d", data.StanValue)
	}
}

func TestPersistData(t *testing.T) {
	// Create a temporary directory
	tempDir := t.TempDir()
	SetPersistenceDirectory(tempDir)

	testData := PersistentData{StanValue: 67890}

	err := persistData(testData)
	if err != nil {
		t.Fatalf("persistData failed: %v", err)
	}

	// Verify file was created and contains correct data
	filePath := getPersistencePath()
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read persisted file: %v", err)
	}

	var loadedData PersistentData
	err = json.Unmarshal(fileData, &loadedData)
	if err != nil {
		t.Fatalf("Failed to unmarshal persisted data: %v", err)
	}

	if loadedData.StanValue != 67890 {
		t.Errorf("Expected persisted StanValue 67890, got %d", loadedData.StanValue)
	}
}

func TestGetCounter(t *testing.T) {
	// Create temp directory and set initial value
	tempDir := t.TempDir()
	SetPersistenceDirectory(tempDir)

	counter := GetCounter()
	if counter == nil {
		t.Fatal("GetCounter returned nil")
	}

	// Test that we can call it multiple times
	counter2 := GetCounter()
	if counter != counter2 {
		t.Error("GetCounter is not returning singleton instance")
	}

	// Stop the worker to clean up
	defer StopPersistWorker()
}

func TestCounterGetStan(t *testing.T) {
	// Create a fresh counter for testing
	counter := &counter{value: 0}

	// Test initial STAN generation
	stan1 := counter.GetStan()
	if len(stan1) != 6 {
		t.Errorf("Expected STAN length 6, got %d", len(stan1))
	}
	if stan1 == "000000" {
		t.Error("First STAN should not be 000000")
	}

	// Test sequential STAN generation
	stan2 := counter.GetStan()
	stan1Int := parseStanToInt(stan1)
	stan2Int := parseStanToInt(stan2)

	if stan2Int != stan1Int+1 {
		t.Errorf("Expected STAN to increment by 1, got %d -> %d", stan1Int, stan2Int)
	}

	// Test rollover at 999999
	counter.value = 999999
	stanRollover := counter.GetStan()
	if stanRollover != "000001" {
		t.Errorf("Expected rollover to 000001, got %s", stanRollover)
	}
}

func TestCounterGetStanRollover(t *testing.T) {
	counter := &counter{value: 999999}

	// Test rollover behavior
	stan := counter.GetStan()
	if stan != "000001" {
		t.Errorf("Expected rollover STAN 000001, got %s", stan)
	}

	// Verify counter rolled over correctly
	if counter.value != 1 {
		t.Errorf("Expected counter value 1 after rollover, got %d", counter.value)
	}
}

func TestCounterGetStanZeroHandling(t *testing.T) {
	counter := &counter{value: 0}

	// Test that 0 becomes 1 (not 000000)
	stan := counter.GetStan()
	if stan == "000000" {
		t.Error("STAN should not be 000000")
	}
	if stan != "000001" {
		t.Errorf("Expected first STAN 000001, got %s", stan)
	}
}

// Helper function to parse STAN string to int for testing
func parseStanToInt(stan string) int {
	result := 0
	for _, digit := range stan {
		result = result*10 + int(digit-'0')
	}
	return result
}

func TestPersistWorker(t *testing.T) {
	// This is a basic test for persistWorker - testing goroutines is complex
	// We'll test that the channel and persistence work

	tempDir := t.TempDir()
	SetPersistenceDirectory(tempDir)

	// Create a channel like the real implementation
	testChan := make(chan uint32, 1)

	// Start a test version of persistWorker that exits after one operation
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		select {
		case val := <-testChan:
			persistData(PersistentData{StanValue: val})
		case <-ticker.C:
			// Timeout
		}
	}()

	// Send a value
	testChan <- 555

	// Give it time to process
	time.Sleep(200 * time.Millisecond)

	// Check if data was persisted
	data, err := loadPersistedData()
	if err != nil {
		t.Fatalf("Failed to load persisted data: %v", err)
	}

	if data.StanValue != 555 {
		t.Errorf("Expected persisted value 555, got %d", data.StanValue)
	}
}
