// analyzeview_test.go covers the §J wizard façade against the
// copied fixture: enumeration finds the 8080 flow with counts,
// MTI histograms, and signon markers; runs produce AnalyzeOutput with
// generated item names and attached items; the item picker's selection
// (SetExcluded) lands in the file via WriteAnalyze through the shared
// merge writer; missing pcap/spec come back as typed ConfigError naming
// the path and never create files (class).
package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"jiso/internal/analyzer"
	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// analyzeFixture builds the fixture capture + spec in t.TempDir and
// returns (pcapPath, specPath).
func analyzeFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(fixtureSpecJSON), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	pcapPath := filepath.Join(dir, "analyze.pcap")
	if err := buildAnalyzePCAP(pcapPath, specPath); err != nil {
		t.Fatalf("build pcap: %v", err)
	}

	return pcapPath, specPath
}

// analyzeApp builds an App carrying only the config leg the façade
// reads (dbViewApp pattern; the global config is reset and restored).
func analyzeApp(t *testing.T) *App {
	t.Helper()
	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)

	return &App{cfg: cfg}
}

func TestAnalyzeViewEnumerateFindsFlowsWithCounts(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)

	enum, err := a.EnumerateFlows(context.Background(), pcap, "ascii4", spec)
	if err != nil {
		t.Fatalf("EnumerateFlows: %v", err)
	}
	if enum.Parsed != 8 || enum.Unparsable != 0 {
		t.Errorf("parsed/unparsable = %d/%d, want 8/0", enum.Parsed, enum.Unparsable)
	}

	byKey := map[string]AnalyzeFlowView{}
	for _, f := range enum.Flows {
		byKey[f.Direction+":"+strings.TrimSpace(strings.Repeat(" ", 0))+strconv.Itoa(f.ServerPort)] = f
	}
	dst8080, ok := byKey["dst:8080"]
	if !ok {
		t.Fatalf("8080 dst flow missing: %+v", enum.Flows)
	}
	if dst8080.Count != 3 {
		t.Errorf("8080 dst count = %d, want 3", dst8080.Count)
	}
	if len(dst8080.MTIHistogram) != 1 || dst8080.MTIHistogram[0].MTI != "0200" || dst8080.MTIHistogram[0].Count != 3 {
		t.Errorf("8080 dst histogram = %+v, want 0200(3)", dst8080.MTIHistogram)
	}
	src8080, ok := byKey["src:8080"]
	if !ok || src8080.Count != 3 || src8080.MTIHistogram[0].MTI != "0210" {
		t.Errorf("8080 src flow = %+v, want 3x 0210", src8080)
	}
	if _, ok := byKey["dst:9999"]; !ok {
		t.Errorf("9999 dst flow missing: %+v", enum.Flows)
	}
	if enum.Flows[0].ServerPort != 8080 {
		t.Errorf("flows not ascending by port: %+v", enum.Flows)
	}
}

func TestAnalyzeViewEnumerateSignonMarker(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(fixtureSpecJSON), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	pcapPath := filepath.Join(dir, "signon.pcap")
	if err := buildAnalyzePCAPSignon(pcapPath, specPath); err != nil {
		t.Fatalf("build pcap: %v", err)
	}
	a := analyzeApp(t)

	enum, err := a.EnumerateFlows(context.Background(), pcapPath, "ascii4", specPath)
	if err != nil {
		t.Fatalf("EnumerateFlows: %v", err)
	}
	for _, f := range enum.Flows {
		if f.Direction == "dst" && f.ServerPort == 8080 && f.SignonCount == 0 {
			t.Errorf("signon marker missing on 8080 dst: %+v", f)
		}
	}
}

func TestAnalyzeViewEnumerateMissingPcapTypedError(t *testing.T) {
	a := analyzeApp(t)
	missing := filepath.Join(t.TempDir(), "gone.pcap")

	_, err := a.EnumerateFlows(context.Background(), missing, "ascii4", "")
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || cfgErr.Path != missing {
		t.Fatalf("err = %v (%T), want *ConfigError naming %s", err, err, missing)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatalf("facade created the pcap")
	}
}

