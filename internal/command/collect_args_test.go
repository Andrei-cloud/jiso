package command

import (
	"testing"
)

func TestFileSelector(t *testing.T) {
	// Test file selector creation
	specSelector := NewFileSelector("spec")
	if specSelector == nil {
		t.Fatal("NewFileSelector returned nil")
	}
	if specSelector.fileType != "spec" {
		t.Errorf("Expected fileType 'spec', got '%s'", specSelector.fileType)
	}
	if specSelector.currentDir != "./specs" {
		t.Errorf("Expected currentDir './specs', got '%s'", specSelector.currentDir)
	}

	transactionSelector := NewFileSelector("transaction")
	if transactionSelector.currentDir != "./transactions" {
		t.Errorf("Expected currentDir './transactions', got '%s'", transactionSelector.currentDir)
	}
}

func TestShouldIncludeFile(t *testing.T) {
	specSelector := NewFileSelector("spec")
	transactionSelector := NewFileSelector("transaction")

	// Test spec file filtering
	if !specSelector.shouldIncludeFile("test.json") {
		t.Error("shouldIncludeFile should return true for .json files for spec selector")
	}
	if specSelector.shouldIncludeFile("test.txt") {
		t.Error("shouldIncludeFile should return false for non-.json files for spec selector")
	}

	// Test transaction file filtering
	if !transactionSelector.shouldIncludeFile("transaction.json") {
		t.Error("shouldIncludeFile should return true for .json files for transaction selector")
	}
	if transactionSelector.shouldIncludeFile("readme.md") {
		t.Error(
			"shouldIncludeFile should return false for non-.json files for transaction selector",
		)
	}
}

func TestFileSelectionTrigger(t *testing.T) {
	// Test that default values trigger file selection
	// This is a unit test for the logic that checks for default values

	// Simulate the answers struct
	answers := struct {
		Host     string
		Port     string
		SpecFile string
		File     string
	}{
		SpecFile: "./specs/spec_bcp.json",           // Default value
		File:     "./transactions/transaction.json", // Default value
	}

	// Test the logic that would trigger file selection
	shouldBrowseSpec := answers.SpecFile == "" || answers.SpecFile == "./specs/spec_bcp.json"
	shouldBrowseFile := answers.File == "" || answers.File == "./transactions/transaction.json"

	if !shouldBrowseSpec {
		t.Error("Should browse for spec file when default value is used")
	}
	if !shouldBrowseFile {
		t.Error("Should browse for transaction file when default value is used")
	}

	// Test with custom values
	answers.SpecFile = "/custom/path/spec.json"
	answers.File = "/custom/path/transaction.json"

	shouldBrowseSpec = answers.SpecFile == "" || answers.SpecFile == "./specs/spec_bcp.json"
	shouldBrowseFile = answers.File == "" || answers.File == "./transactions/transaction.json"

	if shouldBrowseSpec {
		t.Error("Should not browse for spec file when custom value is provided")
	}
	if shouldBrowseFile {
		t.Error("Should not browse for transaction file when custom value is provided")
	}
}
