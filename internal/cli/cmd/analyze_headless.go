package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/moov-io/iso8583"

	"jiso/internal/analyzer"
	"jiso/internal/app"
	"jiso/internal/cli/output"
	cmdpkg "jiso/internal/command"
	cfg "jiso/internal/config"
	"jiso/internal/utils"
)

// Headless analyze modes (PAR-307): tx generates transaction templates and
// datasets, routes generates mock-server routes, scenario scaffolds a test
// scenario. They map to the engine's three analyze goals.
const (
	analyzeModeTx       = "tx"
	analyzeModeRoutes   = "routes"
	analyzeModeScenario = "scenario"
)

// headlessScenarioName is the scenario scaffold name used when no prompt can
// ask for one (the wizard's own default, PAR-307).
const headlessScenarioName = app.AnalyzeDefaultScenarioName

// runAnalyzeHeadless is the PAR-307 non-interactive analyze path. It composes
// the internal/analyzer engine directly — never AnalyzeCommand, whose
// wizard flows (promptAnalyze/runAnalysis/runScenarioAnalysis) carry survey
// prompts — so this path can never block on a terminal.
func runAnalyzeHeadless(cmd *cobra.Command, args []string) error {
	out := output.New(cmd)
	stderr := cmd.ErrOrStderr()

	in, err := resolveHeadlessInputs(cmd, stderr, args)
	if err != nil {
		return err
	}

	if out.DryRun() {
		plan := newAnalyzeDryRunPlan(in.mode, in.pcapPath, in.header, in.flows, in.selected, in.txFile, in.reportPath, in.reportGiven)

		return out.Data(plan, func() { printAnalyzeDryRun(out.Out(), plan) })
	}

	return emitHeadlessAnalyzeResult(out, stderr, in)
}

// headlessAnalyzeInput is the resolved set of flags, capture flows and selected
// flow that runAnalyzeHeadless operates on.
type headlessAnalyzeInput struct {
	mode        string
	pcapPath    string
	reportPath  string
	reportGiven bool
	unsecure    bool
	header      string
	spec        *iso8583.MessageSpec
	flows       []analyzer.TrafficDirection
	selected    analyzer.TrafficDirection
	txFile      string
}

// resolveHeadlessInputs validates the analyze arguments and flags and gathers the
// capture's flows and the selected flow, writing usage errors to stderr.
func resolveHeadlessInputs(cmd *cobra.Command, stderr io.Writer, args []string) (headlessAnalyzeInput, error) {
	mode, err := resolveAnalyzeMode(cmd)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)

		return headlessAnalyzeInput{}, &ExitCodeError{Code: ExitUsage}
	}

	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "Error: pcap file argument is required")

		return headlessAnalyzeInput{}, &ExitCodeError{Code: ExitUsage}
	}
	if len(args) > 1 {
		_, _ = fmt.Fprintf(stderr, "Error: unexpected extra arguments: %s\n", strings.Join(args[1:], " "))

		return headlessAnalyzeInput{}, &ExitCodeError{Code: ExitUsage}
	}

	pcapPath := args[0]
	if _, err := os.Stat(pcapPath); err != nil {
		return headlessAnalyzeInput{}, &ExitConfigError{Path: pcapPath, Err: errors.New("capture file not found")}
	}

	reportPath := ""
	reportGiven := cmd.Flags().Changed("output")
	if reportGiven {
		if reportPath, _ = cmd.Flags().GetString("output"); reportPath == "" {
			_, _ = fmt.Fprintln(stderr, "Error: --output needs a file path")

			return headlessAnalyzeInput{}, &ExitCodeError{Code: ExitUsage}
		}
	}

	in := headlessAnalyzeInput{
		mode:        mode,
		pcapPath:    pcapPath,
		reportPath:  reportPath,
		reportGiven: reportGiven,
		header:      headlessAnalyzeHeader(cmd),
		txFile:      headlessTxOutput(mode),
	}
	in.unsecure, _ = cmd.Flags().GetBool("unsecure")

	// Spec comes from the load-validated --spec (PersistentPreRunE); a bad
	// --spec already exited 3 naming the file, an unset one falls back to
	// the default spec like the engine path does.
	spec, _, err := configuredSpecAndTx()
	if err != nil {
		return headlessAnalyzeInput{}, err
	}
	if spec == nil {
		spec = utils.GetDefaultSpec()
	}
	in.spec = spec

	in.flows, err = headlessFlows(pcapPath)
	if err != nil {
		return headlessAnalyzeInput{}, err
	}

	in.selected, err = selectAnalyzeFlow(cmd, pcapPath, in.flows)
	if err != nil {
		return headlessAnalyzeInput{}, err
	}
	_, _ = fmt.Fprintf(stderr, "selected flow %d (%d msgs)\n", in.selected.TargetPort, in.selected.PacketCount)

	return in, nil
}