func TestAnalyzeViewEnumerateMissingSpecTypedError(t *testing.T) {
	pcap, _ := analyzeFixture(t)
	a := analyzeApp(t)
	missingSpec := filepath.Join(t.TempDir(), "gone.json")

	_, err := a.EnumerateFlows(context.Background(), pcap, "ascii4", missingSpec)
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || cfgErr.Path != missingSpec {
		t.Fatalf("err = %v (%T), want *ConfigError naming %s", err, err, missingSpec)
	}
	if _, statErr := os.Stat(missingSpec); !os.IsNotExist(statErr) {
		t.Fatalf("facade created the spec")
	}
}

func TestAnalyzeViewEnumerateEmptyCaptureNeverFabricates(t *testing.T) {
	dir := t.TempDir()
	pcapPath := filepath.Join(dir, "empty.pcap")
	if err := buildEmptyPCAP(pcapPath); err != nil {
		t.Fatalf("build empty pcap: %v", err)
	}
	a := analyzeApp(t)

	_, err := a.EnumerateFlows(context.Background(), pcapPath, "ascii4", "")
	if err == nil || !strings.Contains(err.Error(), "no flows found in capture") {
		t.Fatalf("err = %v, want the never-fabricate config error", err)
	}
}

func TestAnalyzeViewRunTxProducesGeneratedNames(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)
	outPath := filepath.Join(t.TempDir(), "out", "transaction.json")

	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
		Mode: AnalyzeModeTx, Flows: []FlowSelection{{Port: 8080, Dir: analyzer.DirectionDst}}, OutputFile: outPath,
	})
	if err != nil {
		t.Fatalf("RunAnalyze: %v", err)
	}
	if out.Mode != "transactions" || out.TargetPort != 8080 {
		t.Errorf("mode/port = %s/%d, want transactions/8080", out.Mode, out.TargetPort)
	}
	if len(out.GeneratedTransactionNames) == 0 {
		t.Errorf("no generated transaction names: %+v", out)
	}
	if len(out.GeneratedItems()) == 0 {
		t.Fatal("run must attach generated items for the write leg")
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatal("RunAnalyze must not write the output file")
	}
}

func TestAnalyzeViewRunAutoPicksHighestFlow(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)

	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec, Mode: AnalyzeModeTx,
		OutputFile: filepath.Join(t.TempDir(), "t.json"),
	})
	if err != nil {
		t.Fatalf("RunAnalyze: %v", err)
	}
	if out.TargetPort != 8080 {
		t.Errorf("auto-pick = %d, want 8080 (highest msgs)", out.TargetPort)
	}
}

func TestAnalyzeViewRunUnknownPortTypedError(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)

	_, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
		Mode: AnalyzeModeTx, Flows: []FlowSelection{{Port: 1234, Dir: analyzer.DirectionDst}},
		OutputFile: filepath.Join(t.TempDir(), "t.json"),
	})
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), "flow 1234/dst not found") {
		t.Fatalf("err = %v, want config error listing available flows", err)
	}
}

func TestAnalyzeViewRunMissingPcapTypedErrorNoFile(t *testing.T) {
	a := analyzeApp(t)
	missing := filepath.Join(t.TempDir(), "gone.pcap")

	_, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: missing, Mode: AnalyzeModeTx,
	})
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || cfgErr.Path != missing {
		t.Fatalf("err = %v (%T), want *ConfigError naming %s", err, err, missing)
	}
}

func TestAnalyzeViewRunScenarioModePairs(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)

	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
		Mode: AnalyzeModeScenario, Flows: []FlowSelection{{Port: 8080, Dir: analyzer.DirectionDst}},
		OutputFile: filepath.Join(t.TempDir(), "s.json"),
	})
	if err != nil {
		t.Fatalf("RunAnalyze scenario: %v", err)
	}
	if out.Mode != "scenario" || out.PairCount != 4 || out.ScenarioStepCount != 4 {
		t.Errorf("scenario out = %s pairs=%d steps=%d, want scenario/4/4 (the PAR-307 golden count)", out.Mode, out.PairCount, out.ScenarioStepCount)
	}
	if out.ScenarioName != AnalyzeDefaultScenarioName {
		t.Errorf("scenario name = %q, want the wizard default", out.ScenarioName)
	}
}

