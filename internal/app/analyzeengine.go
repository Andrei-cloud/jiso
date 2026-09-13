// analyzeengine.go holds the ONE analyze engine orchestration shared by
// the PAR-307 headless command and the SCR-510 §J TUI wizard: capture
// flow enumeration, flow auto-pick, the per-mode engine calls
// (StreamAnalyzer / VarianceEngine / Correlator / ScenarioBuilder), and
// the generated-items output path. It was extracted from
// internal/cli/cmd/analyze_engine.go / analyze_headless.go so
// internal/app can drive the same engine without importing cobra; the
// cobra shims now delegate here and map the typed ConfigError into their
// exit-taxonomy type. Engine errors that name the capture or spec are
// config-class (exit 3 / inline field text in the TUI).
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/moov-io/iso8583"

	"jiso/internal/analyzer"
	"jiso/internal/config"
)

// Analyze mode tokens (input side; the AnalyzeOutput.Mode wire values
// stay "transactions"/"mock_routes"/"scenario").
const (
	AnalyzeModeTx       = "tx"
	AnalyzeModeRoutes   = "routes"
	AnalyzeModeScenario = "scenario"
)

// AnalyzeDefaultScenarioName is the scenario scaffold name used when no
// prompt can ask for one (the wizard's default answer, PAR-307).
const AnalyzeDefaultScenarioName = "PCAP Captured Test Scenario"

// AnalyzeEngineOptions are the inputs of one engine run over a single
// selected flow.
type AnalyzeEngineOptions struct {
	Mode         string // AnalyzeModeTx / Routes / Scenario
	PcapPath     string
	HeaderType   string
	Spec         *iso8583.MessageSpec
	Unsecure     bool
	Direction    analyzer.TrafficDirection
	OutputFile   string
	ScenarioName string // scenario mode; empty = AnalyzeDefaultScenarioName
}

// EnumeratePCAPFlows enumerates the capture's flows (port -> message
// count per direction). An empty enumeration is a config-class error
// naming the pcap: the analyze paths never fabricate a flow (E1-FIX #1
// lesson). UAT round 5: src flows are enumerated too so a capture taken
// on the server side (where the requests arrive as src) is analyzable;
// PickAnalyzeFlow keeps dst-first precedence so the PAR-307 auto-pick
// behavior on ordinary captures is unchanged.
func EnumeratePCAPFlows(pcapPath string) ([]analyzer.TrafficDirection, error) {
	dirs, err := analyzer.InspectPCAPDirections(pcapPath)
	if err != nil {
		return nil, &ConfigError{Path: pcapPath, Err: fmt.Errorf("failed to inspect capture: %w", err)}
	}

	flows := make([]analyzer.TrafficDirection, 0, len(dirs))
	for _, d := range dirs {
		if (d.Mode == analyzer.DirectionDst || d.Mode == analyzer.DirectionSrc) && d.PacketCount > 0 {
			flows = append(flows, d)
		}
	}
	if len(flows) == 0 {
		return nil, &ConfigError{Path: pcapPath, Err: fmt.Errorf("no flows found in capture: no server-port TCP traffic")}
	}

	sort.Slice(flows, func(i, j int) bool {
		if flows[i].TargetPort != flows[j].TargetPort {
			return flows[i].TargetPort < flows[j].TargetPort
		}

		return flows[i].Mode < flows[j].Mode // dst before src at equal ports
	})

	return flows, nil
}

// flowModeRank orders flow modes by template quality for the auto-pick:
// dst (our requests) first, src (server-side captures' requests) last
// (UAT round 5).
func flowModeRank(d analyzer.TrafficDirection) int {
	switch d.Mode {
	case analyzer.DirectionDst:
		return 0
	case analyzer.DirectionSrc:
		return 1
	default:
		return 2
	}
}

// PickAnalyzeFlow resolves the analyzed flow: port <= 0 auto-picks the
// highest-message flow (ties broken by lower port for determinism, the
// PAR-307 --yes contract); an explicit port must hit an enumerated flow,
// else a config-class error names the available ports. dst wins over src
// whenever both exist for the same port (the request half is the
// template source); src-only ports — server-side captures — resolve to
// their src flow (UAT round 5).
func PickAnalyzeFlow(flows []analyzer.TrafficDirection, port int, pcapPath string) (analyzer.TrafficDirection, error) {
	if port > 0 {
		return pickAnalyzeFlowPort(flows, port, pcapPath)
	}

	// Auto-pick: dst flows outrank src (request halves are the template
	// source); the PAR-307 highest-message/lowest-port tie rule applies
	// within the outranked set first.
	best := flows[0]
	bestRank := flowModeRank(best)
	for _, d := range flows[1:] {
		rank := flowModeRank(d)
		if rank < bestRank ||
			(rank == bestRank && (d.PacketCount > best.PacketCount ||
				(d.PacketCount == best.PacketCount && d.TargetPort < best.TargetPort))) {
			best, bestRank = d, rank
		}
	}

	return best, nil
}

