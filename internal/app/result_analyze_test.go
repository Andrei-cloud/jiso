package app

import (
	"testing"

	"github.com/moov-io/iso8583"

	"jiso/internal/analyzer"
	"jiso/internal/config"
	"jiso/internal/utils"
)

func analyzeMessage(mti string) *iso8583.Message {
	msg := iso8583.NewMessage(utils.GetDefaultSpec())
	msg.MTI(mti)

	return msg
}

func analyzeFlowsFixture() map[string]*analyzer.CapturedFlow {
	return map[string]*analyzer.CapturedFlow{
		"0200/000000/059000": {MTI: "0200", DE3: "000000", DE22: "059000", Count: 4},
		"0800/000000/":       {MTI: "0800", DE3: "000000", Count: 1},
		"broken/":            nil,
	}
}

func analyzeResultsFixture() []*analyzer.VarianceResult {
	return []*analyzer.VarianceResult{
		{
			Transaction: config.Item{Name: "Purchase Template"},
			Dataset: config.Item{
				Name: "Purchase Dataset",
				Data: []map[string]string{{"2": "4242424242424242"}},
			},
		},
		{Transaction: config.Item{Name: "Purchase Declined Template"}},
		{Transaction: config.Item{Name: "Echo Route", ResponseMTI: "0810"}, Dataset: config.Item{Name: "Empty DS"}},
	}
}

func TestNewAnalyzeOutputFromAnalysis(t *testing.T) {
	t.Parallel()

	direction := analyzer.TrafficDirection{Label: "port 8583 dst", TargetPort: 8583, Mode: "dst", PacketCount: 5, ByteCount: 512}

	tests := []struct {
		name          string
		mockRouteGoal bool
		wantMode      string
		wantExtracted int
		wantFlows     int
		wantTxNames   []string
		wantDsNames   []string
	}{
		{
			name:          "transaction templates mode",
			mockRouteGoal: false,
			wantMode:      "transactions",
			wantExtracted: 5,
			wantFlows:     2,
			wantTxNames:   []string{"Purchase Template", "Purchase Declined Template", "Echo Route"},
			wantDsNames:   []string{"Purchase Dataset"},
		},
		{
			name:          "mock routes mode",
			mockRouteGoal: true,
			wantMode:      "mock_routes",
			wantExtracted: 5,
			wantFlows:     2,
			wantTxNames:   []string{"Purchase Template", "Purchase Declined Template", "Echo Route"},
			wantDsNames:   []string{"Purchase Dataset"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewAnalyzeOutputFromAnalysis(
				"capture.pcap", "binary2", direction, false,
				analyzeFlowsFixture(), analyzeResultsFixture(), tt.mockRouteGoal,
				"./transactions/transaction.json",
			)

			if got.Mode != tt.wantMode {
				t.Errorf("Mode = %q, want %q", got.Mode, tt.wantMode)
			}
			if got.ExtractedMessages != tt.wantExtracted {
				t.Errorf("ExtractedMessages = %d, want %d", got.ExtractedMessages, tt.wantExtracted)
			}
			if got.FlowCount != tt.wantFlows || len(got.Flows) != tt.wantFlows {
				t.Fatalf("FlowCount = %d / flows %+v, want %d", got.FlowCount, got.Flows, tt.wantFlows)
			}
			if got.Flows[0].Key != "0200/000000/059000" {
				t.Errorf("flows not sorted by key: %v", got.Flows[0].Key)
			}
			if got.TargetPort != 8583 || got.DirectionMode != "dst" {
				t.Errorf("direction = %+v, want port 8583 dst", got)
			}
			if len(got.GeneratedTransactionNames) != len(tt.wantTxNames) {
				t.Errorf("GeneratedTransactionNames = %v, want %v", got.GeneratedTransactionNames, tt.wantTxNames)
			}
			if len(got.GeneratedDatasetNames) != len(tt.wantDsNames) {
				t.Errorf("GeneratedDatasetNames = %v, want %v", got.GeneratedDatasetNames, tt.wantDsNames)
			}
		})
	}
}

