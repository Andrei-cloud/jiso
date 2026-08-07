package cli

import (
	"fmt"
	"strings"
	"time"

	cmd "jiso/internal/command"
	cfg "jiso/internal/config"
	"jiso/internal/db"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
)

func (cli *CLI) InitService() error {
	// Validate configuration before creating service
	if err := cfg.GetConfig().Validate(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	svc, err := service.NewService(
		cfg.GetConfig().GetHost(),
		cfg.GetConfig().GetPort(),
		cfg.GetConfig().GetSpec(),
		true, // Enable debug mode for testing reflection
		cfg.GetConfig().GetReconnectAttempts(),
		cfg.GetConfig().GetConnectTimeout(),
		cfg.GetConfig().GetTotalConnectTimeout(),
		cfg.GetConfig().GetResponseTimeout(),
	)
	if err != nil {
		return err
	}

	cli.setService(svc)

	txPath := cfg.GetConfig().GetFile()
	if strings.TrimSpace(txPath) != "" && strings.TrimSpace(cfg.GetConfig().GetSpec()) == "" {
		return fmt.Errorf("specification must be defined before loading transaction file. Please select a specification using 'spec <path>' first")
	}

	// Create transaction collection through the repository interface
	tcInstance, err := transactions.NewTransactionCollection(
		txPath,
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
		// Initialize async logger
		db.InitAsyncLogger(1000, 50, 100*time.Millisecond)
	}

	return nil
}

// Prepare initializes the service and registers commands without starting the interactive shell
func (cli *CLI) Prepare() error {
	if err := cli.InitService(); err != nil {
		return err
	}

	// Create command factory and register commands
	cli.factory = cmd.NewFactory(cli.svc, cli.tc, cli.networkStats, cli)
	cli.registerAllCommands()

	return nil
}

// Connect establishes a connection with the specified length type (non-interactive)
func (cli *CLI) Connect(lengthType string) error {
	header, err := utils.SelectLength(lengthType)
	if err != nil {
		return err
	}

	naps := (lengthType == "NAPS")
	if err := cli.svc.Connect(naps, header); err != nil {
		return err
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
	}

	// Step 2: Close existing service if it exists
	if cli.svc != nil {
		fmt.Println("Closing existing service...")
		if err := cli.svc.Close(); err != nil {
			fmt.Printf("Warning: Failed to close service: %v\n", err)
		}
		cli.svc = nil
	}

	// Step 3: Close database connection
	fmt.Println("Closing database connection...")
	if err := db.Close(); err != nil {
		fmt.Printf("Warning: Failed to close database: %v\n", err)
	}

	// Step 4: Reinitialize service with new configuration
	fmt.Println("Reinitializing service...")
	if err := cli.InitService(); err != nil {
		return fmt.Errorf("failed to reinitialize service: %w", err)
	}

	// Step 5: Recreate command factory with new service and register commands
	fmt.Println("Updating command factory...")
	cli.factory = cmd.NewFactory(cli.svc, cli.tc, cli.networkStats, cli)
	cli.registerAllCommands()

	fmt.Println("Service reloaded successfully")
	return nil
}

// ReloadTransactions reloads the transaction repository with the given transaction file path
func (cli *CLI) ReloadTransactions(txPath string) (int, error) {
	specPath := strings.TrimSpace(cfg.GetConfig().GetSpec())
	if specPath == "" {
		return 0, fmt.Errorf("specification must be defined before loading transaction file. Please select a specification using 'spec <path>' first")
	}

	selectedSpec, err := utils.CreateSpecFromFile(specPath)
	if err != nil {
		return 0, fmt.Errorf("failed to load selected specification from '%s': %w", specPath, err)
	}

	var spec *iso8583.MessageSpec
	if cli.svc != nil {
		spec = cli.svc.GetSpec()
	}
	if spec == nil {
		spec = selectedSpec
	}

	tcInstance, err := transactions.NewTransactionCollection(txPath, spec)
	if err != nil {
		return 0, fmt.Errorf("failed to load transactions: %w", err)
	}

	cli.tc = tcInstance
	cli.factory = cmd.NewFactory(cli.svc, cli.tc, cli.networkStats, cli)
	cli.registerAllCommands()
	return len(tcInstance.ListNames()), nil
}

// Set service instance
func (cli *CLI) setService(svc *service.Service) {
	cli.svc = svc
}

// Get message spec from service
func (cli *CLI) getSpec() *iso8583.MessageSpec {
	return cli.svc.GetSpec()
}
