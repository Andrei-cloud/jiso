package command

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/AlecAivazis/survey/v2"
	json "github.com/goccy/go-json"

	"jiso/internal/cli/output"
	"jiso/internal/service"
	"jiso/internal/transactions"
)

// ErrScenarioFailed is the sentinel RunScenarioCommand.Execute returns when
// one or more scenario steps failed. Callers must match it with errors.Is to
// map the test-failure exit code; never string-match the message.
var ErrScenarioFailed = errors.New("scenario failed")

// RunScenarioCommand runs one scenario over the session's connection and reports
// each step's outcome. It drives the same runner the TUI does, so a scenario that
// passes here passes there -- one implementation, two front ends.
type RunScenarioCommand struct {
	Tc           transactions.Repository
	Svc          *service.Service
	ScenarioName string
	ReportPath   string

	// Out routes the report through the v2 renderer when set (cobra path):
	// under --json stdout carries the pure-JSON TestReport and the ANSI
	// human report is suppressed; nil keeps the legacy REPL stdout
	// behavior (M1 review #14).
	Out *output.Renderer
}

// Execute checks the spec, the transaction file and the connection before anything
// touches the network, then runs one scenario: the named one, or a picker over the
// scenarios the file defines when no name was given. Checking up front is why a
// mistyped --spec or --file is reported at once rather than as a failure after a
// connection was already paid for.
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
		return errors.New("invalid transaction repository type")
	}

	name := c.ScenarioName
	if name == "" {
		scenarios := tcImpl.ListScenarios()
		if len(scenarios) == 0 {
			return errors.New("no scenarios defined in configuration")
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

	// Print the report: JSON data under --json, ANSI human report otherwise.
	if err := c.emitReport(report); err != nil {
		return err
	}

	// Save JSON report if path is provided
	if c.ReportPath != "" {
		if err := c.saveReport(report); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: Failed to save test report: %v\n", err)
		}
	}

	if !report.Success {
		return ErrScenarioFailed
	}

	return nil
}

// emitReport renders the test report: with an injected renderer under --json
// it emits the TestReport as pure JSON on stdout (the human report is
// suppressed there); otherwise it prints the ANSI report to the renderer's
// writer or, without a renderer, to stdout as before.
func (c *RunScenarioCommand) emitReport(report *transactions.TestReport) error {
	if c.Out != nil {
		return c.Out.Data(report, func() { c.printReport(c.Out.Out(), report) })
	}

	c.printReport(os.Stdout, report)

	return nil
}

func (c *RunScenarioCommand) printReport(w io.Writer, report *transactions.TestReport) {
	_, _ = fmt.Fprintf(w, "\n\x1b[1mScenario Execution Report: %s\x1b[0m\n", report.ScenarioName)
	if report.Description != "" {
		_, _ = fmt.Fprintf(w, "Description: %s\n", report.Description)
	}
	_, _ = fmt.Fprintf(w, "Duration: %d ms\n", report.DurationMs)
	if report.Success {
		_, _ = fmt.Fprintf(w, "Overall Status: \x1b[32m\x1b[1mPASSED ✅\x1b[0m\n\n")
	} else {
		_, _ = fmt.Fprintf(w, "Overall Status: \x1b[31m\x1b[1mFAILED ❌\x1b[0m\n\n")
	}

	_, _ = fmt.Fprintln(w, "Steps:")
	for i, step := range report.Steps {
		statusIndicator := "\x1b[32mPASSED ✅\x1b[0m"
		if !step.Success {
			statusIndicator = "\x1b[31mFAILED ❌\x1b[0m"
		}
		_, _ = fmt.Fprintf(w, "  %d. %-35s %s (%d ms)\n", i+1, step.StepName, statusIndicator, step.LatencyMs)
		if step.Error != "" {
			_, _ = fmt.Fprintf(w, "     \x1b[31mError: %s\x1b[0m\n", step.Error)
		}
		if len(step.ValidationErrors) > 0 {
			_, _ = fmt.Fprintf(w, "     \x1b[33mValidation Failures:\x1b[0m\n")
			for _, valErr := range step.ValidationErrors {
				_, _ = fmt.Fprintf(
					w,
					"       - Field %s: expected '%s', got '%s' (Detail: %s)\n",
					valErr.Field, valErr.Expected, valErr.Actual, valErr.Message,
				)
			}
		}
	}
	_, _ = fmt.Fprintln(w)
}

func (c *RunScenarioCommand) saveReport(report *transactions.TestReport) error {
	dir := filepath.Dir(c.ReportPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(c.ReportPath, data, 0o644); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stderr, "Test report exported to: %s\n", c.ReportPath)

	return nil
}
