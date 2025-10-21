package cli

import (
	"testing"
)

func TestNewCLI(t *testing.T) {
	cli := NewCLI()
	if cli == nil {
		t.Fatal("NewCLI returned nil")
	}

	if cli.commands == nil {
		t.Error("commands map not initialized")
	}

	if cli.workers == nil {
		t.Error("workers map not initialized")
	}

	if cli.networkStats == nil {
		t.Error("networkStats not initialized")
	}
}

func TestAddCommand(t *testing.T) {
	cli := NewCLI()

	// Create a mock command
	mockCmd := &mockCommand{name: "test"}

	cli.AddCommand(mockCmd)

	if len(cli.commands) != 1 {
		t.Errorf("Expected 1 command, got %d", len(cli.commands))
	}

	if cli.commands["test"] != mockCmd {
		t.Error("Command not added correctly")
	}
}

// Mock command for testing
type mockCommand struct {
	name string
}

func (m *mockCommand) Name() string {
	return m.name
}

func (m *mockCommand) Synopsis() string {
	return "mock synopsis"
}

func (m *mockCommand) Execute() error {
	return nil
}
