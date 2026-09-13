// analyzeview.go is the §J analyze-wizard façade (SCR-510). The TUI
// never touches internal/analyzer or internal/command itself: root runs
// these *App methods off the UI thread (tea.Cmd) and renders the
// returned views. The engine orchestration is the shared
// RunAnalyzeEngine/EnumeratePCAPFlows extraction (PAR-307's code, now in
// this package), so the wizard and the headless command agree on every
// count, pick rule, and error class. Missing pcap/spec come back as
// typed *ConfigError naming the path (PAR-311 class: the TUI surfaces
// them as inline field text; nothing is ever created here). The write
// leg is config.SaveItems — the one generated-items writer, identical
// bytes to the CLI/REPL persistence path.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/moov-io/iso8583"

	"jiso/internal/analyzer"
	"jiso/internal/config"
	"jiso/internal/utils"
)

// analyzeCtxCheckEvery bounds how often the §J extraction loops re-check the
// caller context. Each iteration extracts every framed message for one flow
// direction, so a capture with many server-port flows keeps scanning between
// checks; re-checking every 100 iterations lets a §J cancel actually stop the
// scan instead of running the whole pcap to completion first.
const analyzeCtxCheckEvery = 100

// AnalyzeEnumeration is one §J step-⑤ enumeration: directional flow
// rows (dst requests first, then their src response rows), the parsed
// message total, and the count of framed messages that failed to unpack
// (the wireframe's "412 msgs parsed, 3 unparsable"). Samples holds the
// capped failure samples behind the unparsable count (UAT round 6 §J
// reviewer); Unparsable is the TRUE total, Samples at most
// analyzer.MaxUnparsableSamples ("showing first 50 of 96").
type AnalyzeEnumeration struct {
	Flows      []AnalyzeFlowView
	Parsed     int
	Unparsable int
	Samples    []analyzer.UnparsableSample
}

// AnalyzeRunOptions are the §J run parameters: goal radio (Mode), masking
// radio (Unsecure = raw), the selected flows (Flows; empty = the PAR-307
// highest-count auto-pick), and the file paths.
type AnalyzeRunOptions struct {
	PcapPath     string
	HeaderType   string
	SpecPath     string
	Mode         string // AnalyzeModeTx / Routes / Scenario
	Unsecure     bool
	Flows        []FlowSelection
	OutputFile   string
	ScenarioName string
}

// AnalyzeDefaults reports the wizard's step prefill: the configured
// spec path ("" = unset, the engine default spec applies) and the
// configured header (default "binary2", the engine default).
func (a *App) AnalyzeDefaults() (specPath, header string) {
	if a != nil && a.cfg != nil {
		specPath = a.cfg.GetSpec()
		header = a.cfg.GetHeader()
	}
	if header == "" {
		header = "binary2"
	}

	return specPath, header
}

// resolveAnalyzeSpec loads the wizard-selected spec: "" is the engine
// default spec, a given path must load (else a typed ConfigError naming
// it — the §J inline field error).
func resolveAnalyzeSpec(specPath string) (*iso8583.MessageSpec, error) {
	if specPath == "" {
		return utils.GetDefaultSpec(), nil
	}
	spec, err := utils.CreateSpecFromFile(specPath)
	if err != nil {
		return nil, &ConfigError{Path: specPath, Err: fmt.Errorf("failed to load spec: %w", err)}
	}

	return spec, nil
}

