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

func TestScenarioBuilder_BasicScaffold(t *testing.T) {
	spec := utils.GetDefaultSpec()

	rawPAN := "4532012345678912" // 16 digit PAN
	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0200")
	require.NoError(t, reqMsg.Field(2, rawPAN))
	require.NoError(t, reqMsg.Field(3, "000000"))
	require.NoError(t, reqMsg.Field(4, "1000"))
	require.NoError(t, reqMsg.Field(11, "000001"))

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0210")
	require.NoError(t, respMsg.Field(3, "000000"))
	require.NoError(t, respMsg.Field(11, "000001"))
	require.NoError(t, respMsg.Field(38, "AUTH01"))
	require.NoError(t, respMsg.Field(39, "00"))

	pair := &CorrelatedPair{
		Request:  &AnnotatedMessage{Message: reqMsg, Direction: DirectionRequest},
		Response: &AnnotatedMessage{Message: respMsg, Direction: DirectionResponse},
		Label:    "Test Pair",
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:     "Test Scaffold Scenario",
		IncludeReversals: map[int]bool{0: false},
	}

	result, err := builder.Build([]*CorrelatedPair{pair}, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify generated Transaction template
	require.Len(t, result.Transactions, 1)
	tx := result.Transactions[0]
	assert.Equal(t, config.TypeTransaction, tx.Type)

	var txFields map[string]interface{}
	err = json.Unmarshal(tx.Fields, &txFields)
	require.NoError(t, err)

	// Check PAN anonymization: BIN prefix '45320123' preserved, positions 14-16 '000'
	anonymizedPAN, ok := txFields["2"].(string)
	require.True(t, ok)
	assert.Equal(t, 16, len(anonymizedPAN))
	assert.Equal(t, "45320123", anonymizedPAN[:8])
	assert.Equal(t, "000", anonymizedPAN[13:16])
	assert.NotEqual(t, rawPAN, anonymizedPAN)

	// Verify System Fields set to "auto"
	assert.Equal(t, "auto", txFields["11"])

	// Verify generated Scenario ConfigItem
	assert.Equal(t, config.TypeScenario, result.Scenario.Type)
	assert.Equal(t, "Test Scaffold Scenario", result.Scenario.Name)

	var steps []transactions.ScenarioStep
	err = json.Unmarshal(result.Scenario.Steps, &steps)
	require.NoError(t, err)
	require.Len(t, steps, 1)

	assert.Equal(t, tx.Name, steps[0].UseTransactionId)
	require.Len(t, steps[0].Validate, 1)
	assert.Equal(t, "39", steps[0].Validate[0].Field)
	assert.Equal(t, "00", steps[0].Validate[0].Expect)
}

func TestScenarioBuilder_ReversalStep(t *testing.T) {
	spec := utils.GetDefaultSpec()

	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0200")
	require.NoError(t, reqMsg.Field(2, "4111111111111111"))
	require.NoError(t, reqMsg.Field(3, "000000"))
	require.NoError(t, reqMsg.Field(11, "000001"))

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0210")
	require.NoError(t, respMsg.Field(3, "000000"))
	require.NoError(t, respMsg.Field(11, "000001"))
	require.NoError(t, respMsg.Field(38, "AUTH01"))
	require.NoError(t, respMsg.Field(39, "00"))

	pair := &CorrelatedPair{
		Request:  &AnnotatedMessage{Message: reqMsg, Direction: DirectionRequest},
		Response: &AnnotatedMessage{Message: respMsg, Direction: DirectionResponse},
		Label:    "Test Pair",
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:     "Reversal Test Scenario",
		IncludeReversals: map[int]bool{0: true},
	}

	result, err := builder.Build([]*CorrelatedPair{pair}, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// 2 transaction templates: Request + Reversal
	require.Len(t, result.Transactions, 2)

	// Scenario should have 2 steps: Request + Reversal
	var steps []transactions.ScenarioStep
	err = json.Unmarshal(result.Scenario.Steps, &steps)
	require.NoError(t, err)
	require.Len(t, steps, 2)

	// First step must extract context variables needed for reversal
	assert.NotNil(t, steps[0].Extract)
	assert.Equal(t, "38", steps[0].Extract["AuthId"])
	assert.Equal(t, "11", steps[0].Extract["OrigSTAN"])
	assert.Equal(t, "7", steps[0].Extract["OrigDateTime"])

	// Second step is reversal
	assert.Contains(t, steps[1].Name, "Reversal")

	// Verify Reversal Transaction template fields
	revTx := result.Transactions[1]
	var revFields map[string]interface{}
	err = json.Unmarshal(revTx.Fields, &revFields)
	require.NoError(t, err)

	assert.Equal(t, "0400", revFields["0"])
	assert.Equal(t, "{{context.AuthId}}", revFields["38"])
	assert.Contains(t, revFields["90"], "{{context.OrigMTI}}{{context.OrigSTAN}}{{context.OrigDateTime}}")
}

func TestScenarioBuilder_MockRoutes(t *testing.T) {
	spec := utils.GetDefaultSpec()

	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0200")
	require.NoError(t, reqMsg.Field(3, "000000"))
	require.NoError(t, reqMsg.Field(11, "000001"))

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0210")
	require.NoError(t, respMsg.Field(3, "000000"))
	require.NoError(t, respMsg.Field(11, "000001"))
	require.NoError(t, respMsg.Field(38, "AUTH01"))
	require.NoError(t, respMsg.Field(39, "00"))

	pair := &CorrelatedPair{
		Request:  &AnnotatedMessage{Message: reqMsg, Direction: DirectionRequest},
		Response: &AnnotatedMessage{Message: respMsg, Direction: DirectionResponse},
		Label:    "Test Pair",
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:       "Mock Route Test Scenario",
		GenerateMockRoutes: true,
	}

	result, err := builder.Build([]*CorrelatedPair{pair}, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.MockRoutes, 1)
	mr := result.MockRoutes[0]
	assert.Equal(t, config.TypeMockRoute, mr.Type)
	assert.Equal(t, "0210", mr.ResponseMTI)
}

func TestScenarioBuilder_MockRoutesWithReversal(t *testing.T) {
	spec := utils.GetDefaultSpec()

	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0100")
	require.NoError(t, reqMsg.Field(3, "000000"))
	require.NoError(t, reqMsg.Field(11, "000001"))

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0110")
	require.NoError(t, respMsg.Field(3, "000000"))
	require.NoError(t, respMsg.Field(11, "000001"))
	require.NoError(t, respMsg.Field(38, "AUTH01"))
	require.NoError(t, respMsg.Field(39, "00"))

	pair := &CorrelatedPair{
		Request:  &AnnotatedMessage{Message: reqMsg, Direction: DirectionRequest},
		Response: &AnnotatedMessage{Message: respMsg, Direction: DirectionResponse},
		Label:    "Test Pair",
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:       "Mock Route Reversal Scenario",
		IncludeReversals:   map[int]bool{0: true},
		GenerateMockRoutes: true,
	}

	result, err := builder.Build([]*CorrelatedPair{pair}, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should generate 2 mock routes: Primary Request Response (0110) + Reversal Response (0410)
	require.Len(t, result.MockRoutes, 2)
	assert.Equal(t, "0110", result.MockRoutes[0].ResponseMTI)
	assert.Equal(t, "0410", result.MockRoutes[1].ResponseMTI)
	assert.Equal(t, "0400", result.MockRoutes[1].MatchFields["0"])
}

func TestScenarioBuilder_MockRoutes_DifferentCardsAndResponseCodes(t *testing.T) {
	spec := utils.GetDefaultSpec()

	cards := []string{"4000111122223333", "4000222233334444", "4000333344445555"}
	rcs := []string{"00", "51", "85"}

	var pairs []*CorrelatedPair
	for i := range cards {
		req := iso8583.NewMessage(spec)
		req.MTI("0100")
		require.NoError(t, req.Field(2, cards[i]))
		require.NoError(t, req.Field(3, "000000"))
		require.NoError(t, req.Field(11, fmt.Sprintf("%06d", i+1)))

		resp := iso8583.NewMessage(spec)
		resp.MTI("0110")
		require.NoError(t, resp.Field(2, cards[i]))
		require.NoError(t, resp.Field(3, "000000"))
		require.NoError(t, resp.Field(11, fmt.Sprintf("%06d", i+1)))
		require.NoError(t, resp.Field(39, rcs[i]))

		pairs = append(pairs, &CorrelatedPair{
			Request:  &AnnotatedMessage{Message: req, Direction: DirectionRequest},
			Response: &AnnotatedMessage{Message: resp, Direction: DirectionResponse},
			Label:    fmt.Sprintf("Pair %d", i+1),
		})
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:       "Multi-Card Scenario",
		GenerateMockRoutes: true,
		Unsecure:           true,
	}

	result, err := builder.Build(pairs, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// 3 distinct mock routes should be generated, each mapped to its card and response code
	require.Len(t, result.MockRoutes, 3)

	for i, mr := range result.MockRoutes {
		assert.Equal(t, config.TypeMockRoute, mr.Type)
		assert.Equal(t, "0110", mr.ResponseMTI)
		assert.Equal(t, cards[i], mr.MatchFields["2"])
		assert.Equal(t, rcs[i], mr.ResponseFields["39"])
	}
}

func TestScenarioBuilder_MockRoutes_GroupedCardsList(t *testing.T) {
	spec := utils.GetDefaultSpec()

	cards := []string{"4000111122223333", "4000222233334444", "4000333344445555", "4000444455556666"}
	rcs := []string{"00", "51", "51", "51"}

	var pairs []*CorrelatedPair
	for i := range cards {
		req := iso8583.NewMessage(spec)
		req.MTI("0100")
		require.NoError(t, req.Field(2, cards[i]))
		require.NoError(t, req.Field(3, "000000"))
		require.NoError(t, req.Field(11, fmt.Sprintf("%06d", i+1)))

		resp := iso8583.NewMessage(spec)
		resp.MTI("0110")
		require.NoError(t, resp.Field(2, cards[i]))
		require.NoError(t, resp.Field(3, "000000"))
		require.NoError(t, resp.Field(11, fmt.Sprintf("%06d", i+1)))
		require.NoError(t, resp.Field(39, rcs[i]))

		pairs = append(pairs, &CorrelatedPair{
			Request:  &AnnotatedMessage{Message: req, Direction: DirectionRequest},
			Response: &AnnotatedMessage{Message: resp, Direction: DirectionResponse},
			Label:    fmt.Sprintf("Pair %d", i+1),
		})
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:       "Grouped Card Scenario",
		GenerateMockRoutes: true,
		Unsecure:           true,
	}

	result, err := builder.Build(pairs, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should generate 2 grouped mock routes: 1 for RC=00 (Card 1) and 1 for RC=51 (Cards 2, 3, 4)
	require.Len(t, result.MockRoutes, 2)

	// Route 1 (RC=00)
	mr00 := result.MockRoutes[0]
	assert.Equal(t, "0110", mr00.ResponseMTI)
	assert.Equal(t, "00", mr00.ResponseFields["39"])
	assert.Equal(t, "4000111122223333", mr00.MatchFields["2"])

	// Route 2 (RC=51) with list of 3 cards
	mr51 := result.MockRoutes[1]
	assert.Equal(t, "0110", mr51.ResponseMTI)
	assert.Equal(t, "51", mr51.ResponseFields["39"])
	assert.Equal(t, []string{"4000222233334444", "4000333344445555", "4000444455556666"}, mr51.MatchFields["2"])
}

func TestScenarioBuilder_ExactEchoAndResponseFieldsSeparation(t *testing.T) {
	spec := utils.GetDefaultSpec()

	req := iso8583.NewMessage(spec)
	req.MTI("0100")
	require.NoError(t, req.Field(2, "4000123456789010"))
	require.NoError(t, req.Field(3, "000000"))
	require.NoError(t, req.Field(4, "15000"))
	require.NoError(t, req.Field(11, "123456"))
	require.NoError(t, req.Field(41, "TERM0001"))
	require.NoError(t, req.Field(49, "840"))

	resp := iso8583.NewMessage(spec)
	resp.MTI("0110")
	require.NoError(t, resp.Field(2, "4000123456789010")) // Matching -> should be ECHOED
	require.NoError(t, resp.Field(3, "000000"))           // Matching -> should be ECHOED
	require.NoError(t, resp.Field(4, "12000"))           // Differing value (e.g. partial approval) -> NOT ECHOED
	require.NoError(t, resp.Field(11, "123456"))         // Matching -> should be ECHOED
	require.NoError(t, resp.Field(38, "AUTH99"))         // Response only -> NOT ECHOED
	require.NoError(t, resp.Field(39, "00"))             // Response only -> NOT ECHOED
	require.NoError(t, resp.Field(41, "TERM0001"))       // Matching -> should be ECHOED
	require.NoError(t, resp.Field(49, "840"))            // Matching -> should be ECHOED

	pair := &CorrelatedPair{
		Request:  &AnnotatedMessage{Message: req, Direction: DirectionRequest},
		Response: &AnnotatedMessage{Message: resp, Direction: DirectionResponse},
		Label:    "Echo Separation Test",
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:       "Echo Separation Scenario",
		GenerateMockRoutes: true,
		Unsecure:           true,
	}

	result, err := builder.Build([]*CorrelatedPair{pair}, opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.MockRoutes, 1)

	mr := result.MockRoutes[0]
	// Echo fields must be exactly the matching fields: 2, 3, 11, 41, 49
	assert.Equal(t, []int{2, 3, 11, 41, 49}, mr.EchoFields)

	// Response fields MUST NOT contain 2, 3, 11, 41, 49
	assert.NotContains(t, mr.ResponseFields, "2")
	assert.NotContains(t, mr.ResponseFields, "3")
	assert.NotContains(t, mr.ResponseFields, "11")
	assert.NotContains(t, mr.ResponseFields, "41")
	assert.NotContains(t, mr.ResponseFields, "49")

	// Response fields MUST contain differing DE 4 ("12000"), DE 38 ("auth_code"), and DE 39 ("00")
	assert.Equal(t, "12000", mr.ResponseFields["4"])
	assert.Equal(t, "auth_code", mr.ResponseFields["38"])
	assert.Equal(t, "00", mr.ResponseFields["39"])
}

func TestScenarioBuilder_FullPCAPExecutionWithMockServer(t *testing.T) {
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

	var items []config.ConfigItem
	items = append(items, scaffold.Transactions...)
	items = append(items, scaffold.Datasets...)
	items = append(items, scaffold.Scenario)
	items = append(items, scaffold.MockRoutes...)

	tmpFile, err := os.CreateTemp("", "synthetic_pcap_scaffold_*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	data, err := json.MarshalIndent(items, "", "  ")
	require.NoError(t, err)
	_, err = tmpFile.Write(data)
	require.NoError(t, err)
	tmpFile.Close()

	// 4. Load into TransactionCollection
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

	// 5. Start Mock Server
	mockServer := server.NewServer(spec, mockRoutes, "binary2")
	require.NoError(t, mockServer.Start("19895"))
	defer func() {
		_ = mockServer.Stop()
	}()

	time.Sleep(50 * time.Millisecond)

	// 6. Connect client
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

	// 7. Run scenario
	runner := transactions.NewScenarioRunner(svc, tc)
	report, err := runner.RunScenario("Synthetic PCAP Scaffolded Scenario")
	require.NoError(t, err)
	require.NotNil(t, report)

	for idx, step := range report.Steps {
		t.Logf("Step %d: %s -> success=%v (err=%s, validation_errs=%v)", idx+1, step.StepName, step.Success, step.Error, step.ValidationErrors)
	}

	assert.True(t, report.Success, "All steps in synthetic PCAP scaffolded scenario must pass 100%%")
}




