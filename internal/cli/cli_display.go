package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

type commandGroup struct {
	category string
	commands []string
}

// ClearTerminal clears the terminal output screen.
func (cli *CLI) ClearTerminal() {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "cls")
	} else {
		cmd = exec.Command("clear")
	}
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to clear terminal: %v\n", err)
	}
}

// PrintHelp prints available commands and their descriptions.
func (cli *CLI) PrintHelp() {
	cli.printHelp()
}

func (cli *CLI) printHelp() {
	groups := []commandGroup{
		{
			category: "🔌 Connection & Configuration Management",
			commands: []string{"connect", "disconnect", "target", "spec", "tx"},
		},
		{
			category: "💳 Transaction & Load Testing Execution",
			commands: []string{"send", "bgsend", "stress", "list", "info"},
		},
		{
			category: "🧪 Scenario & Test Automation",
			commands: []string{"scenario", "run-scenario"},
		},
		{
			category: "⚙️ Embedded Mock Server Subsystem",
			commands: []string{"serve"},
		},
		{
			category: "📊 Worker & Operational Management",
			commands: []string{"stats", "stop", "stop-all", "db-stats", "reload"},
		},
		{
			category: "📁 Scaffolding & Setup Utilities",
			commands: []string{"init-spec", "init-tx", "analyze"},
		},
		{
			category: "🛠️ General & Session Utilities",
			commands: []string{"help", "version", "clear", "exit"},
		},
	}

	aliases := map[string]string{
		"target":   "aliases: set",
		"spec":     "aliases: use-spec",
		"tx":       "aliases: use-tx, transaction",
		"scenario": "aliases: scenarios",
		"analyze":  "aliases: pcap",
		"stats":    "aliases: status",
		"help":     "aliases: h, ?",
		"version":  "aliases: v",
		"clear":    "aliases: cls",
		"exit":     "aliases: quit",
	}

	fmt.Println("================================================================================")
	fmt.Println("                         JISO CLI COMMAND REFERENCE")
	fmt.Println("================================================================================")

	for _, g := range groups {
		fmt.Printf("\n%s:\n", g.category)
		for _, name := range g.commands {
			if command, exists := cli.commands[name]; exists {
				aliasStr := ""
				if a, ok := aliases[name]; ok {
					aliasStr = fmt.Sprintf(" (%s)", a)
				}
				fmt.Printf("  %-16s - %s%s\n", name, command.Synopsis(), aliasStr)
			}
		}
	}
	fmt.Println("\n================================================================================")
}

// PrintVersion prints the version of the JISO CLI.
func (cli *CLI) PrintVersion() {
	cli.printVersion()
}

func (cli *CLI) printVersion() {
	fmt.Printf("JISO CLI (JSON ISO8583) tool version %s\n", Version)
	fmt.Println("(c) 2025 Andrey Babikov <andrei.babikov@gmail.com>")
}
