package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"jiso/internal/cli"
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

	args := flag.Args()
	if len(args) > 0 {
		subcommand := args[0]
		validSubcommands := map[string]bool{
			"init-spec":    true,
			"init-tx":      true,
			"scenarios":    true,
			"scenario":     true,
			"run-scenario": true,
			"serve":        true,
			"server":       true,
		}
		if validSubcommands[subcommand] {
			err := cliTool.RunDirectCommand(subcommand, args[1:])
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return 1
			}
			return 0
		} else {
			fmt.Printf("Unknown subcommand: %s. Available subcommands: init-spec, init-tx, scenarios, run-scenario, serve, server\n", subcommand)
			return 1
		}
	}

	cliTool.ClearTerminal()

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