// pickAnalyzeFlowPort resolves an explicit port: dst wins when both
// directions exist at that port; a src-only port (server-side capture)
// resolves to its src flow; an unknown port is a config-class error
// listing the available ports once each (UAT round 5).
func pickAnalyzeFlowPort(flows []analyzer.TrafficDirection, port int, pcapPath string) (analyzer.TrafficDirection, error) {
	var fallback analyzer.TrafficDirection
	found := false
	for _, d := range flows {
		if int(d.TargetPort) != port {
			continue
		}
		if d.Mode == analyzer.DirectionDst {
			return d, nil
		}
		if d.Mode == analyzer.DirectionSrc {
			fallback, found = d, true
		}
	}
	if found {
		return fallback, nil
	}

	ports := make([]string, 0, len(flows))
	seen := make(map[int]bool, len(flows))
	for _, d := range flows {
		if seen[int(d.TargetPort)] {
			continue
		}
		seen[int(d.TargetPort)] = true
		ports = append(ports, strconv.Itoa(int(d.TargetPort)))
	}

	return analyzer.TrafficDirection{}, &ConfigError{
		Path: pcapPath,
		Err:  fmt.Errorf("flow port %d not found in capture; available flows: %s", port, strings.Join(ports, ", ")),
	}
}

// pickAnalyzeFlowDir resolves an explicit (port, direction) to its traffic
// direction so the operator's independent direction pick is honoured (UAT
// round 7); an unknown pair is a config-class error listing the capture's
// port/direction flows.
func pickAnalyzeFlowDir(flows []analyzer.TrafficDirection, port int, dir, pcapPath string) (analyzer.TrafficDirection, error) {
	for _, d := range flows {
		if int(d.TargetPort) == port && d.Mode == dir {
			return d, nil
		}
	}
	avail := make([]string, 0, len(flows))
	seen := make(map[string]bool, len(flows))
	for _, d := range flows {
		k := strconv.Itoa(int(d.TargetPort)) + "/" + d.Mode
		if seen[k] {
			continue
		}
		seen[k] = true
		avail = append(avail, k)
	}

	return analyzer.TrafficDirection{}, &ConfigError{
		Path: pcapPath,
		Err:  fmt.Errorf("flow %d/%s not found in capture; available flows: %s", port, dir, strings.Join(avail, ", ")),
	}
}

// AnalyzeOutputFile is where generated items land: config --file when
// configured, else the engine's default per-mode path.
func AnalyzeOutputFile(cfg *config.Config, mode string) string {
	if cfg != nil {
		if p := cfg.GetFile(); p != "" {
			return p
		}
	}
	if mode == AnalyzeModeRoutes {
		return filepath.Join("transactions", "mock_routes.json")
	}

	return filepath.Join("transactions", "transaction.json")
}

// StatPath is the §J spec-step validation leg: a missing/unreachable
// path comes back as a config-class error naming it (PAR-311: surfaced
// as inline field text, never a crash, never a created file).
func (a *App) StatPath(_ context.Context, path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return &ConfigError{Path: path, Err: fmt.Errorf("file not found: %w", err)}
	}

	return nil
}

