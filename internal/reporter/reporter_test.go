package reporter

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"jiso/internal/transactions"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportJSONReport(t *testing.T) {
	report := &transactions.TestReport{
		ScenarioName: "Test Scenario",
		Description:  "Sample description",
		Success:      true,
		StartTime:    time.Now(),
		EndTime:      time.Now(),
		DurationMs:   15,
		Steps: []transactions.StepResult{
			{
				StepName:  "Step 1",
				Success:   true,
				LatencyMs: 15,
			},
		},
	}

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "report.json")

	err := ExportJSONReport(report, outPath)
	require.NoError(t, err)

	_, err = os.Stat(outPath)
	assert.NoError(t, err)
}
