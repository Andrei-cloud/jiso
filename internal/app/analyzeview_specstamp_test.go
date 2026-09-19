// analyzeview_specstamp_test.go pins the scenario extract's spec
// provenance: the generated scenario item must record the spec the
// capture was analyzed with (F12.1), so the spec gate can later seat on
// it and the extract says what it is.
package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"jiso/internal/analyzer"
	"jiso/internal/config"
)

func TestStampCoversScenarioItem(t *testing.T) {
	pcap, spec := analyzeFixture(t)
	a := analyzeApp(t)

	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: spec,
		Mode:  AnalyzeModeScenario,
		Flows: []FlowSelection{{Port: 8080, Dir: analyzer.DirectionDst}},
	})
	require.NoError(t, err)

	var scenarios, txs int
	for _, it := range out.GeneratedItems() {
		switch it.Type {
		case config.TypeScenario:
			scenarios++
			require.Equal(t, spec, it.Spec, "the scenario item must record its spec")
		case config.TypeTransaction:
			txs++
			require.Equal(t, spec, it.Spec)
		}
	}
	require.NotZero(t, scenarios, "fixture should produce a scenario item")
	require.NotZero(t, txs)
}
