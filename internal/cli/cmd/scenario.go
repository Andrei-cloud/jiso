package cmd

import (
	"errors"
	"fmt"

	cfg "jiso/internal/config"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"

	cmdpkg "jiso/internal/command"

	"github.com/spf13/cobra"
)

func newScenarioCmd() *cobra.Command {
	scenarioCmd := &cobra.Command{
		Use:   "scenario",
		Short: "Manage and execute test scenarios",
	}

	scenarioCmd.AddCommand(newScenarioListCmd())
	scenarioCmd.AddCommand(newScenarioRunCmd())
	return scenarioCmd
}

func newScenarioListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all defined test scenarios",
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeScenarioList()
		},
	}
}

func executeScenarioList() error {
	specPath := cfg.GetConfig().GetSpec()
	txPath := cfg.GetConfig().GetFile()
	if specPath == "" {
		return errors.New("spec file is required (use -s or --spec)")
	}
	if txPath == "" {
		return errors.New("transaction file is required (use -f or --file)")
	}

	spec, err := utils.CreateSpecFromFile(specPath)
	if err != nil {
		return fmt.Errorf("failed to load spec: %w", err)
	}

	tc, err := transactions.NewTransactionCollection(txPath, spec)
	if err != nil {
		return err
	}

	scenarioCmd := &cmdpkg.ScenarioCommand{Tc: tc}
	return scenarioCmd.Execute()
}

func newScenarioRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <scenario-name>",
		Short: "Run a specific test scenario against a server",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reportPath, _ := cmd.Flags().GetString("report")
			lengthType, _ := cmd.Flags().GetString("length")
			scenarioName := ""
			if len(args) > 0 {
				scenarioName = args[0]
			}
			return executeScenarioRun(scenarioName, reportPath, lengthType)
		},
	}

	cmd.Flags().StringP("report", "R", "", "Path to export the test report JSON")
	cmd.Flags().StringP("length", "l", "ascii4", "Connection length type (ascii4, binary2, bcd2, NAPS, visa)")
	return cmd
}

func executeScenarioRun(scenarioName, reportPath, lengthType string) error {
	specPath := cfg.GetConfig().GetSpec()
	txPath := cfg.GetConfig().GetFile()
	if specPath == "" {
		return errors.New("spec file is required (use -s or --spec)")
	}
	if txPath == "" {
		return errors.New("transaction file is required (use -f or --file)")
	}

	spec, err := utils.CreateSpecFromFile(specPath)
	if err != nil {
		return fmt.Errorf("failed to load spec: %w", err)
	}

	tc, err := transactions.NewTransactionCollection(txPath, spec)
	if err != nil {
		return err
	}

	svc, err := service.NewService(
		cfg.GetConfig().GetHost(),
		cfg.GetConfig().GetPort(),
		cfg.GetConfig().GetSpec(),
		true,
		cfg.GetConfig().GetReconnectAttempts(),
		cfg.GetConfig().GetConnectTimeout(),
		cfg.GetConfig().GetTotalConnectTimeout(),
		cfg.GetConfig().GetResponseTimeout(),
	)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	host := cfg.GetConfig().GetHost()
	port := cfg.GetConfig().GetPort()
	if host != "" || port != "" {
		if host == "" {
			host = "localhost"
		}
		if port == "" {
			port = "9999"
		}
		svc.Address = fmt.Sprintf("%s:%s", host, port)
	}

	header, err := utils.SelectLength(lengthType)
	if err != nil {
		return fmt.Errorf("invalid length type '%s': %w", lengthType, err)
	}
	naps := (lengthType == "NAPS")

	fmt.Printf("Connecting to server at %s...\n", svc.Address)
	if err := svc.Connect(naps, header); err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer func() {
		if err := svc.Disconnect(); err != nil {
			fmt.Printf("Warning: Disconnect error: %v\n", err)
		}
	}()

	runCmd := &cmdpkg.RunScenarioCommand{
		Tc:           tc,
		Svc:          svc,
		ScenarioName: scenarioName,
		ReportPath:   reportPath,
	}

	return runCmd.Execute()
}
