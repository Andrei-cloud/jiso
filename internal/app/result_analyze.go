package app

import (
	"sort"

	"jiso/internal/analyzer"
	"jiso/internal/config"
)

// AnalyzeOutput is the JSON-serializable result of a capture analysis run
// (analyze command): the extraction/correlation stats, the aggregated
// flows or correlated pairs, and the names of the generated config items.
// Mode is "transactions", "mock_routes", or "scenario" and tells a
// frontend which sections are populated.
type AnalyzeOutput struct {
	Mode           string `json:"mode"`
	StreamFile     string `json:"stream_file"`
	HeaderType     string `json:"header_type"`
	DirectionMode  string `json:"direction_mode,omitempty"`
	DirectionLabel string `json:"direction_label,omitempty"`
	TargetPort     int    `json:"target_port"`
	PacketCount    int    `json:"packet_count"`
	ByteCount      int    `json:"byte_count"`
	Unsecure       bool   `json:"unsecure"`

	ExtractedMessages int               `json:"extracted_messages"`
	FlowCount         int               `json:"flow_count"`
	Flows             []AnalyzeFlowView `json:"flows,omitempty"`

	PairCount         int               `json:"pair_count"`
	Pairs             []AnalyzePairView `json:"pairs,omitempty"`
	ScenarioName      string            `json:"scenario_name,omitempty"`
	ScenarioStepCount int               `json:"scenario_step_count"`

	OutputFile                string   `json:"output_file,omitempty"`
	GeneratedTransactionNames []string `json:"generated_transaction_names,omitempty"`
	GeneratedDatasetNames     []string `json:"generated_dataset_names,omitempty"`
	GeneratedMockRouteNames   []string `json:"generated_mock_route_names,omitempty"`

	Warnings []string `json:"warnings,omitempty"`

	// items are the generated config items this result would persist
	// (§J preview-before-write): unexported so the analyze
	// report wire format is unchanged, carried on the result so the
	// §J write leg (WriteAnalyze) persists exactly what the preview
	// promised without re-running the engine.
	items []config.Item

	// excluded names (ItemKey form) the operator deselected in the §J
	// item picker (choose which generated transaction
	// types land in the file). Unexported: the analyze report wire
	// format never sees it.
	excluded map[string]bool
}

// ItemKey identifies one generated item for the selection set
// (kind|name — a transaction and a dataset may share a name).
func ItemKey(it config.Item) string {
	return string(it.Type) + "|" + it.Name
}

// SetExcluded replaces the deselection set (the §J item picker's apply).
func (o *AnalyzeOutput) SetExcluded(keys []string) {
	o.excluded = make(map[string]bool, len(keys))
	for _, k := range keys {
		o.excluded[k] = true
	}
}

// SelectedItems returns the generated items minus the deselected ones
// — exactly what WriteAnalyze persists.
func (o *AnalyzeOutput) SelectedItems() []config.Item {
	if o == nil {
		return nil
	}
	if len(o.excluded) == 0 {
		return o.items
	}
	sel := make([]config.Item, 0, len(o.items))
	for _, it := range o.items {
		if !o.excluded[ItemKey(it)] {
			sel = append(sel, it)
		}
	}

	return sel
}

// ExcludedCount reports how many generated items the picker deselected.
func (o *AnalyzeOutput) ExcludedCount() int {
	if o == nil || len(o.excluded) == 0 {
		return 0
	}

	return len(o.items) - len(o.SelectedItems())
}

// GeneratedItems returns the config items this result would write
// (§J preview/write legs); nil for results not produced by
// RunAnalyze/RunAnalyzeEngine callers that attach them.
func (o *AnalyzeOutput) GeneratedItems() []config.Item {
	if o == nil {
		return nil
	}

	return o.items
}

// AttachGeneratedItems appends generated config items to a result view
// (the RunAnalyze merge path calls it; exported so §J test fakes can
// build realistic outputs without running the engine).
func (o *AnalyzeOutput) AttachGeneratedItems(items []config.Item) {
	if o == nil {
		return
	}
	o.items = append(o.items, items...)
}

// AnalyzeFlowView is one aggregated transaction flow (an
// analyzer.CapturedFlow without the in-memory messages). The
// direction/histogram fields are the §J enumeration extras
// (omitempty keeps the analyze report wire format unchanged
// when unset).
type AnalyzeFlowView struct {
	Key   string `json:"key"`
	MTI   string `json:"mti"`
	DE3   string `json:"de3"`
	DE22  string `json:"de22"`
	Count int    `json:"count"`

	Direction    string            `json:"direction,omitempty"`     // "dst" (requests) or "src" (responses)
	ServerPort   int               `json:"server_port,omitempty"`   // the flow's server port
	PeerPort     int               `json:"peer_port,omitempty"`     // the other end of the conversation
	MTIHistogram []AnalyzeMTICount `json:"mti_histogram,omitempty"` // deterministic: count desc, MTI asc
	SignonCount  int               `json:"signon_count,omitempty"`  // 0800/0810 messages in the flow
}

