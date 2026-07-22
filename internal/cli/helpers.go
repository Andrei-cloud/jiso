package cli

import (
	"errors"
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

	// Print welcome message
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

		// Process the command via registered command objects
		exit := cli.processCommand(line)
		if exit {
			break
		}
	}

	return nil
}

// processCommand handles command line inputs dynamically without hardcoded switch-case blocks
func (cli *CLI) processCommand(line string) bool {
	args, err := cli.lexer.Tokenize(line)
	if err != nil {
		fmt.Printf("Syntax error: %v\n", err)
		return false
	}
	if len(args) == 0 {
		return false
	}

	cmdName := strings.ToLower(args[0])
	command, exists := cli.commands[cmdName]
	if !exists {
		fmt.Printf("Unknown command: %s. Type 'help' for available commands\n", cmdName)
		return false
	}

	// Inject arguments into commands supporting dynamic parameter binding
	if sa, ok := command.(interface{ SetArgs([]string) }); ok {
		sa.SetArgs(args[1:])
	} else if rsc, ok := command.(*cmd.RunScenarioCommand); ok {
		if len(args) > 1 {
			rsc.ScenarioName = args[1]
		} else {
			rsc.ScenarioName = ""
		}
	} else if isc, ok := command.(*cmd.InitSpecCommand); ok {
		if len(args) > 1 {
			isc.OutputPath = args[1]
		} else {
			isc.OutputPath = ""
		}
	} else if itc, ok := command.(*cmd.InitTxCommand); ok {
		if len(args) > 1 {
			itc.OutputPath = args[1]
		} else {
			itc.OutputPath = ""
		}
	} else if dbsc, ok := command.(*cmd.DbStatsCommand); ok {
		if len(args) > 1 {
			dbsc.SessionID = args[1]
		} else {
			dbsc.SessionID = ""
		}
	} else if tcCmd, ok := command.(*cmd.TargetCommand); ok {
		if len(args) > 1 {
			if err := tcCmd.SetTarget(args[1]); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
			return false
		}
	} else if specCmd, ok := command.(*cmd.SpecCommand); ok {
		if len(args) > 1 {
			specCmd.SpecPath = args[1]
		} else {
			specCmd.SpecPath = ""
		}
	} else if txCmd, ok := command.(*cmd.TxCommand); ok {
		if len(args) > 1 {
			txCmd.TxPath = args[1]
		} else {
			txCmd.TxPath = ""
		}
	}

	execErr := command.Execute()
	if errors.Is(execErr, cmd.ErrExit) {
		return true // Exit REPL loop
	}
	if execErr != nil {
		fmt.Printf("Error: %v\n", execErr)
	}

	return false
}

// completer provides tab completion for commands registered in the command map
func (cli *CLI) completer() readline.AutoCompleter {
	commands := make([]readline.PrefixCompleterInterface, 0, len(cli.commands))
	for name := range cli.commands {
		commands = append(commands, readline.PcItem(name))
	}
	return readline.NewPrefixCompleter(commands...)
}

// printWorkerStats prints current worker statistics
func (cli *CLI) printWorkerStats() {
	stats := cli.GetWorkerStats()
	fmt.Printf("Active workers: %v\n", stats["active"])

	workers, ok := stats["workers"].([]map[string]interface{})
	if !ok || len(workers) == 0 {
		fmt.Println("No active workers")
	} else {
		table := tablewriter.NewWriter(os.Stdout)
		table.Header("ID", "Type", "Transaction", "Status", "Workers", "Interval / Target TPS", "Runtime", "Success / Failed")
		for _, worker := range workers {
			typeStr := fmt.Sprintf("%v", worker["type"])
			statusStr := "running"
			if s, ok := worker["status"]; ok {
				statusStr = fmt.Sprintf("%v", s)
			}

			var targetOrInterval string
			if typeStr == "stress_test" {
				targetOrInterval = fmt.Sprintf("%v TPS (Current: %.1f)", worker["target_tps"], worker["current_tps"])
			} else {
				targetOrInterval = fmt.Sprintf("%v", worker["interval"])
			}

			table.Append([]string{
				fmt.Sprintf("%v", worker["id"]),
				typeStr,
				fmt.Sprintf("%v", worker["name"]),
				statusStr,
				fmt.Sprintf("%v", worker["workers"]),
				targetOrInterval,
				fmt.Sprintf("%v", worker["runtime"]),
				fmt.Sprintf("%v / %v", worker["successful"], worker["failed"]),
			})
		}
		table.Render()
	}

	fmt.Println("\nNetworking Statistics:")
	if cli.networkStats != nil {
		netStats := cli.networkStats.GetAllMetrics()
		for key, value := range netStats {
			fmt.Printf("  %-30s: %v\n", key, value)
		}
	} else {
		fmt.Println("  Networking stats not available")
	}
}
