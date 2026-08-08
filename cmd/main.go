package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"jiso/internal/cli"
	clicmd "jiso/internal/cli/cmd"
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
		cancel()
		os.Exit(0)
	}()

	// Configure REPL runner callback for interactive mode
	clicmd.SetREPLRunner(func(replCtx context.Context) error {
		cliTool := cli.NewCLI()
		defer cliTool.Close()

		cliTool.ClearTerminal()
		err := cliTool.Run()
		os.Exit(0)
		return err
	})

	// Execute Cobra root command structure
	if err := clicmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	os.Exit(0)
}
