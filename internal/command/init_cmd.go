package command

import (
	"fmt"
	"os"
	"path/filepath"

	"jiso/internal/command/templates"
)

type InitSpecCommand struct {
	OutputPath string
}

func (c *InitSpecCommand) Name() string {
	return "init-spec"
}

func (c *InitSpecCommand) Synopsis() string {
	return "Generate a default ISO8583 specification file"
}

func (c *InitSpecCommand) Execute() error {
	path := c.OutputPath
	if path == "" {
		path = "./specs/spec.json"
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, templates.DefaultSpecJSON, 0644); err != nil {
		return fmt.Errorf("failed to write spec file to %s: %w", path, err)
	}

	fmt.Printf("Default specification file generated at: %s\n", path)
	return nil
}

type InitTxCommand struct {
	OutputPath string
}

func (c *InitTxCommand) Name() string {
	return "init-tx"
}

func (c *InitTxCommand) Synopsis() string {
	return "Generate a comprehensive sample transaction configuration file"
}

func (c *InitTxCommand) Execute() error {
	path := c.OutputPath
	if path == "" {
		path = "./transactions/transaction.json"
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, templates.DefaultTransactionJSON, 0644); err != nil {
		return fmt.Errorf("failed to write transaction file to %s: %w", path, err)
	}

	fmt.Printf("Comprehensive sample transaction configuration file generated at: %s\n", path)
	return nil
}
