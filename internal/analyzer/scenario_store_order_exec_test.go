package analyzer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/require"

	"jiso/internal/config"
	"jiso/internal/server"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// scenario_store_order_exec_test.go pins the persistence leg of the route
// answer order: what the scaffold decides (most-seen answer first) must
// survive config.SaveItems' alphabetical-by-name store, because the mock
// server answers with the first full match in file order.
// TestScaffoldRouteOrderSurvivesSaveItemsRoundTrip: the generated-items
// store (config.SaveItems) persists items sorted BY NAME, while the mock
// server answers with the first full match in FILE order. The scaffold's
// answer-frequency order must survive that alphabetical store or the
// rare approval silently beats the dominant decline again (the UAT
// echo-back finding: 83 steps got the 2 approvals' answer because
// "RC=00" sorts before "RC=06"). Rank-led route names carry the
// semantics through the store; the scenario here proves it: the three
// steps that captured 06 pass when served straight from the saved file.
func TestScaffoldRouteOrderSurvivesSaveItemsRoundTrip(t *testing.T) {
	t.Parallel()

	spec := utils.GetDefaultSpec()

	mkPair := func(rc, tag string) *CorrelatedPair {
		req := iso8583.NewMessage(spec)
		req.MTI("0100")
		require.NoError(t, req.Field(3, "000000"))
		require.NoError(t, req.Field(11, tag))

		resp := iso8583.NewMessage(spec)
		resp.MTI("0110")
		require.NoError(t, resp.Field(3, "000000"))
		require.NoError(t, resp.Field(11, tag))
		require.NoError(t, resp.Field(39, rc))
		require.NoError(t, resp.Field(48, "BATCH-"+tag))

		return &CorrelatedPair{
			Request:  &AnnotatedMessage{Message: req, Direction: DirectionRequest},
			Response: &AnnotatedMessage{Message: resp, Direction: DirectionResponse},
			Label:    "Pair " + tag,
		}
	}
	pairs := []*CorrelatedPair{
		mkPair("00", "000001"), mkPair("06", "000002"), mkPair("06", "000003"),
		mkPair("06", "000004"), mkPair("00", "000005"),
	}

	scaffold, err := NewScenarioBuilder(spec, true).Build(pairs, ScenarioScaffoldOptions{
		ScenarioName:       "Store Order",
		GenerateMockRoutes: true,
		Unsecure:           true,
	})
	require.NoError(t, err)

	items := append(append(append([]config.Item{},
		scaffold.Transactions...), scaffold.Scenario), scaffold.MockRoutes...)

	store := filepath.Join(t.TempDir(), "store.json")
	require.NoError(t, config.SaveItems(store, items))

	// What the mock server sees on disk: the shared match must be led by
	// the dominant answer after the store's alphabetical sort.
	saved, err := os.ReadFile(store)
	require.NoError(t, err)
	var loaded []config.Item
	require.NoError(t, json.Unmarshal(saved, &loaded))

	seenRoute := false
	for _, it := range loaded {
		if it.Type != config.TypeMockRoute {
			continue
		}
		if mti, _ := it.MatchFields["0"].(string); mti != "0100" {
			continue
		}
		require.Equal(t, "06", it.ResponseFields["39"],
			"the saved order must lead with the most-seen answer")
		seenRoute = true
		break
	}
	require.True(t, seenRoute, "the store carried no 0100 route at all")

	// Serve from the STORE's route order and run the scaffolded scenario:
	// the three steps that captured 06 pass, proving the store's
	// alphabetical order hands the shared match to the dominant answer.
	var mockRoutes []config.MockRouteConfig
	for _, it := range loaded {
		if it.Type != config.TypeMockRoute {
			continue
		}
		mockRoutes = append(mockRoutes, config.MockRouteConfig{
			Name:           it.Name,
			Description:    it.Description,
			MatchFields:    it.MatchFields,
			RequiredFields: it.RequiredFields,
			EchoFields:     it.EchoFields,
			ResponseMTI:    it.ResponseMTI,
			ResponseFields: it.ResponseFields,
			LatencyMs:      1,
			JitterMs:       1,
		})
	}
	require.NotEmpty(t, mockRoutes)

	mockServer := server.NewServer(spec, mockRoutes, "binary2")
	require.NoError(t, mockServer.Start("19901"))
	defer func() { _ = mockServer.Stop() }()
	time.Sleep(50 * time.Millisecond)

	tc, err := transactions.NewTransactionCollection(store, spec)
	require.NoError(t, err)

	svc, err := service.NewService(
		"127.0.0.1", "19901", "", false, 1, 2*time.Second, 5*time.Second, 2*time.Second,
	)
	require.NoError(t, err)
	svc.SetSpec(spec)

	h, err := utils.SelectLength("binary2")
	require.NoError(t, err)
	require.NoError(t, svc.Connect(false, h))
	defer func() { _ = svc.Disconnect() }()

	runner := transactions.NewScenarioRunner(svc, tc)
	report, err := runner.RunScenario("Store Order")
	require.NoError(t, err)

	ok := 0
	for _, s := range report.Steps {
		if s.Success {
			ok++
		}
	}
	require.Equal(t, 3, ok, "the dominant answer must serve the shared match out of the store")
}