func TestNewAnalyzeOutputFromScenarioScaffold(t *testing.T) {
	t.Parallel()

	pairs := []*analyzer.CorrelatedPair{
		{
			Label:    "0200 -> 0210 STAN 1",
			Request:  &analyzer.AnnotatedMessage{Message: analyzeMessage("0200")},
			Response: &analyzer.AnnotatedMessage{Message: analyzeMessage("0210")},
			Reversal: &analyzer.AnnotatedMessage{Message: analyzeMessage("0420")},
		},
		{
			Label:    "0800 -> 0810 network",
			Request:  &analyzer.AnnotatedMessage{Message: analyzeMessage("0800")},
			Response: &analyzer.AnnotatedMessage{Message: analyzeMessage("0810")},
		},
		nil,
	}
	scaffold := &analyzer.ScenarioScaffoldResult{
		Transactions: []config.Item{{Name: "Purchase Template"}},
		Datasets:     []config.Item{{Name: "Purchase Dataset"}},
		Scenario:     config.Item{Name: "PCAP Captured Test Scenario"},
		MockRoutes:   []config.Item{{Name: "Purchase Approved"}},
	}

	got := NewAnalyzeOutputFromScenarioScaffold(
		"capture.pcap", "ascii4", true,
		pairs, map[int]bool{0: true}, "My Scenario", scaffold, "./transactions/transaction.json",
	)

	if got.Mode != "scenario" {
		t.Errorf("Mode = %q, want scenario", got.Mode)
	}
	if got.PairCount != 2 || got.ScenarioStepCount != 2 {
		t.Errorf("PairCount/StepCount = %d/%d, want 2/2", got.PairCount, got.ScenarioStepCount)
	}
	if got.ScenarioName != "PCAP Captured Test Scenario" {
		t.Errorf("ScenarioName = %q, want scaffold scenario name to win", got.ScenarioName)
	}
	if got.Pairs[0].RequestMTI != "0200" || got.Pairs[0].ResponseMTI != "0210" {
		t.Errorf("pair 0 MTIs = %q/%q, want 0200/0210", got.Pairs[0].RequestMTI, got.Pairs[0].ResponseMTI)
	}
	if !got.Pairs[0].HasReversal || !got.Pairs[0].IncludedReversal {
		t.Error("pair 0 should have and include a reversal")
	}
	if got.Pairs[1].HasReversal || got.Pairs[1].IncludedReversal {
		t.Error("pair 1 should have no reversal")
	}
	if len(got.GeneratedMockRouteNames) != 1 || got.GeneratedMockRouteNames[0] != "Purchase Approved" {
		t.Errorf("GeneratedMockRouteNames = %v, want [Purchase Approved]", got.GeneratedMockRouteNames)
	}

	// Failure shape: correlator produced nothing usable.
	empty := NewAnalyzeOutputFromScenarioScaffold("broken.pcap", "binary2", false, nil, nil, "", nil, "")
	if empty.PairCount != 0 || empty.Pairs == nil || empty.ScenarioName != "" {
		t.Errorf("empty scaffold view = %+v, want zeroed sections", empty)
	}
}

func TestAnalyzeOutputJSONRoundTrip(t *testing.T) {
	t.Parallel()

	direction := analyzer.TrafficDirection{Label: "all", Mode: "all", PacketCount: 6, ByteCount: 640}

	tests := []struct {
		name     string
		output   *AnalyzeOutput
		wantKeys []string
	}{
		{
			name: "variance analysis shape",
			output: NewAnalyzeOutputFromAnalysis(
				"capture.pcap", "binary2", direction, false,
				analyzeFlowsFixture(), analyzeResultsFixture(), false, "./transactions/transaction.json",
			),
			wantKeys: []string{"mode", "stream_file", "flows", "mti", "generated_transaction_names"},
		},
		{
			name: "scenario scaffold shape",
			output: NewAnalyzeOutputFromScenarioScaffold(
				"capture.pcap", "binary2", false,
				[]*analyzer.CorrelatedPair{{Label: "p1", Request: &analyzer.AnnotatedMessage{Message: analyzeMessage("0200")}}},
				nil, "Scenario", &analyzer.ScenarioScaffoldResult{Scenario: config.Item{Name: "Scenario"}},
				"./transactions/transaction.json",
			),
			wantKeys: []string{"scenario_name", "pairs", "request_mti", "scenario_step_count"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundTrip(t, tt.output, tt.wantKeys)
		})
	}
}
