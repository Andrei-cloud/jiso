package cli

import (
	"testing"
)

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

func TestNewCLI(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	cli := NewCLI()

	// Create a mock command
	mockCmd := &mockCommand{name: "test"}

	if err := cli.AddCommand(mockCmd); err != nil {
		t.Fatalf("AddCommand failed: %v", err)
	}

	if len(cli.commands) != 1 {
		t.Errorf("Expected 1 command, got %d", len(cli.commands))
	}

	if cli.commands["test"] != mockCmd {
		t.Error("Command not added correctly")
	}
}
