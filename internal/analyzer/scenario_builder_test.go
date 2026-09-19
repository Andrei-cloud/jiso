package analyzer

import (
	"fmt"
	"testing"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

func TestScenarioBuilder_BasicScaffold(t *testing.T) {
	t.Parallel()

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

	var txFields map[string]any
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

	assert.Equal(t, tx.Name, steps[0].UseTransactionID)
	require.Len(t, steps[0].Validate, 1)
	assert.Equal(t, "39", steps[0].Validate[0].Field)
	assert.Equal(t, "00", steps[0].Validate[0].Expect)
}

func TestScenarioBuilder_ReversalStep(t *testing.T) {
	t.Parallel()

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

	revReqMsg := iso8583.NewMessage(spec)
	revReqMsg.MTI("0400")
	require.NoError(t, revReqMsg.Field(2, "4111111111111111"))
	require.NoError(t, revReqMsg.Field(3, "000000"))
	require.NoError(t, revReqMsg.Field(11, "000002"))
	require.NoError(t, revReqMsg.Field(38, "AUTH01"))
	require.NoError(t, revReqMsg.Field(90, "02000000010000000000000000000000"))

	revRespMsg := iso8583.NewMessage(spec)
	revRespMsg.MTI("0410")
	require.NoError(t, revRespMsg.Field(3, "000000"))
	require.NoError(t, revRespMsg.Field(11, "000002"))
	require.NoError(t, revRespMsg.Field(39, "00"))

	pair := &CorrelatedPair{
		Request:      &AnnotatedMessage{Message: reqMsg, Direction: DirectionRequest},
		Response:     &AnnotatedMessage{Message: respMsg, Direction: DirectionResponse},
		Reversal:     &AnnotatedMessage{Message: revReqMsg, Direction: DirectionRequest},
		ReversalResp: &AnnotatedMessage{Message: revRespMsg, Direction: DirectionResponse},
		Label:        "Test Reversal Pair",
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:     "Test Reversal Scenario",
		IncludeReversals: map[int]bool{0: true},
	}

	result, err := builder.Build([]*CorrelatedPair{pair}, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should have 2 transactions: Original + Reversal
	require.Len(t, result.Transactions, 2)
	assert.Equal(t, "Tx 0200 DE3=000000 #1", result.Transactions[0].Name)
	assert.Equal(t, "Reversal for 0200 DE3=000000 #1", result.Transactions[1].Name)

	var revFields map[string]any
	err = json.Unmarshal(result.Transactions[1].Fields, &revFields)
	require.NoError(t, err)

	// Check dynamic placeholders in reversal
	assert.Equal(t, "{{context.AuthId}}", revFields["38"])
	assert.Equal(t, "{{context.OrigMTI}}{{context.OrigSTAN}}{{context.OrigDateTime}}0000000000000000000000", revFields["90"])

	// Check scenario steps: 2 steps (Request + Reversal)
	var steps []transactions.ScenarioStep
	err = json.Unmarshal(result.Scenario.Steps, &steps)
	require.NoError(t, err)
	require.Len(t, steps, 2)

	assert.Equal(t, "38", steps[0].Extract["AuthId"])
	assert.Equal(t, "0", steps[0].Extract["OrigMTI"])
	assert.Equal(t, "11", steps[0].Extract["OrigSTAN"])
}

func TestScenarioBuilder_MockRoutes(t *testing.T) {
	t.Parallel()

	spec := utils.GetDefaultSpec()

	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0100")
	require.NoError(t, reqMsg.Field(2, "4000123456789010"))
	require.NoError(t, reqMsg.Field(3, "000000"))
	require.NoError(t, reqMsg.Field(4, "5000"))
	require.NoError(t, reqMsg.Field(11, "123456"))

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0110")
	require.NoError(t, respMsg.Field(2, "4000123456789010"))
	require.NoError(t, respMsg.Field(3, "000000"))
	require.NoError(t, respMsg.Field(4, "5000"))
	require.NoError(t, respMsg.Field(11, "123456"))
	require.NoError(t, respMsg.Field(38, "AUTH99"))
	require.NoError(t, respMsg.Field(39, "00"))

	pair := &CorrelatedPair{
		Request:  &AnnotatedMessage{Message: reqMsg, Direction: DirectionRequest},
		Response: &AnnotatedMessage{Message: respMsg, Direction: DirectionResponse},
		Label:    "Auth Pair",
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:       "Mock Route Scenario",
		GenerateMockRoutes: true,
	}

	result, err := builder.Build([]*CorrelatedPair{pair}, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.MockRoutes, 1)
	mr := result.MockRoutes[0]
	assert.Equal(t, config.TypeMockRoute, mr.Type)
	assert.Equal(t, "0110", mr.ResponseMTI)
	assert.Equal(t, "0100", mr.MatchFields["0"])
	assert.Equal(t, "000000", mr.MatchFields["3"])

	// Response fields: 38 should map to dynamic keyword "auth_code", 39 should be "00"
	assert.Equal(t, "auth_code", mr.ResponseFields["38"])
	assert.Equal(t, "00", mr.ResponseFields["39"])

	// Echo fields: 2, 3, 4, 11
	assert.Contains(t, mr.EchoFields, 2)
	assert.Contains(t, mr.EchoFields, 3)
	assert.Contains(t, mr.EchoFields, 4)
	assert.Contains(t, mr.EchoFields, 11)
}

func TestScenarioBuilder_MockRoutesWithReversal(t *testing.T) {
	t.Parallel()

	spec := utils.GetDefaultSpec()

	reqMsg := iso8583.NewMessage(spec)
	reqMsg.MTI("0100")
	require.NoError(t, reqMsg.Field(2, "4000123456789010"))
	require.NoError(t, reqMsg.Field(3, "000000"))
	require.NoError(t, reqMsg.Field(11, "123456"))

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0110")
	require.NoError(t, respMsg.Field(2, "4000123456789010"))
	require.NoError(t, respMsg.Field(3, "000000"))
	require.NoError(t, respMsg.Field(11, "123456"))
	require.NoError(t, respMsg.Field(38, "AUTH99"))
	require.NoError(t, respMsg.Field(39, "00"))

	pair := &CorrelatedPair{
		Request:  &AnnotatedMessage{Message: reqMsg, Direction: DirectionRequest},
		Response: &AnnotatedMessage{Message: respMsg, Direction: DirectionResponse},
		Label:    "Auth Pair with Reversal",
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
	t.Parallel()

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

	// F12.3: three distinct response behaviours still become three routes,
	// but NO route matches on a card - and because they now share one
	// request match, the sharing must be named and ordered frequency-first
	// with the approved code winning the ties (the server answers with the
	// first full match).
	require.Len(t, result.MockRoutes, 3)

	seenRC := make(map[string]bool)
	for _, mr := range result.MockRoutes {
		assert.Equal(t, config.TypeMockRoute, mr.Type)
		assert.Equal(t, "0110", mr.ResponseMTI)
		_, hasPAN := mr.MatchFields["2"]
		assert.False(t, hasPAN, "scaffold routes must not match on card: %v", mr.MatchFields)
		rc, _ := mr.ResponseFields["39"].(string)
		assert.NotEmpty(t, rc)
		seenRC[rc] = true
	}
	for _, rc := range rcs {
		assert.True(t, seenRC[rc], "response code %s lost", rc)
	}
	assert.Equal(t, "00", result.MockRoutes[0].ResponseFields["39"], "RC-first order")
	assert.NotEmpty(t, result.Warnings, "shared-match sharing must be named")
}

func TestScenarioBuilder_MockRoutes_GroupedCardsList(t *testing.T) {
	t.Parallel()

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

	// F12.3: still two behaviour groups (RC 00, RC 51), but the cards are
	// gone from the match - the routes now share one request match, so the
	// sharing is named and the answer is frequency-first: the 51 the
	// capture showed three times leads the 00 it showed once (a mock
	// replays the network's dominant behaviour, not a forced approval).
	require.Len(t, result.MockRoutes, 2)

	mr51 := result.MockRoutes[0]
	assert.Equal(t, "0110", mr51.ResponseMTI)
	assert.Equal(t, "51", mr51.ResponseFields["39"])
	assert.NotContains(t, mr51.MatchFields, "2")

	mr00 := result.MockRoutes[1]
	assert.Equal(t, "0110", mr00.ResponseMTI)
	assert.Equal(t, "00", mr00.ResponseFields["39"])
	assert.NotContains(t, mr00.MatchFields, "2")

	assert.NotEmpty(t, result.Warnings, "shared-match sharing must be named")
}

// TestScenarioBuilder_RouteOrderFollowsAnswerFrequency: five distinct
// response SIGNATURES (each carrying a unique per-file batch detail in
// DE48) share one request match, and the capture's ANSWER was 06 three
// times against 00 twice. The dominant answer must lead the shared
// match - signature rarity must not hand the win to the rare approval
// (the UAT echo-back finding: 83 declines lost to 2 approvals because
// every decline signature was unique).
func TestScenarioBuilder_RouteOrderFollowsAnswerFrequency(t *testing.T) {
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
		mkPair("00", "1"), mkPair("06", "2"), mkPair("06", "3"), mkPair("06", "4"), mkPair("00", "5"),
	}

	result, err := NewScenarioBuilder(spec).Build(pairs, ScenarioScaffoldOptions{
		ScenarioName:       "Answer Frequency",
		GenerateMockRoutes: true,
		Unsecure:           true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.MockRoutes)
	require.Equal(t, "06", result.MockRoutes[0].ResponseFields["39"],
		"the most-seen answer leads the shared match")
}

func TestScenarioBuilder_ExactEchoAndResponseFieldsSeparation(t *testing.T) {
	t.Parallel()

	spec := utils.GetDefaultSpec()

	req := iso8583.NewMessage(spec)
	req.MTI("0100")
	require.NoError(t, req.Field(2, "4000123456789010"))
	require.NoError(t, req.Field(3, "000000"))
	require.NoError(t, req.Field(4, "15000"))
	require.NoError(t, req.Field(11, "123456"))
	require.NoError(t, req.Field(14, "2812"))
	require.NoError(t, req.Field(22, "012"))
	require.NoError(t, req.Field(25, "00"))
	require.NoError(t, req.Field(32, "123456"))
	require.NoError(t, req.Field(37, "123456789012"))
	require.NoError(t, req.Field(41, "TERM0001"))
	require.NoError(t, req.Field(42, "MERCHANT0000001"))
	require.NoError(t, req.Field(49, "840"))

	resp := iso8583.NewMessage(spec)
	resp.MTI("0110")
	require.NoError(t, resp.Field(2, "4000123456789010"))
	require.NoError(t, resp.Field(3, "000000"))
	require.NoError(t, resp.Field(4, "15000"))
	require.NoError(t, resp.Field(11, "123456"))
	require.NoError(t, resp.Field(14, "2812"))
	require.NoError(t, resp.Field(22, "012"))
	require.NoError(t, resp.Field(25, "00"))
	require.NoError(t, resp.Field(32, "123456"))
	require.NoError(t, resp.Field(37, "123456789012"))
	require.NoError(t, resp.Field(38, "AUTH01"))
	require.NoError(t, resp.Field(39, "00"))
	require.NoError(t, resp.Field(41, "TERM0001"))
	require.NoError(t, resp.Field(42, "MERCHANT0000001"))
	require.NoError(t, resp.Field(44, "EXTRA_DATA"))
	require.NoError(t, resp.Field(49, "840"))

	pair := &CorrelatedPair{
		Request:  &AnnotatedMessage{Message: req, Direction: DirectionRequest},
		Response: &AnnotatedMessage{Message: resp, Direction: DirectionResponse},
		Label:    "Echo vs Response Test",
	}

	builder := NewScenarioBuilder(spec)
	opts := ScenarioScaffoldOptions{
		ScenarioName:       "Separation Scenario",
		GenerateMockRoutes: true,
		Unsecure:           true,
	}

	result, err := builder.Build([]*CorrelatedPair{pair}, opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.MockRoutes, 1)

	mr := result.MockRoutes[0]

	// Echo fields must contain all identical request-response fields: 2, 3, 4, 11, 14, 22, 25, 32, 37, 41, 42, 49
	expectedEcho := []int{2, 3, 4, 11, 14, 22, 25, 32, 37, 41, 42, 49}
	for _, f := range expectedEcho {
		assert.Contains(t, mr.EchoFields, f, "Field %d must be in EchoFields", f)
	}

	// EchoFields must NOT contain response-only fields (38, 39, 44)
	assert.NotContains(t, mr.EchoFields, 38)
	assert.NotContains(t, mr.EchoFields, 39)
	assert.NotContains(t, mr.EchoFields, 44)

	// ResponseFields must contain 38 (auth_code), 39 (00), 44 (EXTRA_DATA)
	assert.Equal(t, "auth_code", mr.ResponseFields["38"])
	assert.Equal(t, "00", mr.ResponseFields["39"])
	assert.Equal(t, "EXTRA_DATA", mr.ResponseFields["44"])

	// ResponseFields must NOT contain echoed fields (like 4, 11, 41, 42)
	assert.Nil(t, mr.ResponseFields["4"])
	assert.Nil(t, mr.ResponseFields["11"])
	assert.Nil(t, mr.ResponseFields["41"])
	assert.Nil(t, mr.ResponseFields["42"])
}
