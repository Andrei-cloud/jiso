package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/app"
	"jiso/internal/cli/output"
	"jiso/internal/transactions"
)

func TestStressUnknownTxExitsConfig(t *testing.T) {
	isolateConfig(t)
	f := writeCLI102Fixtures(t)

	for _, extra := range [][]string{nil, {"--json"}} {
		args := append([]string{
			"stress", "--tx", "Nope,Purchase",
			"--spec", f.specPath, "--file", f.txPath,
			"--host", "127.0.0.1", "--port", "1",
		}, extra...)

		stdout, stderr, err := runCLI102(t, args...)
		require.Error(t, err)
		assert.Equal(t, ExitConfig, ExitCodeForError(err))
		assert.Contains(t, err.Error()+stderr, "unknown transaction 'Nope'")
		assert.Empty(t, stdout, "failure path must keep stdout empty/pure")
	}
}

func TestStressNoTargetExitsConfig(t *testing.T) {
	isolateConfig(t)
	f := writeCLI102Fixtures(t)

	stdout, stderr, err := runCLI102(t, "stress", "--tx", "Purchase", "--spec", f.specPath, "--file", f.txPath)
	require.Error(t, err)
	assert.Equal(t, ExitConfig, ExitCodeForError(err))
	assert.Contains(t, err.Error()+stderr, "host and port are not configured")
	assert.Contains(t, err.Error(), "--host")
	assert.Empty(t, stdout)
}

func TestStressNumericBoundsExitUsage(t *testing.T) {
	isolateConfig(t)
	f := writeCLI102Fixtures(t)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"tps zero", []string{"--tps", "0"}, "TPS must be greater than 0"},
		{"tps too high", []string{"--tps", "100001"}, "TPS cannot exceed 100000"},
		{"workers zero", []string{"--workers", "0"}, "workers must be greater than 0"},
		{"workers too high", []string{"--workers", "51"}, "workers cannot exceed 50"},
		{"zero duration", []string{"--duration", "0s"}, "duration must be greater than 0"},
		{"negative ramp", []string{"--ramp", "-1s"}, "ramp must not be negative"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{
				"stress", "--tx", "Purchase", "--dry-run",
				"--spec", f.specPath, "--file", f.txPath,
			}, tt.args...)

			stdout, stderr, err := runCLI102(t, args...)
			require.Error(t, err)
			assert.Equal(t, ExitUsage, ExitCodeForError(err))
			assert.Contains(t, stderr, tt.want)
			assert.Empty(t, stdout)
		})
	}
}

func TestStressDryRunPlanWritesNothing(t *testing.T) {
	isolateConfig(t)
	f := writeCLI102Fixtures(t)
	reportPath := filepath.Join(t.TempDir(), "report.json")

	stdout, stderr, err := runCLI102(t, "stress",
		"--tx", "Purchase", "--tps", "5", "--ramp", "1s", "--duration", "2s", "--workers", "2",
		"--report", reportPath, "--dry-run",
		"--spec", f.specPath, "--file", f.txPath,
		"--host", "127.0.0.1", "--port", "1",
	)
	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "STRESS PLAN (dry-run")
	assert.Contains(t, stdout, "Transactions: Purchase")
	assert.Contains(t, stdout, "Target:       127.0.0.1:1")

	_, statErr := os.Stat(reportPath)
	assert.True(t, os.IsNotExist(statErr), "dry-run must not write the report file")

	stdout, stderr, err = runCLI102(t, "stress",
		"--tx", "Purchase", "--tps", "5", "--report", reportPath, "--dry-run", "--json",
		"--spec", f.specPath, "--file", f.txPath,
	)
	require.NoError(t, err)
	assert.Empty(t, stderr)

	var plan stressPlan
	require.NoError(t, json.Unmarshal([]byte(stdout), &plan))
	assert.True(t, plan.DryRun)
	assert.Equal(t, []string{"Purchase"}, plan.Tx)
	assert.Equal(t, 5, plan.TPS)
	assert.Equal(t, "(not configured)", plan.Target)
}

func TestStressTestReportShapeAndSaveConvention(t *testing.T) {
	t.Parallel()

	summary := &app.StressSummary{
		WorkerID: "w1", Name: "Purchase", Type: "stress_test", Status: "completed",
		TransactionNames: []string{"Purchase"}, Workers: 2, TargetTPS: 50,
		RampUpDuration: 30 * time.Second, Duration: time.Minute, Runtime: 91 * time.Second,
		Sent: 100, Successful: 98, Failed: 2,
		StartTime: time.Now().Add(-91 * time.Second), EndTime: time.Now(),
		Transactions: []app.TransactionSummary{
			{Name: "Purchase", Successful: 98, Failed: 2, ResponseCodes: map[string]int{"00": 98, "ERROR": 2}, MeanLatencyMs: 4.567},
		},
	}

	cmd := &cobra.Command{Use: "stress"}
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)

	path := filepath.Join(t.TempDir(), "sub", "report.json")
	require.NoError(t, saveStressReport(output.New(cmd), path, summary))
	assert.Contains(t, errBuf.String(), "Test report exported to: "+path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var report transactions.TestReport
	require.NoError(t, json.Unmarshal(data, &report))
	assert.Equal(t, "stress", report.ScenarioName)
	assert.False(t, report.Success)
	require.Len(t, report.Steps, 1)
	assert.Equal(t, "Purchase", report.Steps[0].StepName)
	assert.False(t, report.Steps[0].Success)
	assert.Contains(t, report.Steps[0].Error, "2 of 100 sends failed")
	assert.Contains(t, report.Steps[0].Error, "00=98, ERROR=2")
	assert.Equal(t, int64(5), report.Steps[0].LatencyMs)
}
