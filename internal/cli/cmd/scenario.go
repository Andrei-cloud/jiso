package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"jiso/internal/cli/output"
	cmdpkg "jiso/internal/command"
	cfg "jiso/internal/config"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"
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

// scenarioListItem is one entry of `jiso scenario list --json`.
type scenarioListItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	StepCount   int    `json:"step_count"`
}

func newScenarioListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   subCmdList,
		Short: "List all defined test scenarios",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeScenarioList(cmd)
		},
	}
}

// loadScenarioCollection returns the cached, load-validated transaction
// collection (configfiles.go); the spec/tx parse happens once per process.
func loadScenarioCollection() (*transactions.TransactionCollection, error) {
	return requireSpecAndTx()
}

func executeScenarioList(cmd *cobra.Command) error {
	out := output.New(cmd)

	tc, err := loadScenarioCollection()
	if err != nil {
		return err
	}

	items := make([]scenarioListItem, 0)
	for _, name := range tc.ListScenarios() {
		scenario, err := tc.GetScenario(name)
		if err != nil {
			return err
		}
		items = append(items, scenarioListItem{Name: scenario.Name, Description: scenario.Description, StepCount: len(scenario.Steps)})
	}

	return out.Data(items, func() {
		w := out.Out()
		if len(items) == 0 {
			_, _ = fmt.Fprintln(w, "No scenarios defined in the configuration file")

			return
		}

		_, _ = fmt.Fprintln(w, "Available Scenarios:")
		for _, it := range items {
			_, _ = fmt.Fprintf(w, "  %-30s - %s\n", it.Name, it.Description)
		}
	})
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

			return executeScenarioRun(cmd, scenarioName, reportPath, lengthType)
		},
	}

	cmd.Flags().StringP("report", "R", "", "Path to export the test report JSON")
	cmd.Flags().StringP("length", "l", "ascii4", "Connection length type (ascii4, binary2, bcd2, NAPS, visa)")
	return cmd
}

func executeScenarioRun(cmd *cobra.Command, scenarioName, reportPath, lengthType string) error {
	out := output.New(cmd)

	tc, err := loadScenarioCollection()
	if err != nil {
		return err
	}

	if out.DryRun() {
		return previewScenarioRun(out, tc, scenarioName)
	}

	if err := validateScenarioDefined(tc, scenarioName); err != nil {
		return err
	}

	host := strings.TrimSpace(cfg.GetConfig().GetHost())
	port := strings.TrimSpace(cfg.GetConfig().GetPort())
	if host == "" || port == "" {
		_, _ = fmt.Fprintf(out.Err(), "Error: %s\n", missingTargetMessage("run scenario", host, port))

		return &ExitCodeError{Code: ExitUsage}
	}

	return runScenarioConnected(cmd, out, tc, host, port, scenarioName, reportPath, lengthType)
}

// validateScenarioDefined fast-fails an unknown scenario before any connect attempt
// as a config-class error naming the scenario and the file that should define it.
func validateScenarioDefined(tc *transactions.TransactionCollection, scenarioName string) error {
	if scenarioName == "" {
		return nil
	}

	// fast-fail (before ANY connect attempt): an unknown scenario
	// is a config-class error naming the scenario and the file that
	// should define it (cf. analyze --flow-unknown → exit 3), and an
	// unset target is the usual usage error (send's
	// missingTargetMessage → exit 2). The old order dialed ":0" through
	// ~7 s of backoff before discovering either problem, then exited 1.
	if _, err := tc.GetScenario(scenarioName); err == nil {
		return nil
	}

	txPath := cfg.GetConfig().GetFile()
	if names := tc.ListScenarios(); len(names) > 0 {
		return &ExitConfigError{
			Path: txPath,
			Err:  fmt.Errorf("unknown scenario %q in %s (defined scenarios: %s)", scenarioName, txPath, strings.Join(names, ", ")),
		}
	}

	return &ExitConfigError{
		Path: txPath,
		Err:  fmt.Errorf("unknown scenario %q: no scenarios are defined in %s", scenarioName, txPath),
	}
}

// runScenarioConnected creates the service, connects, runs the scenario and
// disconnects, mapping a failed scenario to the ExitTestFailure exit code.
func runScenarioConnected(cmd *cobra.Command, out *output.Renderer, tc *transactions.TransactionCollection, host, port, scenarioName, reportPath, lengthType string) error {
	svc, err := service.NewService(
		host,
		port,
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
	svc.Address = host + ":" + port

	header, err := utils.SelectLength(lengthType)
	if err != nil {
		return fmt.Errorf("invalid length type '%s': %w", lengthType, err)
	}
	naps := (lengthType == "NAPS")

	out.Noticef("Connecting to server at %s...", svc.Address)
	if err := svc.Connect(naps, header); err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer func() {
		if err := svc.Disconnect(); err != nil {
			_, _ = fmt.Fprintf(out.Err(), "Warning: Disconnect error: %v\n", err)
		}
	}()

	runCmd := &cmdpkg.RunScenarioCommand{
		Tc:           tc,
		Svc:          svc,
		ScenarioName: scenarioName,
		ReportPath:   reportPath,
		Out:          out,
	}

	if err := runCmd.Execute(); err != nil {
		if errors.Is(err, cmdpkg.ErrScenarioFailed) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", err)

			return &ExitCodeError{Code: ExitTestFailure}
		}

		return err
	}

	return nil
}

// previewScenarioRun implements `scenario run --dry-run`: it prints the
// scenario steps that WOULD run — no connection, no DB writes — and exits 0.
func previewScenarioRun(out *output.Renderer, tc *transactions.TransactionCollection, name string) error {
	names := tc.ListScenarios()
	if name != "" {
		if _, err := tc.GetScenario(name); err != nil {
			return err
		}
		names = []string{name}
	}
	if len(names) == 0 {
		return errors.New("no scenarios defined in configuration")
	}

	scenarios := make([]*transactions.Scenario, 0, len(names))
	for _, n := range names {
		scenario, err := tc.GetScenario(n)
		if err != nil {
			return err
		}
		scenarios = append(scenarios, scenario)
	}

	return out.Data(scenarios, func() {
		out.Noticef("dry-run: no connection will be made; nothing will be sent or written")
		w := out.Out()
		for _, scenario := range scenarios {
			_, _ = fmt.Fprintf(w, "Would run scenario: %s\n", scenario.Name)
			if scenario.Description != "" {
				_, _ = fmt.Fprintf(w, "Description: %s\n", scenario.Description)
			}
			for i, step := range scenario.Steps {
				target := step.UseTransactionID
				if target == "" {
					target = "(inline fields)"
				}
				_, _ = fmt.Fprintf(w, "  %d. %s (transaction: %s)\n", i+1, step.Name, target)
			}
		}
	})
}
