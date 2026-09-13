package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"jiso/internal/app"
	"jiso/internal/cli/output"
	"jiso/internal/transactions"
)

// printStressPlan renders `stress --dry-run`: the plan that WOULD run, with
// no connection, no worker, and no report file.
func printStressPlan(out *output.Renderer, plan *stressPlan) {
	w := out.Out()

	_, _ = fmt.Fprintln(w, "STRESS PLAN (dry-run: no connection, no worker started, no report written)")
	_, _ = fmt.Fprintf(w, "  Transactions: %s\n", strings.Join(plan.Tx, ", "))
	_, _ = fmt.Fprintf(w, "  Target:       %s\n", plan.Target)
	_, _ = fmt.Fprintf(w, "  TPS:          %d\n", plan.TPS)
	_, _ = fmt.Fprintf(w, "  Ramp-up:      %s\n", plan.Ramp)
	_, _ = fmt.Fprintf(w, "  Duration:     %s\n", plan.Duration)
	_, _ = fmt.Fprintf(w, "  Workers:      %d\n", plan.Workers)
	if plan.Report != "" {
		_, _ = fmt.Fprintf(w, "  Report:       %s (not written)\n", plan.Report)
	}
}

// printStressSummary renders the final StressSummary as a human table.
func printStressSummary(out *output.Renderer, s *app.StressSummary, target string) {
	w := out.Out()

	_, _ = fmt.Fprintf(w, "STRESS SUMMARY (worker %s, %s)\n", s.WorkerID, s.Status)
	_, _ = fmt.Fprintf(w, "Target:       %s\n", target)
	_, _ = fmt.Fprintf(w, "Transactions: %s\n", strings.Join(s.TransactionNames, ", "))
	_, _ = fmt.Fprintf(w, "Workers:      %d    Target TPS: %d    Ramp-up: %s\n", s.Workers, s.TargetTPS, s.RampUpDuration)
	_, _ = fmt.Fprintf(w, "Duration:     %s    Runtime: %s\n", s.Duration, s.Runtime.Round(time.Microsecond))
	_, _ = fmt.Fprintf(w, "Sent:         %d    Successful: %d    Failed: %d\n", s.Sent, s.Successful, s.Failed)
	_, _ = fmt.Fprintf(w, "TPS:          actual %.2f    peak %.2f\n", s.ActualTPS, s.PeakTPS)
	_, _ = fmt.Fprintf(w, "Latency ms:   min %.2f  p50 %.2f  p90 %.2f  p95 %.2f  p99 %.2f  max %.2f\n",
		s.MinLatencyMs, s.P50LatencyMs, s.P90LatencyMs, s.P95LatencyMs, s.P99LatencyMs, s.MaxLatencyMs)
	_, _ = fmt.Fprintf(w, "Latency budget: satisfactory=%d tolerable=%d exceeded=%d\n",
		s.LatencySatisfactory, s.LatencyTolerable, s.LatencyExceeded)
	_, _ = fmt.Fprintf(w, "Response codes: %s\n", formatRCCounts(s.ResponseCodes))

	if len(s.LatencyBuckets) > 0 {
		_, _ = fmt.Fprintln(w, "Latency histogram:")
		for _, b := range s.LatencyBuckets {
			_, _ = fmt.Fprintf(w, "  [%s] %d\n", b.Label, b.Count)
		}
	}

	if len(s.Transactions) > 0 {
		_, _ = fmt.Fprintln(w, "Per transaction:")
		for _, tx := range s.Transactions {
			_, _ = fmt.Fprintf(w, "  %-20s ok=%d err=%d mean=%.2fms p99=%.2fms rc=%s\n",
				tx.Name, tx.Successful, tx.Failed, tx.MeanLatencyMs, tx.P99LatencyMs, formatRCCounts(tx.ResponseCodes))
		}
	}
}

// formatRCCounts renders a response-code distribution deterministically
// ("00=12, 12=3"), empty distribution as "-".
func formatRCCounts(codes map[string]int) string {
	if len(codes) == 0 {
		return "-"
	}

	keys := make([]string, 0, len(codes))
	for k := range codes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, codes[k]))
	}

	return strings.Join(parts, ", ")
}

// stressTestReport maps the stress summary into the TestReport shape
// `scenario run --report` writes: one step per transaction type, overall
// success when nothing failed.
func stressTestReport(s *app.StressSummary) *transactions.TestReport {
	steps := make([]transactions.StepResult, 0, len(s.Transactions))
	for _, tx := range s.Transactions {
		step := transactions.StepResult{
			StepName:  tx.Name,
			Success:   tx.Failed == 0,
			LatencyMs: int64(math.Round(tx.MeanLatencyMs)),
		}
		if tx.Failed > 0 {
			step.Error = fmt.Sprintf("%d of %d sends failed (response codes: %s)",
				tx.Failed, tx.Successful+tx.Failed, formatRCCounts(tx.ResponseCodes))
		}
		steps = append(steps, step)
	}

	return &transactions.TestReport{
		ScenarioName: "stress",
		Description: fmt.Sprintf("Headless stress run: %s at %d tps, ramp %s, duration %s, workers %d",
			s.Name, s.TargetTPS, s.RampUpDuration, s.Duration, s.Workers),
		Success:    s.Failed == 0,
		StartTime:  s.StartTime,
		EndTime:    s.EndTime,
		DurationMs: s.Runtime.Milliseconds(),
		Steps:      steps,
	}
}

// saveStressReport exports the TestReport-shaped JSON with the same
// convention as scenario run's saveReport: mkdir -p the dir, indent two
// spaces, and announce the path on stderr.
func saveStressReport(out *output.Renderer, path string, s *app.StressSummary) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(stressTestReport(s), "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out.Err(), "Test report exported to: %s\n", path)

	return nil
}
