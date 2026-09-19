// analyzescan_test.go pins the matching wizard's scan leg: ScanForMatch
// extracts and correlates the capture's dst flows once, hands back the
// correlated pairs the run will group, and reports the fields whose values
// varied so the group-by pane can suggest them.
package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

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