// EnumerateFlows enumerates the capture's flows for §J step ⑤: one
// "dst" row per server port with messages plus its paired "src" row,
// each carrying the MTI histogram and signon marker. A missing capture,
// a bad header extraction, or an empty enumeration is a typed
// ConfigError naming the pcap — the wizard never fabricates flows
// (E1-FIX lesson).
func (a *App) EnumerateFlows(ctx context.Context, pcapPath, headerType, specPath string) (*AnalyzeEnumeration, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	if _, err := os.Stat(pcapPath); err != nil {
		return nil, &ConfigError{Path: pcapPath, Err: fmt.Errorf("capture file not readable: %w", err)}
	}
	spec, err := resolveAnalyzeSpec(specPath)
	if err != nil {
		return nil, err
	}

	dirs, err := analyzer.InspectPCAPDirections(pcapPath)
	if err != nil {
		return nil, &ConfigError{Path: pcapPath, Err: fmt.Errorf("failed to inspect capture: %w", err)}
	}

	dst := make([]analyzer.TrafficDirection, 0, len(dirs))
	srcByPort := make(map[int]analyzer.TrafficDirection, len(dirs))
	for _, d := range dirs {
		switch {
		case d.Mode == "dst" && d.PacketCount > 0:
			dst = append(dst, d)
		case d.Mode == "src" && d.PacketCount > 0:
			srcByPort[int(d.TargetPort)] = d
		}
	}
	if len(dst) == 0 && len(srcByPort) == 0 {
		return nil, &ConfigError{Path: pcapPath, Err: errors.New("no flows found in capture: no server-port TCP traffic")}
	}
	sort.Slice(dst, func(i, j int) bool { return dst[i].TargetPort < dst[j].TargetPort })

	streamAnalyzer := analyzer.NewStreamAnalyzer(spec)
	out := &AnalyzeEnumeration{}
	for i, d := range dst {
		if i%analyzeCtxCheckEvery == 0 {
			if err := checkCtx(ctx); err != nil {
				return nil, err
			}
		}
		extract, err := enumerateFlowView(streamAnalyzer, pcapPath, headerType, d)
		if err != nil {
			return nil, err
		}
		mergeFlowExtraction(out, extract)
		if src, ok := srcByPort[int(d.TargetPort)]; ok {
			srcExtract, serr := enumerateFlowView(streamAnalyzer, pcapPath, headerType, src)
			if serr == nil {
				mergeFlowExtraction(out, srcExtract)
			}
			// A src row that will not extract stays hidden: the dst row
			// is the analysis unit and enumeration must not fail on the
			// display-only half.
		}
	}

	// UAT round 5: src-only ports (captures taken on the server side,
	// where the requests arrive as src) are units too — enumerate them
	// so the run step can select them.
	if err := appendSrcOnlyFlows(ctx, out, streamAnalyzer, pcapPath, headerType, dst, srcByPort); err != nil {
		return nil, err
	}

	return out, nil
}

// mergeFlowExtraction folds one direction's row and counts into the
// enumeration. Unparsable is the true total across directions; Samples
// keeps at most analyzer.MaxUnparsableSamples overall so a pathological
// capture cannot make the wizard hold thousands.
func mergeFlowExtraction(out *AnalyzeEnumeration, e flowExtraction) {
	out.Flows = append(out.Flows, e.view)
	out.Parsed += e.parsed
	out.Unparsable += e.unparsed
	for _, s := range e.samples {
		if len(out.Samples) >= analyzer.MaxUnparsableSamples {
			break
		}
		out.Samples = append(out.Samples, s)
	}
}

// appendSrcOnlyFlows enumerates the src flows with no dst counterpart
// (server-side captures, UAT round 5). A src-only port that will not
// extract stays hidden rather than failing the enumeration.
func appendSrcOnlyFlows(ctx context.Context, out *AnalyzeEnumeration, streamAnalyzer *analyzer.StreamAnalyzer,
	pcapPath, headerType string, dst []analyzer.TrafficDirection, srcByPort map[int]analyzer.TrafficDirection,
) error {
	for _, d := range sortedSrcOnly(dst, srcByPort) {
		if err := checkCtx(ctx); err != nil {
			return err
		}
		srcExtract, serr := enumerateFlowView(streamAnalyzer, pcapPath, headerType, d)
		if serr != nil {
			continue
		}
		mergeFlowExtraction(out, srcExtract)
	}

	return nil
}

// sortedSrcOnly returns the src flows with no dst counterpart, ordered
// by port (deterministic §J rows).
func sortedSrcOnly(dst []analyzer.TrafficDirection, srcByPort map[int]analyzer.TrafficDirection) []analyzer.TrafficDirection {
	out := make([]analyzer.TrafficDirection, 0, len(srcByPort))
	for port, d := range srcByPort {
		hasDst := false
		for _, dd := range dst {
			if int(dd.TargetPort) == port {
				hasDst = true

				break
			}
		}
		if !hasDst {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TargetPort < out[j].TargetPort })

	return out
}

// flowExtraction is one direction's §J row plus the two counts the caller
// folds into the enumeration totals: messages that unpacked, messages
// that were framed but would not unpack, and this direction's capped
// failure samples.
type flowExtraction struct {
	view     AnalyzeFlowView
	parsed   int
	unparsed int
	samples  []analyzer.UnparsableSample
}