func TestAnalyzeViewRunMultiPortMerges(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)

	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
		Mode: AnalyzeModeTx, Flows: []FlowSelection{{Port: 8080, Dir: analyzer.DirectionDst}, {Port: 9999, Dir: analyzer.DirectionDst}},
		OutputFile: filepath.Join(t.TempDir(), "m.json"),
	})
	if err != nil {
		t.Fatalf("RunAnalyze multi: %v", err)
	}
	if out.TargetPort != 8080 {
		t.Errorf("headline port = %d, want 8080", out.TargetPort)
	}
	seen := map[int]bool{}
	for _, f := range out.Flows {
		seen[f.Count] = true
	}
	if len(out.Flows) < 2 || out.FlowCount != len(out.Flows) {
		t.Errorf("flows not merged: %+v", out.Flows)
	}
}

// runGeneratedMTIs collects the MTI (field 0) of every generated
// transaction item, so a test can tell which directions were analysed.
func runGeneratedMTIs(out *AnalyzeOutput) map[string]bool {
	mtis := map[string]bool{}
	for _, it := range out.GeneratedItems() {
		if it.Type != config.TypeTransaction || len(it.Fields) == 0 {
			continue
		}
		var f map[string]any
		if err := json.Unmarshal(it.Fields, &f); err != nil {
			continue
		}
		if m, ok := f["0"].(string); ok {
			mtis[m] = true
		}
	}

	return mtis
}

// TestAnalyzeViewRunBothDirectionsAnalyzeEach pins: selecting
// BOTH directions of a port analyses the requests AND the responses (the
// fixture's dst is 0200, its src is 0210) and merges them — the transactions
// goal no longer collapses a port to its dst half.
func TestAnalyzeViewRunBothDirectionsAnalyzeEach(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)

	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
		Mode:       AnalyzeModeTx,
		Flows:      []FlowSelection{{Port: 8080, Dir: analyzer.DirectionDst}, {Port: 8080, Dir: analyzer.DirectionSrc}},
		OutputFile: filepath.Join(t.TempDir(), "b.json"),
	})
	if err != nil {
		t.Fatalf("RunAnalyze both directions: %v", err)
	}
	mtis := runGeneratedMTIs(out)
	if !mtis["0200"] || !mtis["0210"] {
		t.Errorf("both directions must be analysed, got MTIs %v", mtis)
	}
}

// TestAnalyzeViewRunSrcOnlyAnalyzesResponses pins: selecting ONLY
// the response (src) direction writes ONLY the response templates (0210) —
// the request (0200) half is not analysed. This is the "only the selected
// direction is written" contract.
func TestAnalyzeViewRunSrcOnlyAnalyzesResponses(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)

	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
		Mode:       AnalyzeModeTx,
		Flows:      []FlowSelection{{Port: 8080, Dir: analyzer.DirectionSrc}},
		OutputFile: filepath.Join(t.TempDir(), "s.json"),
	})
	if err != nil {
		t.Fatalf("RunAnalyze src-only: %v", err)
	}
	mtis := runGeneratedMTIs(out)
	if !mtis["0210"] {
		t.Errorf("the selected src half must be analysed, got MTIs %v", mtis)
	}
	if mtis["0200"] {
		t.Errorf("the unselected dst half must NOT be analysed, got MTIs %v", mtis)
	}
}

