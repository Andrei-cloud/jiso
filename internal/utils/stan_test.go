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

func TestCounterGetStanCyclicBehavior(t *testing.T) {
	counter := &counter{value: 999998}

	// Generate STANs around the rollover point
	stan1 := counter.GetStan() // Should be 999999
	stan2 := counter.GetStan() // Should rollover to 000001
	stan3 := counter.GetStan() // Should be 000002

	if stan1 != "999999" {
		t.Errorf("Expected STAN 999999, got %s", stan1)
	}
	if stan2 != "000001" {
		t.Errorf("Expected rollover STAN 000001, got %s", stan2)
	}
	if stan3 != "000002" {
		t.Errorf("Expected STAN 000002, got %s", stan3)
	}
}

func TestCounterGetStanNoDuplicates(t *testing.T) {
	counter := &counter{value: 0}

	// Generate many STANs and ensure no duplicates
	generated := make(map[string]bool)
	for i := 0; i < 100; i++ {
		stan := counter.GetStan()
		if generated[stan] {
			t.Errorf("Duplicate STAN generated: %s", stan)
		}
		generated[stan] = true

		// Verify format
		if len(stan) != 6 {
			t.Errorf("Invalid STAN length: %s", stan)
		}
		if stan == "000000" {
			t.Errorf("Invalid STAN value: %s", stan)
		}
	}
}

func TestCounterGetStanConcurrentSafety(t *testing.T) {
	counter := &counter{value: 0}

	// Test concurrent access
	const numGoroutines = 10
	const numsPerGoroutine = 100

	results := make(chan string, numGoroutines*numsPerGoroutine)

	// Start multiple goroutines generating STANs
	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < numsPerGoroutine; j++ {
				results <- counter.GetStan()
			}
		}()
	}

	// Collect results
	generated := make(map[string]int)
	for i := 0; i < numGoroutines*numsPerGoroutine; i++ {
		stan := <-results
		generated[stan]++

		// Verify format
		if len(stan) != 6 {
			t.Errorf("Invalid STAN length: %s", stan)
		}
		if stan == "000000" {
			t.Errorf("Invalid STAN value: %s", stan)
		}
	}

	// Verify no duplicates
	for stan, count := range generated {
		if count > 1 {
			t.Errorf("STAN %s generated %d times (should be 1)", stan, count)
		}
	}

	// Should have generated exactly 1000 unique STANs
	if len(generated) != numGoroutines*numsPerGoroutine {
		t.Errorf("Expected %d unique STANs, got %d", numGoroutines*numsPerGoroutine, len(generated))
	}
}

func TestCounterGetStanRolloverConcurrent(t *testing.T) {
	// Test rollover under concurrent load
	counter := &counter{value: 999990}

	const numGoroutines = 20
	results := make(chan string, numGoroutines)

	// Start goroutines that will trigger rollover
	for i := 0; i < numGoroutines; i++ {
		go func() {
			results <- counter.GetStan()
		}()
	}

	// Collect results
	generated := make(map[string]bool)
	for i := 0; i < numGoroutines; i++ {
		stan := <-results
		if generated[stan] {
			t.Errorf("Duplicate STAN during rollover: %s", stan)
		}
		generated[stan] = true

		// All should be valid (either 99999x or 00000x)
		if len(stan) != 6 {
			t.Errorf("Invalid STAN length during rollover: %s", stan)
		}
		if stan == "000000" {
			t.Errorf("Invalid STAN value during rollover: %s", stan)
		}
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
