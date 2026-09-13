package analyzer

import (
	"fmt"
	"os"
	"testing"
	"time"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/config"
	"jiso/internal/server"
	"jiso/internal/service"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// scenario_builder_exec_test.go runs the built scenarios end to end
// against the in-process mock server (the heavy execution legs).

func TestScenarioBuilder_FullPCAPExecutionWithMockServer(t *testing.T) {
	t.Parallel()

	spec := utils.GetDefaultSpec()

	cards := []string{
		"4000111122223333",
		"4000222233334444",
		"4000333344445555",
		"4000444455556666",
		"4000555566667777",
	}
	rcs := []string{"00", "51", "00", "51", "00"}

	var pairs []*CorrelatedPair
	for i := range cards {
		req := iso8583.NewMessage(spec)
		req.MTI("0100")
		require.NoError(t, req.Field(2, cards[i]))
		require.NoError(t, req.Field(3, "000000"))
		require.NoError(t, req.Field(4, fmt.Sprintf("%d000", (i+1)*10)))
		require.NoError(t, req.Field(11, fmt.Sprintf("%06d", i+1)))
		require.NoError(t, req.Field(41, fmt.Sprintf("TERM%04d", i+1)))
		require.NoError(t, req.Field(49, "840"))

		resp := iso8583.NewMessage(spec)
		resp.MTI("0110")
		require.NoError(t, resp.Field(2, cards[i]))
		require.NoError(t, resp.Field(3, "000000"))
		require.NoError(t, resp.Field(4, fmt.Sprintf("%d000", (i+1)*10)))
		require.NoError(t, resp.Field(11, fmt.Sprintf("%06d", i+1)))
		require.NoError(t, resp.Field(38, fmt.Sprintf("AUTH%02d", i+1)))
		require.NoError(t, resp.Field(39, rcs[i]))
		require.NoError(t, resp.Field(41, fmt.Sprintf("TERM%04d", i+1)))
		require.NoError(t, resp.Field(49, "840"))

		var revReq *AnnotatedMessage
		var revResp *AnnotatedMessage
		if i == 0 || i == 2 {
			revM := iso8583.NewMessage(spec)
			revM.MTI("0400")
			require.NoError(t, revM.Field(2, cards[i]))
			require.NoError(t, revM.Field(3, "000000"))
			require.NoError(t, revM.Field(4, fmt.Sprintf("%d000", (i+1)*10)))
			require.NoError(t, revM.Field(11, fmt.Sprintf("%06d", i+100)))
			require.NoError(t, revM.Field(38, fmt.Sprintf("AUTH%02d", i+1)))
			require.NoError(t, revM.Field(41, fmt.Sprintf("TERM%04d", i+1)))
			require.NoError(t, revM.Field(49, "840"))
			require.NoError(t, revM.Field(90, fmt.Sprintf("0100%06d000000000000000000000000", i+1)))
			revReq = &AnnotatedMessage{Message: revM, Direction: DirectionRequest}

			revRM := iso8583.NewMessage(spec)
			revRM.MTI("0410")
			require.NoError(t, revRM.Field(2, cards[i]))
			require.NoError(t, revRM.Field(3, "000000"))
			require.NoError(t, revRM.Field(4, fmt.Sprintf("%d000", (i+1)*10)))
			require.NoError(t, revRM.Field(11, fmt.Sprintf("%06d", i+100)))
			require.NoError(t, revRM.Field(38, fmt.Sprintf("AUTH%02d", i+1)))
			require.NoError(t, revRM.Field(39, "00"))
			require.NoError(t, revRM.Field(41, fmt.Sprintf("TERM%04d", i+1)))
			require.NoError(t, revRM.Field(49, "840"))
			revResp = &AnnotatedMessage{Message: revRM, Direction: DirectionResponse}
		}

		pairs = append(pairs, &CorrelatedPair{
			Request:      &AnnotatedMessage{Message: req, Direction: DirectionRequest},
			Response:     &AnnotatedMessage{Message: resp, Direction: DirectionResponse},
			Reversal:     revReq,
			ReversalResp: revResp,
			Label:        fmt.Sprintf("Correlated Pair #%d", i+1),
		})
	}

	includeReversals := map[int]bool{0: true, 2: true}

	builder := NewScenarioBuilder(spec, true)
	opts := ScenarioScaffoldOptions{
		ScenarioName:       "Synthetic PCAP Scaffolded Scenario",
		IncludeReversals:   includeReversals,
		GenerateMockRoutes: true,
		Unsecure:           true,
	}

	scaffold, err := builder.Build(pairs, opts)
	require.NoError(t, err)
	require.NotNil(t, scaffold)

	var items []config.Item
	items = append(items, scaffold.Transactions...)
	items = append(items, scaffold.Datasets...)
	items = append(items, scaffold.Scenario)
	items = append(items, scaffold.MockRoutes...)

	tmpFile, err := os.CreateTemp(t.TempDir(), "synthetic_pcap_scaffold_*.json")
	require.NoError(t, err)
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	data, err := json.MarshalIndent(items, "", "  ")
	require.NoError(t, err)
	_, err = tmpFile.Write(data)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	// Load into TransactionCollection
	tc, err := transactions.NewTransactionCollection(tmpFile.Name(), spec)
	require.NoError(t, err)

	var mockRoutes []config.MockRouteConfig
	for _, r := range scaffold.MockRoutes {
		mockRoutes = append(mockRoutes, config.MockRouteConfig{
			Name:           r.Name,
			Description:    r.Description,
			MatchFields:    r.MatchFields,
			RequiredFields: r.RequiredFields,
			EchoFields:     r.EchoFields,
			ResponseMTI:    r.ResponseMTI,
			ResponseFields: r.ResponseFields,
			LatencyMs:      1,
			JitterMs:       1,
		})
	}
	require.NotEmpty(t, mockRoutes)

	// Start Mock Server
	mockServer := server.NewServer(spec, mockRoutes, "binary2")
	require.NoError(t, mockServer.Start("19895"))
	defer func() {
		_ = mockServer.Stop()
	}()

	time.Sleep(50 * time.Millisecond)

	// Connect client
	svc, err := service.NewService(
		"127.0.0.1", "19895", "", false, 1, 2*time.Second, 5*time.Second, 2*time.Second,
	)
	require.NoError(t, err)
	svc.SetSpec(spec)

	h, err := utils.SelectLength("binary2")
	require.NoError(t, err)
	require.NoError(t, svc.Connect(false, h))
	defer func() {
		_ = svc.Disconnect()
	}()

	// Run scenario
	runner := transactions.NewScenarioRunner(svc, tc)
	report, err := runner.RunScenario("Synthetic PCAP Scaffolded Scenario")
	require.NoError(t, err)
	require.NotNil(t, report)

	for idx, step := range report.Steps {
		t.Logf("Step %d: %s -> success=%v (err=%s, validation_errs=%v)", idx+1, step.StepName, step.Success, step.Error, step.ValidationErrors)
	}

	assert.True(t, report.Success, "All steps in synthetic PCAP scaffolded scenario must pass 100%%")
}

func TestScenarioBuilder_Anonymized14StepExecutionWithMockServer(t *testing.T) {
	t.Parallel()

	spec := utils.GetDefaultSpec()

	// 14 Steps imitating real PCAP captured transactions
	// Steps with different processing codes, approvals (00), declines (51), and reversals
	cards := []string{
		"4174480011112222", // Step 1: 0100 DE3=000000 -> 00
		"4085658933334444", // Step 2: 0100 DE3=000000 -> 00
		"4000123456789010", // Step 3: 0100 DE3=200000 -> 00
		"4111222233334444", // Step 4: 0100 DE3=100000 -> 00
		"4222333344445555", // Step 5: 0100 DE3=100000 -> 00
		"4333444455556666", // Step 6: 0100 DE3=110000 -> 00
		"4444555566667777", // Step 7: 0100 DE3=100000 -> 51 (Card-specific decline)
		"4555666677778888", // Step 8: 0100 DE3=100000 -> 00
		"4666777788889999", // Step 9: 0100 DE3=003000 -> 00
		"4777888899990000", // Step 10: 0100 DE3=200000 -> 00
		"4888999900001111", // Step 11: 0100 DE3=100000 -> 00
		"4999000011112222", // Step 12: 0100 DE3=100000 -> 00
		"4444555566667777", // Step 13: 0100 DE3=100000 -> 51 (Card-specific decline again)
		"4000123456789010", // Step 14: 0100 DE3=200000 -> 00
	}

	de3s := []string{
		"0", "0", "200000", "100000", "100000", "110000", "100000",
		"100000", "3000", "200000", "100000", "100000", "100000", "200000",
	}

	rcs := []string{
		"00", "00", "00", "00", "00", "00", "51",
		"00", "00", "00", "00", "00", "51", "00",
	}

	var pairs []*CorrelatedPair
	for i := range cards {
		req := iso8583.NewMessage(spec)
		req.MTI("0100")
		require.NoError(t, req.Field(2, cards[i]))
		require.NoError(t, req.Field(3, de3s[i]))
		require.NoError(t, req.Field(4, fmt.Sprintf("%d00", (i+1)*50)))
		require.NoError(t, req.Field(11, fmt.Sprintf("%06d", i+1)))
		require.NoError(t, req.Field(41, fmt.Sprintf("TERM%04d", i+1)))
		require.NoError(t, req.Field(49, "840"))

		resp := iso8583.NewMessage(spec)
		resp.MTI("0110")
		require.NoError(t, resp.Field(2, cards[i]))
		require.NoError(t, resp.Field(3, de3s[i]))
		require.NoError(t, resp.Field(4, fmt.Sprintf("%d00", (i+1)*50)))
		require.NoError(t, resp.Field(11, fmt.Sprintf("%06d", i+1)))
		require.NoError(t, resp.Field(38, fmt.Sprintf("AUTH%02d", i+1)))
		require.NoError(t, resp.Field(39, rcs[i]))
		require.NoError(t, resp.Field(41, fmt.Sprintf("TERM%04d", i+1)))
		require.NoError(t, resp.Field(49, "840"))

		pairs = append(pairs, &CorrelatedPair{
			Request:  &AnnotatedMessage{Message: req, Direction: DirectionRequest},
			Response: &AnnotatedMessage{Message: resp, Direction: DirectionResponse},
			Label:    fmt.Sprintf("Step #%d DE3=%s", i+1, de3s[i]),
		})
	}

	// Build with Unsecure = false (SECURE ANONYMIZATION ENABLED!)
	builder := NewScenarioBuilder(spec, false)
	opts := ScenarioScaffoldOptions{
		ScenarioName:       "PCAP Captured 14-Step Test Scenario",
		GenerateMockRoutes: true,
		Unsecure:           false, // Security sanitization / anonymization ON
	}

	scaffold, err := builder.Build(pairs, opts)
	require.NoError(t, err)
	require.NotNil(t, scaffold)

	var items []config.Item
	items = append(items, scaffold.Transactions...)
	items = append(items, scaffold.Datasets...)
	items = append(items, scaffold.Scenario)
	items = append(items, scaffold.MockRoutes...)

	tmpDir := t.TempDir()
	txFile := tmpDir + "/anonymized_pcap_scenario.json"
	data, err := json.MarshalIndent(items, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(txFile, data, 0o644))

	tc, err := transactions.NewTransactionCollection(txFile, spec)
	require.NoError(t, err)

	var mockRoutes []config.MockRouteConfig
	for _, r := range scaffold.MockRoutes {
		mockRoutes = append(mockRoutes, config.MockRouteConfig{
			Name:           r.Name,
			Description:    r.Description,
			MatchFields:    r.MatchFields,
			RequiredFields: r.RequiredFields,
			EchoFields:     r.EchoFields,
			ResponseMTI:    r.ResponseMTI,
			ResponseFields: r.ResponseFields,
			LatencyMs:      1,
			JitterMs:       1,
		})
	}
	require.NotEmpty(t, mockRoutes)

	// Start Mock Server on dedicated port
	mockServer := server.NewServer(spec, mockRoutes, "binary2")
	require.NoError(t, mockServer.Start("19896"))
	defer func() {
		_ = mockServer.Stop()
	}()

	time.Sleep(50 * time.Millisecond)

	// Connect client
	svc, err := service.NewService(
		"127.0.0.1", "19896", "", false, 1, 2*time.Second, 5*time.Second, 2*time.Second,
	)
	require.NoError(t, err)
	svc.SetSpec(spec)

	h, err := utils.SelectLength("binary2")
	require.NoError(t, err)
	require.NoError(t, svc.Connect(false, h))
	defer func() {
		_ = svc.Disconnect()
	}()

	// Execute 14-step scenario against the Mock Server
	runner := transactions.NewScenarioRunner(svc, tc)
	report, err := runner.RunScenario("PCAP Captured 14-Step Test Scenario")
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, 14, len(report.Steps))
	for idx, step := range report.Steps {
		t.Logf("Step %d: %s -> success=%v (err=%s, validation_errs=%v)", idx+1, step.StepName, step.Success, step.Error, step.ValidationErrors)
		assert.True(t, step.Success, "Step %d (%s) must pass validation", idx+1, step.StepName)
	}

	assert.True(t, report.Success, "The entire 14-step anonymized PCAP scenario must pass with 100%% success!")
}