// TestAnalyzeViewSelectedItemsWrite pins the contract: the
// picker's deselection (SetExcluded) lands in the file — WriteAnalyze
// persists exactly the selected items, the deselected item is absent,
// and an all-deselected output is an error, never a silent empty write.
func TestAnalyzeViewSelectedItemsWrite(t *testing.T) {
	t.Parallel()

	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)
	outPath := filepath.Join(t.TempDir(), "sel.json")

	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
		Mode: AnalyzeModeTx, Flows: []FlowSelection{{Port: 8080, Dir: analyzer.DirectionDst}}, OutputFile: outPath,
	})
	if err != nil {
		t.Fatalf("RunAnalyze: %v", err)
	}
	out.AttachGeneratedItems([]config.Item{
		{Type: config.TypeDataset, Name: "pool"},
		{Type: config.TypeMockRoute, Name: "route-0200-00"},
	})
	items := out.GeneratedItems()
	if len(items) < 2 {
		t.Fatalf("fixture must generate >= 2 items, got %d", len(items))
	}

	drop := ItemKey(items[0])
	out.SetExcluded([]string{drop})
	if got := out.ExcludedCount(); got != 1 {
		t.Errorf("ExcludedCount = %d, want 1", got)
	}
	if len(out.SelectedItems()) != len(items)-1 {
		t.Fatalf("SelectedItems = %d, want %d", len(out.SelectedItems()), len(items)-1)
	}
	if err := a.WriteAnalyze(context.Background(), out); err != nil {
		t.Fatalf("WriteAnalyze: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output missing: %v", err)
	}
	var written []map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if len(written) != len(items)-1 {
		t.Errorf("file holds %d items, want %d", len(written), len(items)-1)
	}
	for _, w := range written {
		if w["name"] == items[0].Name {
			t.Errorf("deselected item %q still in the file", items[0].Name)
		}
	}

	all := make([]string, 0, len(items))
	for _, it := range items {
		all = append(all, ItemKey(it))
	}
	out.SetExcluded(all)
	if err := a.WriteAnalyze(context.Background(), out); err == nil ||
		!strings.Contains(err.Error(), "deselected") {
		t.Errorf("all-deselected write err = %v, want a deselection error", err)
	}
}

func TestAnalyzeViewWriteAnalyzeWritesAndMergesOnce(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out", "transaction.json")

	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
		Mode: AnalyzeModeTx, Flows: []FlowSelection{{Port: 8080, Dir: analyzer.DirectionDst}}, OutputFile: outPath,
	})
	if err != nil {
		t.Fatalf("RunAnalyze: %v", err)
	}
	if err := a.WriteAnalyze(context.Background(), out); err != nil {
		t.Fatalf("WriteAnalyze: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output missing: %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if len(items) != len(out.GeneratedItems()) {
		t.Errorf("wrote %d items, generated %d", len(items), len(out.GeneratedItems()))
	}
	if err := a.WriteAnalyze(context.Background(), out); err != nil {
		t.Fatalf("second WriteAnalyze: %v", err)
	}
	data2, _ := os.ReadFile(outPath)
	var items2 []map[string]any
	_ = json.Unmarshal(data2, &items2)
	if len(items2) != len(items) {
		t.Errorf("re-write duplicated items: %d vs %d", len(items2), len(items))
	}
}

// TestAnalyzeViewGeneratedTransactionsCarryTheirSpec: a
// capture analyzed with a named spec stamps that spec onto every generated
// transaction (config.Item.Spec) so the file self-describes. Datasets resolve
// through their transaction and carry none, and the engine default spec (empty
// SpecPath) has nothing portable to record.
func TestAnalyzeViewGeneratedTransactionsCarryTheirSpec(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)

	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
		Mode: AnalyzeModeTx, Flows: []FlowSelection{{Port: 8080, Dir: analyzer.DirectionDst}},
		OutputFile: filepath.Join(t.TempDir(), "out.json"),
	})
	if err != nil {
		t.Fatalf("RunAnalyze: %v", err)
	}
	txs := 0
	for _, it := range out.GeneratedItems() {
		switch it.Type {
		case config.TypeTransaction:
			txs++
			if it.Spec != spec {
				t.Errorf("transaction %q spec = %q, want the analyzed spec %q", it.Name, it.Spec, spec)
			}
		case config.TypeDataset:
			if it.Spec != "" {
				t.Errorf("dataset %q must not carry a spec, got %q", it.Name, it.Spec)
			}
		}
	}
	if txs == 0 {
		t.Fatal("fixture generated no transactions")
	}

	// The engine default spec (no path) stamps nothing: items keep resolving
	// to whatever spec the session holds when the file is later opened.
	dflt, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4",
		Mode: AnalyzeModeTx, Flows: []FlowSelection{{Port: 8080, Dir: analyzer.DirectionDst}},
		OutputFile: filepath.Join(t.TempDir(), "out.json"),
	})
	if err != nil {
		t.Fatalf("RunAnalyze (default spec): %v", err)
	}
	for _, it := range dflt.GeneratedItems() {
		if it.Type == config.TypeTransaction && it.Spec != "" {
			t.Errorf("default-spec transaction %q must not stamp a spec, got %q", it.Name, it.Spec)
		}
	}
}

