package reporter

import (
	"fmt"
	json "github.com/goccy/go-json"
	"os"

	"jiso/internal/transactions"
)

// PrintTerminalReport outputs a colorized execution tree to stdout
func PrintTerminalReport(report *transactions.TestReport) {
	if report == nil {
		return
	}

	statusStr := "PASSED ✅"
	if !report.Success {
		statusStr = "FAILED ❌"
	}

	fmt.Println("================================================================================")
	fmt.Printf(" SCENARIO EXECUTION REPORT: %s (%s)\n", report.ScenarioName, statusStr)
	fmt.Println("================================================================================")
	if report.Description != "" {
		fmt.Printf(" Description: %s\n", report.Description)
	}
	fmt.Printf(" Start Time:  %s\n", report.StartTime.Format("2006-01-02 15:04:05"))
	fmt.Printf(" Total Time:  %d ms\n", report.DurationMs)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println(" STEPS:")

	for i, step := range report.Steps {
		stepStatus := "PASSED ✅"
		if !step.Success {
			stepStatus = "FAILED ❌"
		}
		fmt.Printf("  Step %d: %-30s [%s] (%d ms)\n", i+1, step.StepName, stepStatus, step.LatencyMs)

		if step.Error != "" {
			fmt.Printf("    └─ 🔴 Error: %s\n", step.Error)
		}

		for _, vErr := range step.ValidationErrors {
			fmt.Printf("    └─ ⚠️ Field %s Validation Failed: %s (Expected: '%s', Actual: '%s')\n",
				vErr.Field, vErr.Message, vErr.Expected, vErr.Actual)
		}
	}
	fmt.Println("================================================================================")
}

// ExportJSONReport exports the TestReport struct to a specified JSON file path
func ExportJSONReport(report *transactions.TestReport, filePath string) error {
	if report == nil {
		return fmt.Errorf("test report is nil")
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal test report: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write test report to %s: %w", filePath, err)
	}

	return nil
}
