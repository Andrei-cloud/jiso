package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	cmd "jiso/internal/command"

	"github.com/chzyer/readline"
	"github.com/olekukonko/tablewriter"
)

type workerStats struct {
	totalRuns     int
	successRuns   int
	failedRuns    int
	lastRunTime   time.Time
	responseCodes map[string]uint64
	durations     []time.Duration
	errors        []string
}

// Legacy workerState for compatibility with existing code
// This will be replaced by the implementation in worker.go
type workerState struct {
	command      cmd.BgCommand
	interval     time.Duration
	done         chan struct{}
	stats        workerStats
	lastActivity time.Time
}

// runWithHistory runs the CLI with command history support
func (cli *CLI) runWithHistory() error {
	// Configure readline with history
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "jiso> ",
		HistoryFile:     "/tmp/jiso_history.txt",
		AutoComplete:    cli.completer(),
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		return err
	}
	defer rl.Close()

	// Print welcome message and available commands
	fmt.Printf("Welcome to JISO CLI %s\n", Version)
	fmt.Println("Type 'help' for available commands")

	// Main interaction loop
	for {
		line, err := rl.Readline()
		if err != nil { // io.EOF, readline.ErrInterrupt
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Process the command
		exit := cli.processCommand(line)
		if exit {
			break
		}
	}

	return nil
}

// processCommand handles a single command line input
func (cli *CLI) processCommand(line string) bool {
	parts := strings.Split(line, " ")
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "help", "h", "?":
		cli.printHelp()

	case "version", "v":
		cli.printVersion()

	case "clear", "cls":
		cli.ClearTerminal()

	case "quit", "exit":
		return true

	case "stats", "status":
		cli.printWorkerStats()

	case "stop-all":
		if err := cli.StopAllWorkers(); err != nil {
			fmt.Printf("Error stopping workers: %v\n", err)
		} else {
			fmt.Println("All workers stopped successfully")
		}

	case "reload":
		if err := cli.InitService(); err != nil {
			fmt.Printf("Error reloading: %v\n", err)
		} else {
			fmt.Println("Service reloaded successfully")
		}

	case "stop":
		if len(parts) < 2 {
			fmt.Println("Usage: stop <worker-id>")
			return false
		}
		if err := cli.StopWorker(parts[1]); err != nil {
			fmt.Printf("Error stopping worker: %v\n", err)
		} else {
			fmt.Printf("Worker %s stopped successfully\n", parts[1])
		}

	default:
		// Try to find a registered command
		if command, exists := cli.commands[cmd]; exists {
			if err := command.Execute(); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		} else {
			fmt.Printf("Unknown command: %s\n", cmd)
		}
	}

	return false
}

// completer provides tab completion for commands
func (cli *CLI) completer() readline.AutoCompleter {
	// Create a map of command names for auto-completion
	commands := []readline.PrefixCompleterInterface{
		readline.PcItem("help"),
		readline.PcItem("version"),
		readline.PcItem("clear"),
		readline.PcItem("quit"),
		readline.PcItem("exit"),
		readline.PcItem("stats"),
		readline.PcItem("status"),
		readline.PcItem("stop-all"),
		readline.PcItem("reload"),
		readline.PcItem("stop"),
	}

	// Add registered commands
	for name := range cli.commands {
		commands = append(commands, readline.PcItem(name))
	}

	return readline.NewPrefixCompleter(commands...)
}

// printWorkerStats prints current worker statistics
func (cli *CLI) printWorkerStats() {
	stats := cli.GetWorkerStats()
	fmt.Printf("Active workers: %d\n", stats["active"])

	workers, ok := stats["workers"].([]map[string]interface{})
	if !ok || len(workers) == 0 {
		fmt.Println("No active workers")
		return
	}

	// Create a table for better presentation
	table := tablewriter.NewWriter(os.Stdout)
	// table.SetHeader([]string{"Command", "Description"})
	for _, worker := range workers {
		// table.Append([]string{fmt.Sprintf("%v", worker["id"]), fmt.Sprintf("%v", worker["name"])})
		table.Append([]string{
			fmt.Sprintf("%v", worker["id"]),
			fmt.Sprintf("%v", worker["name"]),
			fmt.Sprintf("%v", worker["workers"]),
			fmt.Sprintf("%v", worker["interval"]),
			fmt.Sprintf("%v", worker["runtime"]),
			fmt.Sprintf("%v", worker["successful"]),
			fmt.Sprintf("%v", worker["failed"]),
			fmt.Sprintf("%v", worker["total"]),
		})
	}

	table.Render()
}
