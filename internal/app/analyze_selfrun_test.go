// analyze_selfrun_test.go is the finding-12 contract end to end: what the
// wizard writes, it runs. A capture analyzed under the scenario goal lands
// as ONE extract file; the §G serve leg starts from that same file (its
// routes-file loader skips the scenario/transaction/dataset entries), and
// the extract's own scenario then passes against that server — so the
// post-write "use it now" keys start from a promise that is already true.
package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"jiso/internal/analyzer"
	"jiso/internal/config"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

func TestExtractRunsRightAfterExtraction(t *testing.T) {
	pcap, specPath := analyzeFixture(t)
	a := serveLiveApp(t)

	// 1. The analyze the scenario goal runs (the same leg §J arms).
	out, err := a.RunAnalyze(context.Background(), AnalyzeRunOptions{
		PcapPath: pcap, HeaderType: "ascii4", SpecPath: specPath,
		Mode:  AnalyzeModeScenario,
		Flows: []FlowSelection{{Port: 8080, Dir: analyzer.DirectionDst}},
	})
	require.NoError(t, err)
	require.NotEmpty(t, out.SelectedItems())

	extract := filepath.Join(t.TempDir(), "captured-extract.json")
	require.NoError(t, config.SaveItems(extract, out.SelectedItems()))

	// 2. The extract names its routes: LoadRoutesFile — the very loader
	// behind the §G routes-file pick — keeps route entries and skips the
	// rest without a fuss.
	routes, err := LoadRoutesFile(extract)
	require.NoError(t, err)
	require.NotEmpty(t, routes)
	require.Less(t, len(routes), len(out.SelectedItems()),
		"non-route entries must be skipped, not counted")
	for _, r := range routes {
		require.NotContains(t, r.MatchFields, "2", "extracted routes carry no card match")
	}

	// 3. §G start with the extract itself as the routes file (what [g]
	// pre-fills): ephemeral port, the snapshot names the real one.
	require.NoError(t, a.ServeStart("0", "ascii4", specPath, "", extract))
	snap := a.ServeSnapshot()
	require.True(t, snap.Running)
	require.NotEmpty(t, snap.Port)
	require.NotEqual(t, "0", snap.Port)

	// 4. The extract as the transactions file, run over a real connection
	// — the same composition the §F run performs.
	spec, err := utils.CreateSpecFromFile(specPath)
	require.NoError(t, err)
	tc, err := transactions.NewTransactionCollection(extract, spec)
	require.NoError(t, err)

	svc, err := service.NewService("127.0.0.1", snap.Port, "", false, 1,
		2*time.Second, 5*time.Second, 2*time.Second)
	require.NoError(t, err)
	svc.SetSpec(spec)
	h, err := utils.SelectLength("ascii4")
	require.NoError(t, err)
	require.NoError(t, svc.Connect(false, h))
	defer func() { _ = svc.Disconnect() }()

	runner := transactions.NewScenarioRunner(svc, tc)
	report, err := runner.RunScenario(out.ScenarioName)
	require.NoError(t, err)
	require.NotEmpty(t, report.Steps)
	for _, step := range report.Steps {
		require.True(t, step.Success, "step %s must replay: err=%s validation=%v",
			step.StepName, step.Error, step.ValidationErrors)
	}
	require.True(t, report.Success, "the freshly written extract must run right after extraction")

	// The server saw the scenario's traffic (the steps really hit it).
	final := a.ServeSnapshot()
	require.GreaterOrEqual(t, final.Matched, int64(len(report.Steps)))
}