// AnalyzeMTICount is one MTI bucket of a §J enumeration histogram.
type AnalyzeMTICount struct {
	MTI   string `json:"mti"`
	Count int    `json:"count"`
}

// AnalyzePairView is one correlated request/response pair from scenario
// analysis.
type AnalyzePairView struct {
	Index            int    `json:"index"`
	Label            string `json:"label"`
	RequestMTI       string `json:"request_mti,omitempty"`
	ResponseMTI      string `json:"response_mti,omitempty"`
	HasReversal      bool   `json:"has_reversal"`
	IncludedReversal bool   `json:"included_reversal"`
}

// NewAnalyzeOutputFromAnalysis builds the result of a variance analysis
// run: flows is the output of StreamAnalyzer.AggregateFlows, results the
// flat list of analyzer.VarianceEngine outcomes for the selected flows
// (in mock-route mode the outcomes describe generated mock routes), and
// outputFile the transaction/route file the items were saved to.
func NewAnalyzeOutputFromAnalysis(
	streamFile string,
	headerType string,
	direction analyzer.TrafficDirection,
	unsecure bool,
	flows map[string]*analyzer.CapturedFlow,
	results []*analyzer.VarianceResult,
	mockRouteGoal bool,
	outputFile string,
) *AnalyzeOutput {
	mode := "transactions"
	if mockRouteGoal {
		mode = "mock_routes"
	}

	out := &AnalyzeOutput{
		Mode:           mode,
		StreamFile:     streamFile,
		HeaderType:     headerType,
		DirectionMode:  direction.Mode,
		DirectionLabel: direction.Label,
		TargetPort:     int(direction.TargetPort),
		PacketCount:    direction.PacketCount,
		ByteCount:      direction.ByteCount,
		Unsecure:       unsecure,
		OutputFile:     outputFile,
	}

	keys := make([]string, 0, len(flows))
	for key := range flows {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out.Flows = make([]AnalyzeFlowView, 0, len(keys))
	for _, key := range keys {
		flow := flows[key]
		if flow == nil {
			continue
		}
		out.Flows = append(out.Flows, AnalyzeFlowView{
			Key:   key,
			MTI:   flow.MTI,
			DE3:   flow.DE3,
			DE22:  flow.DE22,
			Count: flow.Count,
		})
		out.ExtractedMessages += flow.Count
	}
	out.FlowCount = len(out.Flows)

	for _, res := range results {
		if res == nil {
			continue
		}
		if res.Transaction.Name != "" {
			out.GeneratedTransactionNames = append(out.GeneratedTransactionNames, res.Transaction.Name)
		}
		if res.Dataset.Name != "" && len(res.Dataset.Data) > 0 {
			out.GeneratedDatasetNames = append(out.GeneratedDatasetNames, res.Dataset.Name)
		}
	}

	return out
}

// NewAnalyzeOutputFromScenarioScaffold builds the result of a scenario
// analysis run: pairs is the correlator output in scaffold order,
// includeReversals maps selected-pair index to whether the reversal step
// was kept, and scaffold is the analyzer.ScenarioBuilder output.
func NewAnalyzeOutputFromScenarioScaffold(
	streamFile string,
	headerType string,
	unsecure bool,
	pairs []*analyzer.CorrelatedPair,
	includeReversals map[int]bool,
	scenarioName string,
	scaffold *analyzer.ScenarioScaffoldResult,
	outputFile string,
) *AnalyzeOutput {
	out := &AnalyzeOutput{
		Mode:         "scenario",
		StreamFile:   streamFile,
		HeaderType:   headerType,
		Unsecure:     unsecure,
		ScenarioName: scenarioName,
		OutputFile:   outputFile,
	}

	out.Pairs = make([]AnalyzePairView, 0, len(pairs))
	for i, pair := range pairs {
		if pair == nil {
			continue
		}
		view := AnalyzePairView{
			Index:            i + 1,
			Label:            pair.Label,
			RequestMTI:       messageMTI(pair.Request),
			ResponseMTI:      messageMTI(pair.Response),
			HasReversal:      pair.Reversal != nil,
			IncludedReversal: includeReversals[i],
		}
		out.Pairs = append(out.Pairs, view)
	}
	out.PairCount = len(out.Pairs)
	out.ScenarioStepCount = out.PairCount

	if scaffold == nil {
		return out
	}

	if scaffold.Scenario.Name != "" {
		out.ScenarioName = scaffold.Scenario.Name
	}

	out.GeneratedTransactionNames = itemNames(scaffold.Transactions)
	out.GeneratedDatasetNames = itemNames(scaffold.Datasets)
	out.GeneratedMockRouteNames = itemNames(scaffold.MockRoutes)

	return out
}

func itemNames(items []config.Item) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item.Name != "" {
			names = append(names, item.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}

	return names
}

func messageMTI(message *analyzer.AnnotatedMessage) string {
	if message == nil || message.Message == nil {
		return ""
	}

	mti, err := message.Message.GetMTI()
	if err != nil {
		return ""
	}

	return mti
}
