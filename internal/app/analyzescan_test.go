// analyzescan_test.go pins the matching wizard's scan leg: ScanForMatch
// extracts and correlates the capture's dst flows once, hands back the
// correlated pairs the run will group, and reports the fields whose values
// varied so the group-by pane can suggest them.
package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"jiso/internal/config"

	"jiso/internal/analyzer"
)

func TestScanForMatchPairsAndVariances(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)

	scan, err := a.ScanForMatch(context.Background(), AnalyzeScanOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
	})
	require.NoError(t, err)

	// The fixture carries three 0200/0210 exchanges on 8080 and one on 9999.
	require.Len(t, scan.Pairs, 4)
	for _, p := range scan.Pairs {
		require.NotNil(t, p.Request)
		require.NotNil(t, p.Response)
	}

	// STAN (field 11) is the fixture's varying request field.
	var stan *FieldVariance
	for i := range scan.Variances {
		if scan.Variances[i].Field == "11" && scan.Variances[i].Side == analyzer.CondSideReq {
			stan = &scan.Variances[i]
		}
	}
	require.NotNil(t, stan, "STAN variance missing: %+v", scan.Variances)
	require.Equal(t, 4, stan.Distinct)
	require.NotEmpty(t, stan.Sample)

	// A constant field (DE 3 = 000000 everywhere) is not offered.
	for _, v := range scan.Variances {
		if v.Field == "3" {
			t.Errorf("constant field 3 offered as variance: %+v", v)
		}
	}
}

func TestScanForMatchMissingCapture(t *testing.T) {
	a := analyzeApp(t)
	_, err := a.ScanForMatch(context.Background(), AnalyzeScanOptions{PcapPath: "/nope/none.pcap"})
	require.Error(t, err)
}

// TestRunAnalyzeRoutesMatched pins the wizard's run path: with Match set,
// the routes goal groups the capture on the operator's fields and emits
// wizard routes (no auto-inferred PAN), while the legacy scaffold path
// stays reachable with Match == nil.
func TestRunAnalyzeRoutesMatched(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)

	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
		Mode:  AnalyzeModeRoutes,
		Flows: []FlowSelection{{Port: 8080, Dir: analyzer.DirectionDst}},
		Match: &analyzer.MatchSpec{
			Conditions: []analyzer.MatchCond{{Side: analyzer.CondSideReq, Field: "0", Cond: "equals", Value: "0200"}},
			GroupBy:    []analyzer.GroupField{{Field: "4", Side: analyzer.CondSideReq}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "mock_routes", out.Mode)

	var routes []config.Item
	for _, it := range out.GeneratedItems() {
		if it.Type == config.TypeMockRoute {
			routes = append(routes, it)
		}
	}
	require.NotEmpty(t, routes)
	for _, r := range routes {
		require.Equal(t, "0200", r.MatchFields["0"])
		require.Equal(t, "1000", r.MatchFields["4"], "the chosen group field must enter the match")
		_, hasPAN := r.MatchFields["2"]
		require.False(t, hasPAN, "the wizard never writes PAN into a match: %v", r.MatchFields)
	}
	require.NotEmpty(t, out.GeneratedMockRouteNames)
}
