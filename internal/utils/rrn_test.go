package utils

import (
	"testing"
)

func TestGetRRNInstance(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()
	SetPersistenceDirectory(tempDir)

	rrn := GetRRNInstance()
	if rrn == nil {
		t.Fatal("GetRRNInstance returned nil")
	}

	// Test singleton behavior
	rrn2 := GetRRNInstance()
	if rrn != rrn2 {
		t.Error("GetRRNInstance is not returning singleton instance")
	}
}

func TestRRNGetRRNFormat(t *testing.T) {
	rrn := &RRN{value: 0}

	// Test RRN format: YYDDDNNNNNNN (2+3+7=12 digits)
	rrnStr := rrn.GetRRN()
	if len(rrnStr) != 12 {
		t.Errorf("Expected RRN length 12, got %d: %s", len(rrnStr), rrnStr)
	}

	// Verify format components
	year := rrnStr[0:2]
	day := rrnStr[2:5]
	seq := rrnStr[5:12]

	if len(year) != 2 || len(day) != 3 || len(seq) != 7 {
		t.Errorf("Invalid RRN format: %s", rrnStr)
	}

	// Sequence should not be 0000000
	if seq == "0000000" {
		t.Errorf("RRN sequence should not be 0000000: %s", rrnStr)
	}
}

func TestRRNGetRRNSequential(t *testing.T) {
	rrn := &RRN{value: 0}

	// Generate a few RRNs and verify they increment
	rrn1 := rrn.GetRRN()
	rrn2 := rrn.GetRRN()

	// Extract sequence parts
	seq1 := rrn1[5:12]
	seq2 := rrn2[5:12]

	seq1Int := parseRRNSeqToInt(seq1)
	seq2Int := parseRRNSeqToInt(seq2)

	if seq2Int != seq1Int+1 {
		t.Errorf("Expected RRN sequence to increment by 1, got %d -> %d", seq1Int, seq2Int)
	}
}

func TestRRNGetRRNCyclicBehavior(t *testing.T) {
	rrn := &RRN{value: 9999998}

	// Generate RRNs around the rollover point
	rrn1 := rrn.GetRRN() // Should have sequence 9999999
	rrn2 := rrn.GetRRN() // Should rollover to 0000001
	rrn3 := rrn.GetRRN() // Should be 0000002

	seq1 := rrn1[5:12]
	seq2 := rrn2[5:12]
	seq3 := rrn3[5:12]

	if seq1 != "9999999" {
		t.Errorf("Expected sequence 9999999, got %s", seq1)
	}
	if seq2 != "0000001" {
		t.Errorf("Expected rollover sequence 0000001, got %s", seq2)
	}
	if seq3 != "0000002" {
		t.Errorf("Expected sequence 0000002, got %s", seq3)
	}
}

func TestRRNGetRRNNoDuplicates(t *testing.T) {
	rrn := &RRN{value: 0}

	// Generate many RRNs and ensure no duplicate sequences
	generated := make(map[string]bool)
	for i := 0; i < 50; i++ {
		rrnStr := rrn.GetRRN()
		seq := rrnStr[5:12] // Extract sequence part

		if generated[seq] {
			t.Errorf("Duplicate RRN sequence generated: %s", seq)
		}
		generated[seq] = true

		// Verify sequence format
		if len(seq) != 7 {
			t.Errorf("Invalid RRN sequence length: %s", seq)
		}
		if seq == "0000000" {
			t.Errorf("Invalid RRN sequence value: %s", seq)
		}
	}
}

func TestRRNGetRRNConcurrentSafety(t *testing.T) {
	rrn := &RRN{value: 0}

	// Test concurrent access
	const numGoroutines = 5
	const numsPerGoroutine = 20

	results := make(chan string, numGoroutines*numsPerGoroutine)

	// Start multiple goroutines generating RRNs
	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < numsPerGoroutine; j++ {
				results <- rrn.GetRRN()
			}
		}()
	}

	// Collect results
	generated := make(map[string]int)
	for i := 0; i < numGoroutines*numsPerGoroutine; i++ {
		rrnStr := <-results
		seq := rrnStr[5:12]
		generated[seq]++

		// Verify format
		if len(rrnStr) != 12 {
			t.Errorf("Invalid RRN length: %s", rrnStr)
		}
		if seq == "0000000" {
			t.Errorf("Invalid RRN sequence: %s", seq)
		}
	}

	// Verify no duplicates
	for seq, count := range generated {
		if count > 1 {
			t.Errorf("RRN sequence %s generated %d times (should be 1)", seq, count)
		}
	}

	// Should have generated exactly the expected number of unique sequences
	if len(generated) != numGoroutines*numsPerGoroutine {
		t.Errorf(
			"Expected %d unique RRN sequences, got %d",
			numGoroutines*numsPerGoroutine,
			len(generated),
		)
	}
}

func TestRRNGetRRNZeroHandling(t *testing.T) {
	rrn := &RRN{value: 0}

	// Test that 0 becomes 1 (not 0000000)
	rrnStr := rrn.GetRRN()
	seq := rrnStr[5:12]
	if seq == "0000000" {
		t.Error("RRN sequence should not be 0000000")
	}
	if seq != "0000001" {
		t.Errorf("Expected first RRN sequence 0000001, got %s", seq)
	}
}

func TestRRNPersistence(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()
	SetPersistenceDirectory(tempDir)

	// Test persistence
	testData := RRNPersistentData{RRNValue: 12345}
	err := persistRRNData(testData)
	if err != nil {
		t.Fatalf("persistRRNData failed: %v", err)
	}

	// Test loading
	data, err := loadPersistedRRNData()
	if err != nil {
		t.Fatalf("loadPersistedRRNData failed: %v", err)
	}

	if data.RRNValue != 12345 {
		t.Errorf("Expected persisted RRNValue 12345, got %d", data.RRNValue)
	}
}

// Helper function to parse RRN sequence string to int for testing
func parseRRNSeqToInt(seq string) int {
	result := 0
	for _, digit := range seq {
		result = result*10 + int(digit-'0')
	}
	return result
}
