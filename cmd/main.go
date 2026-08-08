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
	}()

	// Configure REPL runner callback for interactive mode
	clicmd.SetREPLRunner(func(replCtx context.Context) error {
		cliTool := cli.NewCLI()
		defer cliTool.Close()

		cliTool.ClearTerminal()
		errCh := make(chan error, 1)
		go func() {
			errCh <- cliTool.Run()
		}()

		select {
		case err := <-errCh:
			return err
		case <-replCtx.Done():
			return nil
		}
	})

	// Execute Cobra root command structure
	if err := clicmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
