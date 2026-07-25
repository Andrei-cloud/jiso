package command

import (
	json "github.com/goccy/go-json"
	"fmt"
	"os"
	"path/filepath"

	"jiso/internal/service"
	"jiso/internal/transactions"

	"github.com/AlecAivazis/survey/v2"
)

type ScenarioCommand struct {
	Tc transactions.Repository
}

func (c *ScenarioCommand) Name() string {
	return "scenarios"
}

func (c *ScenarioCommand) Synopsis() string {
	return "List all defined test scenarios"
}

func (c *ScenarioCommand) Execute() error {
	if err := VerifyTx(c.Tc); err != nil {
		return err
	}
	tcImpl, ok := c.Tc.(*transactions.TransactionCollection)
	if !ok {
		return fmt.Errorf("invalid transaction repository type")
	}

	scenarios := tcImpl.ListScenarios()
	if len(scenarios) == 0 {
		fmt.Println("No scenarios defined in the configuration file")
		return nil
	}

	fmt.Println("Available Scenarios:")
	for _, name := range scenarios {
		scenario, err := tcImpl.GetScenario(name)
		if err == nil {
			fmt.Printf("  %-30s - %s\n", scenario.Name, scenario.Description)
		}
	}
	return nil
}

type RunScenarioCommand struct {
	Tc           transactions.Repository
	Svc          *service.Service
	ScenarioName string
	ReportPath   string
}

func (c *RunScenarioCommand) Name() string {
	return "run-scenario"
}

func (c *RunScenarioCommand) Synopsis() string {
	return "Run a specific test scenario. (requires connection to server)"
}

func (c *RunScenarioCommand) Execute() error {
	if err := VerifySpec(c.Svc); err != nil {
		return err
	}
	if err := VerifyTx(c.Tc); err != nil {
		return err
	}
	if err := VerifyConnection(c.Svc); err != nil {
		return err
	}

	tcImpl, ok := c.Tc.(*transactions.TransactionCollection)
	if !ok {
		return fmt.Errorf("invalid transaction repository type")
	}

	name := c.ScenarioName
	if name == "" {
		scenarios := tcImpl.ListScenarios()
		if len(scenarios) == 0 {
			return fmt.Errorf("no scenarios defined in configuration")
		}

		prompt := &survey.Select{
			Message: "Select scenario to run:",
			Options: scenarios,
		}
		err := survey.AskOne(prompt, &name)
		if err != nil {
			return err
		}
	}

	runner := transactions.NewScenarioRunner(c.Svc, tcImpl)
	report, err := runner.RunScenario(name)
	if err != nil {
		return fmt.Errorf("failed to run scenario '%s': %w", name, err)
	}

	// Print formatted terminal report
	c.printReport(report)

	// Save JSON report if path is provided
	if c.ReportPath != "" {
		if err := c.saveReport(report); err != nil {
			fmt.Printf("Warning: Failed to save test report: %v\n", err)
		}
	}

	if !report.Success {
		return fmt.Errorf("scenario failed")
	}

	return nil
}

func (c *RunScenarioCommand) printReport(report *transactions.TestReport) {
	fmt.Printf("\n\x1b[1mScenario Execution Report: %s\x1b[0m\n", report.ScenarioName)
	if report.Description != "" {
		fmt.Printf("Description: %s\n", report.Description)
	}
	fmt.Printf("Duration: %d ms\n", report.DurationMs)
	if report.Success {
		fmt.Printf("Overall Status: \x1b[32m\x1b[1mPASSED ✅\x1b[0m\n\n")
	} else {
		fmt.Printf("Overall Status: \x1b[31m\x1b[1mFAILED ❌\x1b[0m\n\n")
	}

	fmt.Println("Steps:")
	for i, step := range report.Steps {
		statusIndicator := "\x1b[32mPASSED ✅\x1b[0m"
		if !step.Success {
			statusIndicator = "\x1b[31mFAILED ❌\x1b[0m"
		}
		fmt.Printf("  %d. %-35s %s (%d ms)\n", i+1, step.StepName, statusIndicator, step.LatencyMs)
		if step.Error != "" {
			fmt.Printf("     \x1b[31mError: %s\x1b[0m\n", step.Error)
		}
		if len(step.ValidationErrors) > 0 {
			fmt.Printf("     \x1b[33mValidation Failures:\x1b[0m\n")
			for _, valErr := range step.ValidationErrors {
				fmt.Printf("       - Field %s: expected '%s', got '%s' (Detail: %s)\n", valErr.Field, valErr.Expected, valErr.Actual, valErr.Message)
			}
		}
	}
	fmt.Println()
}

func (c *RunScenarioCommand) saveReport(report *transactions.TestReport) error {
	dir := filepath.Dir(c.ReportPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(c.ReportPath, data, 0644); err != nil {
		return err
	}

	fmt.Printf("Test report exported to: %s\n", c.ReportPath)
	return nil
}
