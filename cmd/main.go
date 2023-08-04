package main

import (
	"fmt"
	"os"

	"jiso/internal/cli"
	cmd "jiso/internal/command"
	"jiso/internal/config"
)

func main() {
	cfg, err := config.Parse()
	if err != nil {
		fmt.Printf("Error parsing config: %s\n", err)
		os.Exit(1)
	}

	cli := cli.NewCLI()

	if cfg.Host == "" || cfg.Port == "" || cfg.SpecFileName == "" || cfg.File == "" {
		cli.AddCommand(&cmd.CollectArgsCommand{
			Host:         &cfg.Host,
			Port:         &cfg.Port,
			SpecFileName: &cfg.SpecFileName,
			File:         &cfg.File,
		})
	}

	cli.AddCommand(&cmd.ExampleCommand{})
	err = cli.Run()
	if err != nil {
		fmt.Printf("Error running CLI: %s\n", err)
	}
}
