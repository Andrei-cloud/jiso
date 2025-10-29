package cli

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync"

	cmd "jiso/internal/command"
	cfg "jiso/internal/config"
	"jiso/internal/db"
	"jiso/internal/metrics"
	"jiso/internal/service"
	"jiso/internal/transactions"

	"github.com/moov-io/iso8583"
)

var Version string = "v0.4.0"

type CLI struct {
	commands map[string]cmd.Command
	svc      *service.Service
	tc       transactions.Repository
	factory  *cmd.Factory

	// Add configuration options
	config struct {
		debugMode   bool
		logLevel    string
		autoConnect bool
	}

	// Background worker state
	workers       map[string]*workerInfo
	stressWorkers map[string]*stressTestWorker
	networkStats  *metrics.NetworkingStats
	mu            sync.Mutex
}

func NewCLI() *CLI {
	return &CLI{
		commands:      make(map[string]cmd.Command),
		workers:       make(map[string]*workerInfo),
		stressWorkers: make(map[string]*stressTestWorker),
		networkStats:  metrics.NewNetworkingStats(),
	}
}

func (cli *CLI) AddCommand(command cmd.Command) {
	if _, exists := cli.commands[command.Name()]; exists {
		log.Fatalf("Command '%s' is already registered", command.Name())
	}
	cli.commands[command.Name()] = command
}

func (cli *CLI) Run() error {
	if collectArgsCommand, ok := cli.commands["collect-args"]; ok {
		err := collectArgsCommand.Execute()
		if err != nil {
			return err
		}
	}
	err := cli.InitService()
	if err != nil {
		return err
	}

	// Create command factory
	cli.factory = cmd.NewFactory(cli.svc, cli.tc, cli.networkStats, cli)

	// Add commands using the factory
	cli.AddCommand(cli.factory.CreateListCommand())
	cli.AddCommand(cli.factory.CreateInfoCommand())
	cli.AddCommand(cli.factory.CreateSendCommand())
	cli.AddCommand(cli.factory.CreateConnectCommand())
	cli.AddCommand(cli.factory.CreateDisconnectCommand())
	cli.AddCommand(cli.factory.CreateBackgroundCommand())
	cli.AddCommand(cli.factory.CreateStressTestCommand())
	cli.AddCommand(cli.factory.CreateDbStatsCommand())

	return cli.runWithHistory()
}

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

func (cli *CLI) Close() {
	cli.mu.Lock()
	defer cli.mu.Unlock()
	for _, worker := range cli.workers {
		worker.cancel()
	}
	for _, stressWorker := range cli.stressWorkers {
		stressWorker.cancel()
	}

	if cli.svc != nil {
		cli.svc.Close()
	}

	// Close database connection
	db.Close()
}

func (cli *CLI) printHelp() {
	fmt.Println("JISO CLI Commands:")
	fmt.Println("  help, h, ?     - Display this help message")
	fmt.Println("  version, v     - Display version information")
	fmt.Println("  clear, cls     - Clear terminal")
	fmt.Println("  quit, exit     - Exit the program")
	fmt.Println("  stats, status  - Show worker statistics")
	fmt.Println("  stop-all       - Stop all background workers")
	fmt.Println("  reload         - Reload transaction specification and connection")
	fmt.Println("  stop           - Stop a specific worker")
	fmt.Println("")

	if len(cli.commands) > 0 {
		fmt.Println("Available commands:")
		for name, cmd := range cli.commands {
			fmt.Printf("  %-14s - %s\n", name, cmd.Synopsis())
		}
	}
}

func (cli *CLI) printVersion() {
	fmt.Printf("JISO CLI (JSON ISO8583) tool version %s\n", Version)
	fmt.Println("(c) 2025 Andrey Babikov <andrei.babikov@gmail.com>")
}

func (cli *CLI) InitService() error {
	svc, err := service.NewService(
		cfg.GetConfig().GetHost(),
		cfg.GetConfig().GetPort(),
		cfg.GetConfig().GetSpec(),
		true, // Enable debug mode for testing reflection
		cfg.GetConfig().GetReconnectAttempts(),
		cfg.GetConfig().GetConnectTimeout(),
		cfg.GetConfig().GetTotalConnectTimeout(),
	)
	if err != nil {
		return err
	}

	cli.setService(svc)

	// Create transaction collection through the repository interface
	tcInstance, err := transactions.NewTransactionCollection(
		cfg.GetConfig().GetFile(),
		cli.getSpec(),
	)
	if err != nil {
		return err
	}

	cli.tc = tcInstance

	// Initialize database
	dbPath := cfg.GetConfig().GetDbPath()
	if dbPath != "" {
		if err := db.InitDB(dbPath); err != nil {
			return fmt.Errorf("failed to initialize database: %w", err)
		}
	}

	return nil
}

// Reload reloads the service and transaction specifications
func (cli *CLI) Reload() error {
	fmt.Println("Reloading service...")

	// Step 1: Stop all background workers
	fmt.Println("Stopping all workers...")
	if err := cli.StopAllWorkers(); err != nil {
		fmt.Printf("Warning: Failed to stop all workers: %v\n", err)
		// Continue with reload even if worker stopping fails
	}

	// Step 2: Close existing service if it exists
	if cli.svc != nil {
		fmt.Println("Closing existing service...")
		if err := cli.svc.Close(); err != nil {
			fmt.Printf("Warning: Failed to close service: %v\n", err)
			// Continue with reload
		}
		cli.svc = nil
	}

	// Step 3: Close database connection
	fmt.Println("Closing database connection...")
	if err := db.Close(); err != nil {
		fmt.Printf("Warning: Failed to close database: %v\n", err)
		// Continue with reload
	}

	// Step 4: Reinitialize service with new configuration
	fmt.Println("Reinitializing service...")
	if err := cli.InitService(); err != nil {
		return fmt.Errorf("failed to reinitialize service: %w", err)
	}

	// Step 5: Recreate command factory with new service
	fmt.Println("Updating command factory...")
	cli.factory = cmd.NewFactory(cli.svc, cli.tc, cli.networkStats, cli)

	// Step 6: Re-add commands (in case the factory changed)
	cli.commands = make(map[string]cmd.Command) // Clear existing commands
	cli.AddCommand(cli.factory.CreateListCommand())
	cli.AddCommand(cli.factory.CreateInfoCommand())
	cli.AddCommand(cli.factory.CreateSendCommand())
	cli.AddCommand(cli.factory.CreateConnectCommand())
	cli.AddCommand(cli.factory.CreateDisconnectCommand())
	cli.AddCommand(cli.factory.CreateBackgroundCommand())
	cli.AddCommand(cli.factory.CreateStressTestCommand())
	cli.AddCommand(cli.factory.CreateDbStatsCommand())

	fmt.Println("Service reloaded successfully")
	return nil
}

// Set service instance
func (cli *CLI) setService(svc *service.Service) {
	cli.svc = svc
}

// Get message spec from service
func (cli *CLI) getSpec() *iso8583.MessageSpec {
	return cli.svc.GetSpec()
}

// Add a configuration method
func (cli *CLI) Configure(debugMode bool, logLevel string, autoConnect bool) {
	cli.config.debugMode = debugMode
	cli.config.logLevel = logLevel
	cli.config.autoConnect = autoConnect
}
