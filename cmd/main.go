package main

import (
	"fmt"
	"os"

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
	cli.ClearTerminal()

	if cfg.GetConfig().GetHost() == "" ||
		cfg.GetConfig().GetPort() == "" ||
		cfg.GetConfig().GetSpec() == "" ||
		cfg.GetConfig().GetFile() == "" {
		cli.AddCommand(&cmd.CollectArgsCommand{})
	}

	cli.AddCommand(&cmd.ExampleCommand{})
	err = cli.Run()
	if err != nil {
		fmt.Printf("Error running CLI: %s\n", err)
	}
}
