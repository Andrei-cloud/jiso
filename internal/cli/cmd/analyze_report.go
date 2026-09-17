package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"jiso/internal/analyzer"
	"jiso/internal/app"
)

// analyzeFlowStat is one enumerated capture flow in the dry-run plan.
type analyzeFlowStat struct {
	Port int `json:"port"`
	Msgs int `json:"msgs"`
}

// analyzeDryRunPlan is the machine-readable --dry-run preview of a
// headless analyze: the flow table and what WOULD run, with nothing
// written.
type analyzeDryRunPlan struct {
	DryRun       bool              `json:"dry_run"`
	Action       string            `json:"action"`
	Mode         string            `json:"mode"`
	StreamFile   string            `json:"stream_file"`
	HeaderType   string            `json:"header_type"`
	SelectedPort int               `json:"selected_port"`
	SelectedMsgs int               `json:"selected_msgs"`
	Flows        []analyzeFlowStat `json:"flows"`
	TxOutput     string            `json:"tx_output"`
	ReportOutput string            `json:"report_output,omitempty"`
}

func newAnalyzeDryRunPlan(
	mode string,
	streamFile string,
	header string,
	flows []analyzer.TrafficDirection,
	selected analyzer.TrafficDirection,
	txOutput string,
	reportOutput string,
	reportGiven bool,
) *analyzeDryRunPlan {
	stats := make([]analyzeFlowStat, 0, len(flows))
	for _, f := range flows {
		stats = append(stats, analyzeFlowStat{Port: int(f.TargetPort), Msgs: f.PacketCount})
	}

	plan := &analyzeDryRunPlan{
		DryRun:       true,
		Action:       "analyze",
		Mode:         mode,
		StreamFile:   streamFile,
		HeaderType:   header,
		SelectedPort: int(selected.TargetPort),
		SelectedMsgs: selected.PacketCount,
		Flows:        stats,
		TxOutput:     txOutput,
	}
	if reportGiven {
		plan.ReportOutput = reportOutput
	}

	return plan
}

// printAnalyzeDryRun renders the flow table and the plan for humans.
func printAnalyzeDryRun(w io.Writer, plan *analyzeDryRunPlan) {
	_, _ = fmt.Fprintln(w, "Flows in capture (destination port -> messages):")
	for _, f := range plan.Flows {
		_, _ = fmt.Fprintf(w, "  %-7d %d msgs\n", f.Port, f.Msgs)
	}
	_, _ = fmt.Fprintf(w, "\ndry-run: would analyze flow %d (%d msgs) of '%s' (header %s) in %s mode\n",
		plan.SelectedPort, plan.SelectedMsgs, plan.StreamFile, plan.HeaderType, plan.Mode)
	_, _ = fmt.Fprintf(w, "dry-run: generated items would be written to %s\n", plan.TxOutput)
	if plan.ReportOutput != "" {
		_, _ = fmt.Fprintf(w, "dry-run: report would be written to %s\n", plan.ReportOutput)
	}
	_, _ = fmt.Fprintln(w, "dry-run: nothing was written")
}

// printAnalyzeSummary renders the human summary of an AnalyzeOutput.
func printAnalyzeSummary(w io.Writer, out *app.AnalyzeOutput) {
	sep := "================================================================================"

	_, _ = fmt.Fprintln(w, sep)
	_, _ = fmt.Fprintf(w, " ANALYZE COMPLETE — mode %s\n", out.Mode)
	_, _ = fmt.Fprintln(w, sep)
	_, _ = fmt.Fprintf(w, " Capture:    %s (header %s, flow port %d, %d pkts)\n",
		out.StreamFile, out.HeaderType, out.TargetPort, out.PacketCount)

	if out.Mode == "scenario" {
		_, _ = fmt.Fprintf(w, " Scenario:   %s (%d pair(s), %d step(s))\n",
			out.ScenarioName, out.PairCount, out.ScenarioStepCount)
	} else {
		_, _ = fmt.Fprintf(w, " Messages:   %d extracted into %d flow(s)\n", out.ExtractedMessages, out.FlowCount)
	}

	_, _ = fmt.Fprintf(w, " Generated:  %d transaction(s), %d dataset(s), %d mock route(s)\n",
		len(out.GeneratedTransactionNames), len(out.GeneratedDatasetNames), len(out.GeneratedMockRouteNames))
	_, _ = fmt.Fprintf(w, " Saved to:   %s\n", out.OutputFile)
	_, _ = fmt.Fprintln(w, sep)
}

// writeJSONAtomicReport writes v as indented JSON via temp-file + rename, so
// -o readers never observe a partial report (same pattern as
// app.writeJSONAtomic).
func writeJSONAtomicReport(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding report: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	if err := writeAndRename(tmp, tmpName, data, path); err != nil {
		_ = os.Remove(tmpName)

		return err
	}

	return nil
}

func writeAndRename(tmp *os.File, tmpName string, data []byte, path string) error {
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("writing temp report %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("chmod temp report %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp report %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("renaming report into %s: %w", path, err)
	}

	return nil
}