// enumerateFlowView extracts one direction's messages (counted, with
// capped failure samples) and builds its §J table row view. The second
// result is the parsed count, the third the framed-but-unpackable count.
func enumerateFlowView(streamAnalyzer *analyzer.StreamAnalyzer, pcapPath, headerType string, dir analyzer.TrafficDirection) (flowExtraction, error) {
	collector := &analyzer.UnparsableCollector{}

	messages, unparsed, err := streamAnalyzer.ExtractMessagesFromFileSampled(pcapPath, headerType, dir, collector)
	if err != nil {
		return flowExtraction{}, &ConfigError{Path: pcapPath, Err: fmt.Errorf("extraction failed: %w", err)}
	}

	counts := make(map[string]int, len(messages))
	for _, msg := range messages {
		mti, merr := msg.GetMTI()
		if merr != nil || mti == "" {
			continue
		}
		counts[mti]++
	}

	histogram := make([]AnalyzeMTICount, 0, len(counts))
	for mti, n := range counts {
		histogram = append(histogram, AnalyzeMTICount{MTI: mti, Count: n})
	}
	sort.Slice(histogram, func(i, j int) bool {
		if histogram[i].Count != histogram[j].Count {
			return histogram[i].Count > histogram[j].Count
		}

		return histogram[i].MTI < histogram[j].MTI
	})

	return flowExtraction{
		view: AnalyzeFlowView{
			Direction:    dir.Mode,
			ServerPort:   int(dir.TargetPort),
			PeerPort:     int(dir.PeerPort),
			Count:        len(messages),
			MTIHistogram: histogram,
			SignonCount:  counts["0800"] + counts["0810"],
		},
		parsed:   len(messages),
		unparsed: unparsed,
		samples:  collector.Samples(),
	}, nil
}

// RunAnalyze runs the engine for the selected flows (flow filter) with
// the goal radio as mode and the masking radio as unsecure, and returns
// the merged AnalyzeOutput carrying its generated items (PreviewWrite /
// WriteAnalyze consume them; nothing is written here). Multiple ports
// merge deterministically in ascending-port order; the headline
// TargetPort is the selected flow with the most messages (ties the
// lowest port — the PAR-307 pick rule).
func (a *App) RunAnalyze(ctx context.Context, opts AnalyzeRunOptions) (*AnalyzeOutput, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	if _, err := os.Stat(opts.PcapPath); err != nil {
		return nil, &ConfigError{Path: opts.PcapPath, Err: fmt.Errorf("capture file not readable: %w", err)}
	}
	spec, err := resolveAnalyzeSpec(opts.SpecPath)
	if err != nil {
		return nil, err
	}
	flows, err := EnumeratePCAPFlows(opts.PcapPath)
	if err != nil {
		return nil, err
	}

	selected, err := selectAnalyzeFlows(flows, opts.Flows, opts.Mode, opts.PcapPath)
	if err != nil {
		return nil, err
	}

	outputFile := opts.OutputFile
	if outputFile == "" {
		var cfg *config.Config
		if a != nil {
			cfg = a.cfg
		}
		outputFile = AnalyzeOutputFile(cfg, opts.Mode)
	}

	merged, allItems, err := a.runAnalyzeFlows(ctx, opts, spec, selected, outputFile)
	if err != nil {
		return nil, err
	}
	if merged == nil {
		return nil, fmt.Errorf("no analysis output produced for '%s'", opts.PcapPath)
	}
	if len(selected) > 1 {
		best := pickHeadlineFlow(selected)
		merged.TargetPort = int(best.TargetPort)
		merged.DirectionMode = best.Mode
		merged.DirectionLabel = best.Label
	}
	stampGeneratedSpec(allItems, opts.SpecPath)
	merged.AttachGeneratedItems(allItems)

	return merged, nil
}

// stampGeneratedSpec records the spec each generated transaction was composed
// with on the item itself (config.Item.Spec), so the written file describes
// itself. NewTransactionCollection (the load) and the composer (the send) both
// resolve a transaction's own Spec via utils.ResolveSpec, falling back to the
// session's global spec only when the item carries none. Without this stamp a
// capture analyzed with, say, the visa spec is validated against whatever spec
// the session happens to hold when the file is later opened, so a tool-written
// extract is rejected on the transactions screen for fields that are correct
// for its own spec (UAT round 7: the file the tool wrote would not load).
// An empty specPath means the engine default spec was used — there is nothing
// portable to record, so items keep resolving to the session spec.
func stampGeneratedSpec(items []config.Item, specPath string) {
	if specPath == "" {
		return
	}
	for i := range items {
		if items[i].Type == config.TypeTransaction {
			items[i].Spec = specPath
		}
	}
}

