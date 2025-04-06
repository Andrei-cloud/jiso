package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"jiso/internal/cli"
	cmd "jiso/internal/command"
	cfg "jiso/internal/config"
)

func main() {
	// Create a cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutdown signal received")
		cancel() // Cancel context to propagate shutdown
	}()

	// Create and configure CLI
	cliTool := cli.NewCLI()
	defer cliTool.Close() // Ensure cleanup happens on all exit paths

	// Clear terminal and run application
	exitCode := runApp(ctx, cliTool)
	os.Exit(exitCode)
}

func runApp(ctx context.Context, cliTool *cli.CLI) int {
	// Parse configuration
	err := cfg.GetConfig().Parse()
	if err != nil {
		fmt.Printf("Error parsing config: %s\n", err)
		return 1
	}

	cliTool.ClearTerminal()

	// Add collect args command if config is incomplete
	if !validateConfig() {
		cliTool.AddCommand(&cmd.CollectArgsCommand{})
	}

	// Run the CLI with context awareness
	errCh := make(chan error, 1)
	go func() {
		errCh <- cliTool.Run()
	}()

	// Wait for either completion or cancellation
	select {
	case err := <-errCh:
		if err != nil {
			fmt.Printf("Error running CLI: %s\n", err)
			return 1
		}
		return 0
	case <-ctx.Done():
		fmt.Println("Exiting CLI tool")
		return 0
	}
}

func validateConfig() bool {
	config := cfg.GetConfig()
	return config.GetHost() != "" &&
		config.GetPort() != "" &&
		config.GetSpec() != "" &&
		config.GetFile() != ""
}
