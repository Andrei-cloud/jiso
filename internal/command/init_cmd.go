package command

import (
	"fmt"
	"os"
	"path/filepath"

	"jiso/internal/command/templates"
)

// Default output paths used when no explicit path is given; exported so the
// cobra --dry-run plans name the same files the writers would create.
const (
	DefaultSpecInitPath = "./specs/spec.json"
	DefaultTxInitPath   = "./transactions/transaction.json"
)

// InitSpecCommand writes a starter message spec to OutputPath. It is the answer to
// "I installed jiso and have no spec" -- a file an operator can open and edit, not
// a documented example they have to copy from a manual.
type InitSpecCommand struct {
	OutputPath string
}

// InitTxCommand writes a starter transaction file to OutputPath, paired with
// InitSpecCommand so the two files a first run needs are one command each.
type InitTxCommand struct {
	OutputPath string
}

// Execute writes the default spec, creating the parent directory and defaulting to
// ./specs/spec.json when no path was given.
func (c *InitSpecCommand) Execute() error {
	path := c.OutputPath
	if path == "" {
		path = "./specs/spec.json"
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, templates.DefaultSpecJSON, 0o644); err != nil {
		return fmt.Errorf("failed to write spec file to %s: %w", path, err)
	}

	fmt.Printf("Default specification file generated at: %s\n", path)

	return nil
}

// Execute writes the starter transaction file, creating the parent directory and
// defaulting to ./transactions/transaction.json when no path was given.
func (c *InitTxCommand) Execute() error {
	path := c.OutputPath
	if path == "" {
		path = "./transactions/transaction.json"
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, templates.DefaultTransactionJSON, 0o644); err != nil {
		return fmt.Errorf("failed to write transaction file to %s: %w", path, err)
	}

	fmt.Printf("Comprehensive sample transaction configuration file generated at: %s\n", path)

	return nil
}