// mergeAnalyzeOutputs folds a second per-flow result into the first
// (multi-flow §J runs): counters sum, list sections concatenate in
// selection order, scenario pairs re-index after the existing ones.
func mergeAnalyzeOutputs(into, extra *AnalyzeOutput) {
	into.Flows = append(into.Flows, extra.Flows...)
	into.FlowCount = len(into.Flows)
	into.ExtractedMessages += extra.ExtractedMessages

	for _, pair := range extra.Pairs {
		pair.Index = into.PairCount + 1
		into.PairCount++
		into.Pairs = append(into.Pairs, pair)
	}
	into.ScenarioStepCount = into.PairCount

	into.GeneratedTransactionNames = append(into.GeneratedTransactionNames, extra.GeneratedTransactionNames...)
	into.GeneratedDatasetNames = append(into.GeneratedDatasetNames, extra.GeneratedDatasetNames...)
	into.GeneratedMockRouteNames = append(into.GeneratedMockRouteNames, extra.GeneratedMockRouteNames...)
	into.Warnings = append(into.Warnings, extra.Warnings...)
}

// WriteAnalyze persists the SELECTED items (the §J item picker's
// outcome, UAT round 6) through the shared generated-items writer
// (config.SaveItems). An output without items (never produced by
// RunAnalyze) or an empty selection is an error, never a silent empty
// write. ctx is checked before touching the disk: an aborted or left
// wizard cancels its write leg, and a cancelled leg never starts a
// second SaveItems on a file a previous leg may still be rewriting
// (E5-FIX/B2).
func (a *App) WriteAnalyze(ctx context.Context, out *AnalyzeOutput) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("analyze write cancelled: %w", err)
	}
	if out == nil {
		return errors.New("analyze: nothing to write")
	}
	if len(out.items) == 0 {
		return fmt.Errorf("analyze: no generated items to write to '%s'", out.OutputFile)
	}
	sel := out.SelectedItems()
	if len(sel) == 0 {
		return fmt.Errorf("analyze: every generated item is deselected - nothing to write to '%s'", out.OutputFile)
	}
	if err := config.SaveItems(out.OutputFile, sel); err != nil {
		return fmt.Errorf("failed to save generated items to '%s': %w", out.OutputFile, err)
	}

	return nil
}

// pickHeadlineFlow chooses the §J headline direction: the flow with the most
// messages, ties broken by the lowest target port (the PAR-307 pick rule).
func pickHeadlineFlow(selected []analyzer.TrafficDirection) analyzer.TrafficDirection {
	best := selected[0]
	for _, d := range selected[1:] {
		if d.PacketCount > best.PacketCount || (d.PacketCount == best.PacketCount && d.TargetPort < best.TargetPort) {
			best = d
		}
	}

	return best
}

// runAnalyzeFlows runs the engine for each selected flow in order and merges the
// outputs, checking the context every analyzeCtxCheckEvery flows.
func (a *App) runAnalyzeFlows(ctx context.Context, opts AnalyzeRunOptions, spec *iso8583.MessageSpec, selected []analyzer.TrafficDirection, outputFile string) (*AnalyzeOutput, []config.Item, error) {
	var merged *AnalyzeOutput
	var allItems []config.Item
	for i, dir := range selected {
		if i%analyzeCtxCheckEvery == 0 {
			if err := checkCtx(ctx); err != nil {
				return nil, nil, err
			}
		}
		out, items, err := RunAnalyzeEngine(AnalyzeEngineOptions{
			Mode:         opts.Mode,
			PcapPath:     opts.PcapPath,
			HeaderType:   opts.HeaderType,
			Spec:         spec,
			Unsecure:     opts.Unsecure,
			Direction:    dir,
			OutputFile:   outputFile,
			ScenarioName: opts.ScenarioName,
		})
		if err != nil {
			return nil, nil, err
		}
		allItems = append(allItems, items...)
		if merged == nil {
			merged = out

			continue
		}
		mergeAnalyzeOutputs(merged, out)
	}

	return merged, allItems, nil
}