// emitHeadlessAnalyzeResult runs the analyze engine, persists generated items and
// the optional report, and renders the summary.
func emitHeadlessAnalyzeResult(out *output.Renderer, stderr io.Writer, in headlessAnalyzeInput) error {
	result, err := runHeadlessAnalyze(in.mode, in.pcapPath, in.header, in.spec, in.unsecure, in.selected)
	if err != nil {
		return err
	}

	if err := cmdpkg.SaveConfigItems(result.outputFile, result.items); err != nil {
		return fmt.Errorf("failed to save generated items to '%s': %w", result.outputFile, err)
	}

	if in.reportGiven {
		if err := writeJSONAtomicReport(in.reportPath, result.output); err != nil {
			return fmt.Errorf("failed to write report to %s: %w", in.reportPath, err)
		}
		_, _ = fmt.Fprintf(stderr, "report written to: %s\n", in.reportPath)
	}

	return out.Data(result.output, func() { printAnalyzeSummary(out.Out(), result.output) })
}

// resolveAnalyzeMode validates --mode; the legacy --scenario bool is an alias
// for --mode scenario unless --mode was given explicitly.
func resolveAnalyzeMode(cmd *cobra.Command) (string, error) {
	raw, _ := cmd.Flags().GetString("mode")
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		mode = analyzeModeTx
	}
	if scenario, _ := cmd.Flags().GetBool("scenario"); scenario && !cmd.Flags().Changed("mode") {
		mode = analyzeModeScenario
	}

	switch mode {
	case analyzeModeTx, analyzeModeRoutes, analyzeModeScenario:
		return mode, nil
	}

	return "", fmt.Errorf("invalid --mode %q (want tx, routes, or scenario)", raw)
}

// headlessAnalyzeHeader resolves the message length header: local --flag >
// global config > the engine default "binary2".
func headlessAnalyzeHeader(cmd *cobra.Command) string {
	header, _ := cmd.Flags().GetString("header")
	if header == "" {
		header = cfg.GetConfig().GetHeader()
	}
	if header == "" {
		header = "binary2"
	}

	return header
}

// headlessFlows enumerates the capture's flows (port -> message count
// per direction). An empty enumeration is an exit-3 config error naming
// the pcap: the command never fabricate a flow (E1-FIX #1 lesson). The
// enumeration itself is the shared app.EnumeratePCAPFlows (SCR-510
// extraction, driven by the §J TUI wizard too); the listing collapses to
// analysis units (UAT round 5: the raw direction rows double-listed
// every port once dst and once src).
func headlessFlows(pcapPath string) ([]analyzer.TrafficDirection, error) {
	flows, err := app.EnumeratePCAPFlows(pcapPath)
	if err != nil {
		return nil, mapAppConfigError(err)
	}

	return headlessFlowUnits(flows), nil
}

// headlessFlowUnits collapses direction rows to one unit per port: dst
// preferred (the request half is the template source), src-only ports
// kept so server-side captures stay selectable (UAT round 5).
func headlessFlowUnits(flows []analyzer.TrafficDirection) []analyzer.TrafficDirection {
	units := make([]analyzer.TrafficDirection, 0, len(flows))
	index := make(map[int]int, len(flows))
	for _, f := range flows {
		if i, ok := index[int(f.TargetPort)]; ok {
			if f.Mode == analyzer.DirectionDst {
				units[i] = f
			}

			continue
		}
		index[int(f.TargetPort)] = len(units)
		units = append(units, f)
	}

	return units
}

// selectAnalyzeFlow applies the headless selection contract: --flow must hit
// an enumerated port (else exit 3 listing the available ports), otherwise the
// highest-message flow wins (ties broken by lower port for determinism).
func selectAnalyzeFlow(cmd *cobra.Command, pcapPath string, flows []analyzer.TrafficDirection) (analyzer.TrafficDirection, error) {
	port := 0
	if cmd.Flags().Changed("flow") {
		port, _ = cmd.Flags().GetInt("flow")
	}

	dir, err := app.PickAnalyzeFlow(flows, port, pcapPath)
	if err != nil {
		return analyzer.TrafficDirection{}, mapAppConfigError(err)
	}

	return dir, nil
}

// headlessTxOutput is where generated items land: --file when configured,
// else the engine's default per-mode path (shared app.AnalyzeOutputFile).
func headlessTxOutput(mode string) string {
	return app.AnalyzeOutputFile(cfg.GetConfig(), mode)
}