// RunAnalyzeEngine drives the engine entry points (StreamAnalyzer,
// VarianceEngine, Correlator, ScenarioBuilder) for the selected flow —
// the same calls the REPL wizard performs after its prompts. It returns
// the result view and the items the caller may persist (nothing is
// written here).
func RunAnalyzeEngine(opts AnalyzeEngineOptions) (*AnalyzeOutput, []config.Item, error) {
	streamAnalyzer := analyzer.NewStreamAnalyzer(opts.Spec)

	if opts.Mode == AnalyzeModeScenario {
		return runAnalyzeScenario(streamAnalyzer, opts)
	}

	messages, err := streamAnalyzer.ExtractMessagesFromFileWithDirection(opts.PcapPath, opts.HeaderType, opts.Direction)
	if err != nil {
		return nil, nil, &ConfigError{Path: opts.PcapPath, Err: fmt.Errorf("extraction failed: %w", err)}
	}
	if len(messages) == 0 {
		return nil, nil, &ConfigError{Path: opts.PcapPath, Err: fmt.Errorf("no valid ISO8583 messages could be extracted with header '%s'", opts.HeaderType)}
	}

	flows := streamAnalyzer.AggregateFlows(messages)
	mockRouteGoal := opts.Mode == AnalyzeModeRoutes

	keys := make([]string, 0, len(flows))
	for key := range flows {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	varianceEngine := analyzer.NewVarianceEngine(opts.Spec, opts.Unsecure)

	var items []config.Item
	var results []*analyzer.VarianceResult

	for _, key := range keys {
		var res []*analyzer.VarianceResult
		var err error
		if mockRouteGoal {
			res, err = varianceEngine.AnalyzeFlowToMockRoutes(flows[key])
		} else {
			res, err = varianceEngine.AnalyzeFlow(flows[key])
		}
		if err != nil {
			// Per-flow variance failures are warnings; the wizard skips
			// them the same way.
			continue
		}
		for _, r := range res {
			results = append(results, r)
			items = append(items, r.Transaction)
			if r.Dataset.Name != "" && len(r.Dataset.Data) > 0 {
				items = append(items, r.Dataset)
			}
		}
	}

	if len(items) == 0 {
		return nil, nil, fmt.Errorf("no transaction templates, mock routes, or datasets could be generated from flow %d", opts.Direction.TargetPort)
	}

	output := NewAnalyzeOutputFromAnalysis(opts.PcapPath, opts.HeaderType, opts.Direction, opts.Unsecure, flows, results, mockRouteGoal, opts.OutputFile)

	return output, items, nil
}

// runAnalyzeScenario correlates request/response pairs and scaffolds the
// scenario without prompts: every pair is selected, reversals are kept
// where the capture contains them, and mock routes are generated (the
// wizard's default answers).
func runAnalyzeScenario(streamAnalyzer *analyzer.StreamAnalyzer, opts AnalyzeEngineOptions) (*AnalyzeOutput, []config.Item, error) {
	scenarioName := opts.ScenarioName
	if scenarioName == "" {
		scenarioName = AnalyzeDefaultScenarioName
	}

	annotated, err := streamAnalyzer.ExtractAnnotatedMessagesFromFile(opts.PcapPath, opts.HeaderType, opts.Direction.TargetPort)
	if err != nil {
		return nil, nil, &ConfigError{Path: opts.PcapPath, Err: fmt.Errorf("annotated extraction failed: %w", err)}
	}
	if len(annotated) == 0 {
		return nil, nil, &ConfigError{Path: opts.PcapPath, Err: fmt.Errorf("no valid ISO8583 messages could be extracted with header '%s'", opts.HeaderType)}
	}

	pairs, err := analyzer.NewCorrelator(opts.Unsecure).Correlate(annotated)
	if err != nil {
		return nil, nil, &ConfigError{Path: opts.PcapPath, Err: fmt.Errorf("correlation failed: %w", err)}
	}
	if len(pairs) == 0 {
		return nil, nil, &ConfigError{Path: opts.PcapPath, Err: fmt.Errorf("no request/response pairs could be correlated from '%s'", opts.PcapPath)}
	}

	includeReversals := make(map[int]bool, len(pairs))
	for i, pair := range pairs {
		includeReversals[i] = pair.Reversal != nil
	}

	scaffold, err := analyzer.NewScenarioBuilder(opts.Spec, opts.Unsecure).Build(pairs, analyzer.ScenarioScaffoldOptions{
		ScenarioName:       scenarioName,
		IncludeReversals:   includeReversals,
		GenerateMockRoutes: true,
		Unsecure:           opts.Unsecure,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build scenario scaffold: %w", err)
	}

	items := make([]config.Item, 0, len(scaffold.Transactions)+len(scaffold.Datasets)+len(scaffold.MockRoutes)+1)
	items = append(items, scaffold.Transactions...)
	items = append(items, scaffold.Datasets...)
	items = append(items, scaffold.Scenario)
	items = append(items, scaffold.MockRoutes...)

	output := NewAnalyzeOutputFromScenarioScaffold(opts.PcapPath, opts.HeaderType, opts.Unsecure, pairs, includeReversals, scenarioName, scaffold, opts.OutputFile)

	return output, items, nil
}
