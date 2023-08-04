package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"jiso/internal/cli"
	cmd "jiso/internal/command"
	cfg "jiso/internal/config"
)

func main() {
	err := cfg.GetConfig().Parse()
	if err != nil {
		fmt.Printf("Error parsing config: %s\n", err)
		os.Exit(1)
	}

	cli := cli.NewCLI()

	// Handle kill and interrupt signals to close the service's connection gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cli.Close()
		fmt.Println("Exiting CLI tool")
		os.Exit(0)
	}()

	cli.ClearTerminal()

	if cfg.GetConfig().GetHost() == "" ||
		cfg.GetConfig().GetPort() == "" ||
		cfg.GetConfig().GetSpec() == "" ||
		cfg.GetConfig().GetFile() == "" {
		cli.AddCommand(&cmd.CollectArgsCommand{})
	}

	err = cli.Run()
	if err != nil {
		fmt.Printf("Error running CLI: %s\n", err)
	}
}
