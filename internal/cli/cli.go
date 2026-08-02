package cli

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	cmd "jiso/internal/command"
	cfg "jiso/internal/config"
	"jiso/internal/db"
	"jiso/internal/metrics"
	"jiso/internal/repl/core"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
)

var Version string = "v1.4.5"

type CLI struct {
	commands map[string]cmd.Command
	lexer    *core.Lexer
	svc      *service.Service
	tc       transactions.Repository
	factory  *cmd.Factory

	// Background worker state
	workers       map[string]*workerInfo
	stressWorkers map[string]*stressTestWorker
	networkStats  *metrics.NetworkingStats
	mu            sync.Mutex
}

func NewCLI() *CLI {
	return &CLI{
		commands:      make(map[string]cmd.Command),
		lexer:         core.NewLexer(),
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

func (cli *CLI) registerAllCommands() {
	cli.commands = make(map[string]cmd.Command)

	// Helper commands
	helpCmd := cli.factory.CreateHelpCommand()
	cli.commands["help"] = helpCmd
	cli.commands["h"] = helpCmd
	cli.commands["?"] = helpCmd

	versionCmd := cli.factory.CreateVersionCommand()
	cli.commands["version"] = versionCmd
	cli.commands["v"] = versionCmd

	clearCmd := cli.factory.CreateClearCommand()
	cli.commands["clear"] = clearCmd
	cli.commands["cls"] = clearCmd

	exitCmd := cli.factory.CreateExitCommand()
	cli.commands["exit"] = exitCmd
	cli.commands["quit"] = exitCmd

	statsCmd := cli.factory.CreateStatsCommand()
	cli.commands["stats"] = statsCmd
	cli.commands["status"] = statsCmd

	cli.commands["stop-all"] = cli.factory.CreateStopAllCommand()
	cli.commands["stop"] = cli.factory.CreateStopCommand()
	cli.commands["reload"] = cli.factory.CreateReloadCommand()

	// Core & Feature commands
	cli.AddCommand(cli.factory.CreateListCommand())
	cli.AddCommand(cli.factory.CreateInfoCommand())
	cli.AddCommand(cli.factory.CreateSendCommand())
	cli.AddCommand(cli.factory.CreateConnectCommand())
	cli.AddCommand(cli.factory.CreateDisconnectCommand())
	cli.AddCommand(cli.factory.CreateBackgroundCommand())
	cli.AddCommand(cli.factory.CreateStressTestCommand())
	cli.AddCommand(cli.factory.CreateDbStatsCommand())

	scenarioCmd := cli.factory.CreateScenarioCommand()
	cli.commands["scenario"] = scenarioCmd
	cli.commands["scenarios"] = scenarioCmd

	cli.AddCommand(cli.factory.CreateRunScenarioCommand())
	cli.AddCommand(cli.factory.CreateInitSpecCommand())
	cli.AddCommand(cli.factory.CreateInitTxCommand())

	targetCmd := cli.factory.CreateTargetCommand()
	cli.commands["target"] = targetCmd
	cli.commands["set"] = targetCmd

	specCmd := cli.factory.CreateSpecCommand()
	cli.commands["spec"] = specCmd
	cli.commands["use-spec"] = specCmd

	txCmd := cli.factory.CreateTxCommand()
	cli.commands["tx"] = txCmd
	cli.commands["use-tx"] = txCmd
	cli.commands["transaction"] = txCmd

	serverCmd := cli.factory.CreateServerCommand()
	cli.commands["serve"] = serverCmd
	cli.commands["server"] = serverCmd
	analyzeCmd := cli.factory.CreateAnalyzeCommand()
	cli.commands["analyze"] = analyzeCmd
	cli.commands["pcap"] = analyzeCmd
}

func (cli *CLI) Run() error {
	err := cli.InitService()
	if err != nil {
		return err
	}

	// Create command factory and register commands
	cli.factory = cmd.NewFactory(cli.svc, cli.tc, cli.networkStats, cli)
	cli.registerAllCommands()

	return cli.runWithHistory()
}

func (cli *CLI) Close() {
	cli.mu.Lock()

	// Collect all workers to stop
	workersToStop := make([]*workerInfo, 0, len(cli.workers))
	stressWorkersToStop := make([]*stressTestWorker, 0, len(cli.stressWorkers))

	for _, worker := range cli.workers {
		workersToStop = append(workersToStop, worker)
	}
	for _, stressWorker := range cli.stressWorkers {
		stressWorkersToStop = append(stressWorkersToStop, stressWorker)
	}

	// Clear maps immediately
	cli.workers = make(map[string]*workerInfo)
	cli.stressWorkers = make(map[string]*stressTestWorker)

	cli.mu.Unlock() // Unlock while waiting

	// Cancel all workers
	for _, worker := range workersToStop {
		worker.cancel()
	}
	for _, stressWorker := range stressWorkersToStop {
		stressWorker.cancel()
	}

	// Wait for all worker goroutines to finish
	done := make(chan struct{})
	go func() {
		for _, worker := range workersToStop {
			worker.wg.Wait()
		}
		for _, stressWorker := range stressWorkersToStop {
			stressWorker.wg.Wait()
		}
		close(done)
	}()

	// Wait with timeout
	select {
	case <-done:
		// All workers stopped cleanly
	case <-time.After(10 * time.Second):
		fmt.Printf("Warning: Some workers did not stop cleanly during shutdown\n")
	}

	if cli.svc != nil {
		cli.svc.Close()
	}

	// Save final transaction collection state
	if saver, ok := cli.tc.(interface{ SaveState() error }); ok {
		_ = saver.SaveState()
	}

	// Close database connection
	db.StopAsyncLogger()
	db.Close()
}

func (cli *CLI) RunDirectCommand(subcommand string, args []string) error {
	if subcommand == "version" || subcommand == "v" || subcommand == "-v" || subcommand == "--version" {
		cli.PrintVersion()
		return nil
	}
	if subcommand == "init-spec" {
		var path string
		if len(args) > 0 {
			path = args[0]
		}
		cmdObj := &cmd.InitSpecCommand{OutputPath: path}
		return cmdObj.Execute()
	}
	if subcommand == "init-tx" {
		var path string
		if len(args) > 0 {
			path = args[0]
		}
		cmdObj := &cmd.InitTxCommand{OutputPath: path}
		return cmdObj.Execute()
	}

	if subcommand == "analyze" || subcommand == "pcap" {
		specPath := cfg.GetConfig().GetSpec()
		var spec *iso8583.MessageSpec
		if specPath != "" {
			if s, err := utils.CreateSpecFromFile(specPath); err == nil {
				spec = s
			}
		}
		var tcRepo transactions.Repository
		txPath := cfg.GetConfig().GetFile()
		if txPath != "" && spec != nil {
			if tc, err := transactions.NewTransactionCollection(txPath, spec); err == nil {
				tcRepo = tc
			}
		}
		cmdObj := cmd.NewAnalyzeCommand(spec, tcRepo)
		cmdObj.SetArgs(args)
		return cmdObj.Execute()
	}

	if subcommand == "scenarios" || subcommand == "scenario" {
		specPath := cfg.GetConfig().GetSpec()
		txPath := cfg.GetConfig().GetFile()
		if specPath == "" {
			return fmt.Errorf("spec file is required (use -spec-file)")
		}
		if txPath == "" {
			return fmt.Errorf("transaction file is required (use -file)")
		}
		spec, err := utils.CreateSpecFromFile(specPath)
		if err != nil {
			return fmt.Errorf("failed to load spec: %w", err)
		}
		tc, err := transactions.NewTransactionCollection(txPath, spec)
		if err != nil {
			return err
		}
		cmdObj := &cmd.ScenarioCommand{Tc: tc}
		return cmdObj.Execute()
	}

	if subcommand == "serve" || subcommand == "server" {
		specPath := cfg.GetConfig().GetSpec()
		txPath := cfg.GetConfig().GetFile()
		var spec *iso8583.MessageSpec
		var routes []cfg.MockRouteConfig
		if specPath != "" {
			if s, err := utils.CreateSpecFromFile(specPath); err == nil {
				spec = s
			}
		}
		if spec == nil {
			spec = utils.GetDefaultSpec()
		}
		var tcRepo transactions.Repository
		if txPath != "" && spec != nil {
			if tc, err := transactions.NewTransactionCollection(txPath, spec); err == nil {
				routes = tc.GetMockRoutes()
				tcRepo = tc
			}
		}
		cmdObj := cmd.NewServerCommand(spec, routes, tcRepo)

		subCmd := "start"
		port := "9999"
		headerType := "binary2"

		if len(args) > 0 {
			subCmd = strings.ToLower(args[0])
		}
		if subCmd == "stop" {
			return fmt.Errorf("'serve stop' is only applicable in interactive REPL mode. In standalone mode, stop the server with Ctrl+C")
		}

		if subCmd == "routes" || subCmd == "list" {
			cmdObj.ListRoutes()
			return nil
		}

		if subCmd == "start" {
			if len(args) > 1 {
				port = args[1]
			}
			if len(args) > 2 {
				headerType = args[2]
			}
			if len(args) > 3 {
				if s, err := utils.CreateSpecFromFile(args[3]); err == nil {
					spec = s
					cmdObj = cmd.NewServerCommand(spec, routes, tcRepo)
				}
			}
		} else {
			// If first argument is numeric port or header type
			port = args[0]
			if len(args) > 1 {
				headerType = args[1]
			}
			if len(args) > 2 {
				if s, err := utils.CreateSpecFromFile(args[2]); err == nil {
					spec = s
					cmdObj = cmd.NewServerCommand(spec, routes, tcRepo)
				}
			}
		}

		return cmdObj.RunDirectServer(port, headerType)
	}

	// For other commands (run-scenario), we must initialize the service
	if err := cli.InitService(); err != nil {
		return err
	}

	cli.factory = cmd.NewFactory(cli.svc, cli.tc, cli.networkStats, cli)
	cli.registerAllCommands()

	if subcommand == "run-scenario" {
		fs := flag.NewFlagSet("run-scenario", flag.ContinueOnError)
		reportPath := fs.String("report", "", "Path to export the test report JSON")
		lengthType := fs.String("length", "ascii4", "Connection length type (ascii4, binary2, bcd2, NAPS, visa)")
		if err := fs.Parse(args); err != nil {
			return err
		}

		scenarioName := ""
		if len(fs.Args()) > 0 {
			scenarioName = fs.Arg(0)
		}

		if scenarioName == "" {
			return fmt.Errorf("scenario name is required")
		}

		// Connect first
		header, err := utils.SelectLength(*lengthType)
		if err != nil {
			return fmt.Errorf("invalid length type '%s': %w", *lengthType, err)
		}
		naps := (*lengthType == "NAPS")
		fmt.Printf("Connecting to server at %s...\n", cli.svc.Address)
		if err := cli.svc.Connect(naps, header); err != nil {
			return fmt.Errorf("failed to connect to server: %w", err)
		}
		defer cli.svc.Disconnect()

		cmdObj := cli.factory.CreateRunScenarioCommand()
		if runScenarioCmd, ok := cmdObj.(*cmd.RunScenarioCommand); ok {
			runScenarioCmd.ScenarioName = scenarioName
			runScenarioCmd.ReportPath = *reportPath
		}
		return cmdObj.Execute()
	}

	return fmt.Errorf("unknown subcommand: %s", subcommand)
}
