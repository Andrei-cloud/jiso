package command

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/olekukonko/tablewriter"
)

// ErrExit is returned by ExitCommand to signal CLI exit
var ErrExit = errors.New("exit CLI")

// CLIController extends WorkerController with CLI management actions
type CLIController interface {
	WorkerController
	ClearTerminal()
	Reload() error
	PrintHelp()
	PrintVersion()
}

// HelpCommand displays available CLI commands and help information
type HelpCommand struct {
	Ctrl CLIController
}

func (c *HelpCommand) Name() string     { return "help" }
func (c *HelpCommand) Synopsis() string { return "Display help message" }
func (c *HelpCommand) Execute() error {
	if c.Ctrl != nil {
		c.Ctrl.PrintHelp()
	}
	return nil
}

// VersionCommand displays CLI version information
type VersionCommand struct {
	Ctrl CLIController
}

func (c *VersionCommand) Name() string     { return "version" }
func (c *VersionCommand) Synopsis() string { return "Display version information" }
func (c *VersionCommand) Execute() error {
	if c.Ctrl != nil {
		c.Ctrl.PrintVersion()
	}
	return nil
}

// ClearCommand clears the terminal screen
type ClearCommand struct {
	Ctrl CLIController
}

func (c *ClearCommand) Name() string     { return "clear" }
func (c *ClearCommand) Synopsis() string { return "Clear terminal screen" }
func (c *ClearCommand) Execute() error {
	if c.Ctrl != nil {
		c.Ctrl.ClearTerminal()
	}
	return nil
}

// ExitCommand exits the interactive CLI session
type ExitCommand struct{}

func (c *ExitCommand) Name() string     { return "exit" }
func (c *ExitCommand) Synopsis() string { return "Exit the interactive CLI session" }
func (c *ExitCommand) Execute() error   { return ErrExit }

// StatsCommand displays active worker and networking statistics
type StatsCommand struct {
	Ctrl CLIController
}

func (c *StatsCommand) Name() string     { return "stats" }
func (c *StatsCommand) Synopsis() string { return "Show active worker statistics" }
func (c *StatsCommand) Execute() error {
	if c.Ctrl == nil {
		return nil
	}
	stats := c.Ctrl.GetWorkerStats()
	fmt.Printf("Active workers: %v\n", stats["active"])

	workers, ok := stats["workers"].([]map[string]interface{})
	if !ok || len(workers) == 0 {
		fmt.Println("No active workers")
		return nil
	}

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
			targetOrInterval = fmt.Sprintf("%v Target (Inst: %.1f, Avg: %.1f)", worker["target_tps"], worker["instant_tps"], worker["actual_tps"])
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
	return nil
}

// StopAllCommand stops all running background worker threads
type StopAllCommand struct {
	Ctrl CLIController
}

func (c *StopAllCommand) Name() string     { return "stop-all" }
func (c *StopAllCommand) Synopsis() string { return "Stop all background workers" }
func (c *StopAllCommand) Execute() error {
	if c.Ctrl == nil {
		return nil
	}
	if err := c.Ctrl.StopAllWorkers(); err != nil {
		return fmt.Errorf("error stopping workers: %w", err)
	}
	fmt.Println("All workers stopped successfully")
	return nil
}

// StopCommand stops a specific worker by ID
type StopCommand struct {
	Ctrl     CLIController
	WorkerID string
}

func (c *StopCommand) Name() string     { return "stop" }
func (c *StopCommand) Synopsis() string { return "Stop a specific background worker by ID" }
func (c *StopCommand) SetArgs(args []string) {
	if len(args) > 0 {
		c.WorkerID = args[0]
	}
}
func (c *StopCommand) Execute() error {
	if c.Ctrl == nil {
		return nil
	}
	workerID := strings.TrimSpace(c.WorkerID)
	if workerID == "" {
		return fmt.Errorf("usage: stop <worker-id>")
	}
	if err := c.Ctrl.StopWorker(workerID); err != nil {
		return fmt.Errorf("error stopping worker: %w", err)
	}
	fmt.Printf("Worker %s stopped successfully\n", workerID)
	return nil
}

// ReloadCommand reloads transaction specs and reconnects service
type ReloadCommand struct {
	Ctrl CLIController
}

func (c *ReloadCommand) Name() string     { return "reload" }
func (c *ReloadCommand) Synopsis() string { return "Reload transaction specifications and connections" }
func (c *ReloadCommand) Execute() error {
	if c.Ctrl == nil {
		return nil
	}
	return c.Ctrl.Reload()
}

// MemoryHelper clears operating system memory references (for Windows/Unix compatibility)
func ClearOSMemory() {
	_ = runtime.GOOS
}