// TestAnalyzeViewWrittenFileSelfDescribesItsSpec: the spec
// a transaction was analyzed with must survive config.SaveItems' serializable
// form and make the file load against the RIGHT spec. Before this the writer
// dropped the field, so a visa capture opened while the session held another
// spec was validated against the wrong spec and rejected in silence.
func TestAnalyzeViewWrittenFileSelfDescribesItsSpec(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)
	outPath := filepath.Join(t.TempDir(), "visa_trxns.json")

	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
		Mode: AnalyzeModeTx, Flows: []FlowSelection{{Port: 8080, Dir: analyzer.DirectionDst}},
		OutputFile: outPath,
	})
	if err != nil {
		t.Fatalf("RunAnalyze: %v", err)
	}
	if err := a.WriteAnalyze(context.Background(), out); err != nil {
		t.Fatalf("WriteAnalyze: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	var written []map[string]any
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("written file not JSON: %v", err)
	}
	seen := 0
	for _, it := range written {
		if it["type"] != "transaction" {
			continue
		}
		seen++
		if got := it["spec"]; got != spec {
			t.Errorf("written transaction %v spec = %v, want %q", it["name"], got, spec)
		}
	}
	if seen == 0 {
		t.Error("written file has no transactions to check")
	}

	// Self-describing load: the file opens even when the session's global spec
	// is unrelated, because each transaction resolves its own recorded spec.
	if _, err := transactions.NewTransactionCollection(outPath, utils.GetDefaultSpec()); err != nil {
		t.Errorf("written file must load against a different global spec via its recorded spec: %v", err)
	}
}

func TestAnalyzeViewWriteWithoutItemsErrors(t *testing.T) {
	a := analyzeApp(t)
	path := filepath.Join(t.TempDir(), "never.json")

	err := a.WriteAnalyze(context.Background(), &AnalyzeOutput{OutputFile: path})
	if err == nil {
		t.Fatal("write without items must error")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("failed write created the file")
	}
}

func TestAnalyzeDefaultsHeaderFallback(t *testing.T) {
	a := analyzeApp(t)

	specPath, header := a.AnalyzeDefaults()
	if header != "binary2" {
		t.Errorf("header = %q, want the engine default binary2", header)
	}
	if specPath != "" {
		t.Errorf("specPath = %q, want empty when unset", specPath)
	}

	a.cfg.SetSpec("./custom.json")
	specPath, _ = a.AnalyzeDefaults()
	if specPath != "./custom.json" {
		t.Errorf("specPath = %q, want the configured prefill", specPath)
	}
}

// TestMergeFlowExtractionCapsSamples pins the memory bound:
// Unparsable is the true total across directions, Samples keeps at most
// analyzer.MaxUnparsableSamples so a pathological capture cannot make
// the wizard hold thousands.
func TestMergeFlowExtractionCapsSamples(t *testing.T) {
	t.Parallel()

	one := make([]analyzer.UnparsableSample, analyzer.MaxUnparsableSamples)
	for i := range one {
		one[i] = analyzer.UnparsableSample{Offset: int64(i), Length: 40, Reason: "boom"}
	}

	out := &AnalyzeEnumeration{}
	mergeFlowExtraction(out, flowExtraction{unparsed: len(one), samples: one})
	mergeFlowExtraction(out, flowExtraction{unparsed: len(one), samples: one})

	if out.Unparsable != 2*analyzer.MaxUnparsableSamples {
		t.Errorf("Unparsable = %d, want %d", out.Unparsable, 2*analyzer.MaxUnparsableSamples)
	}
	if len(out.Samples) != analyzer.MaxUnparsableSamples {
		t.Errorf("Samples = %d, want the %d cap", len(out.Samples), analyzer.MaxUnparsableSamples)
	}
}
