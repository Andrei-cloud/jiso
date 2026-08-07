package server

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockServerLifecycleAndMatching(t *testing.T) {
	spec, err := utils.CreateSpecFromFile("../../specs/spec_bcp.json")
	if err != nil || spec == nil {
		spec = utils.GetDefaultSpec()
	}

	routes := []config.MockRouteConfig{
		{
			Name: "SignOn Approval",
			MatchFields: map[string]interface{}{
				"0": "0800",
			},
			ResponseMTI:    "0810",
			EchoFields:     []int{7, 11, 37},
			ResponseFields: map[string]interface{}{"39": "00"},
			LatencyMs:      10,
			JitterMs:       5,
		},
	}

	server := NewServer(spec, routes, "binary2")
	require.False(t, server.IsRunning())

	err = server.Start("19890")
	require.NoError(t, err)
	defer server.Stop()
	assert.True(t, server.IsRunning())
	assert.Equal(t, "19890", server.GetPort())

	// Connect to mock server via TCP socket
	conn, err := net.Dial("tcp", "localhost:19890")
	require.NoError(t, err)
	defer conn.Close()

	// Build 0800 Sign On request
	req := iso8583.NewMessage(spec)
	req.MTI("0800")
	req.Field(7, "0412232900")
	req.Field(11, "000151")
	req.Field(37, "251020000150")
	req.Field(70, "1")

	reqPacked, err := req.Pack()
	require.NoError(t, err)

	buf := make([]byte, 2+len(reqPacked))
	binary.BigEndian.PutUint16(buf[0:2], uint16(len(reqPacked)))
	copy(buf[2:], reqPacked)

	start := time.Now()
	_, err = conn.Write(buf)
	require.NoError(t, err)

	// Read 2-byte response length
	var respLen uint16
	err = binary.Read(conn, binary.BigEndian, &respLen)
	require.NoError(t, err)
	assert.Greater(t, respLen, uint16(0))

	respBuf := make([]byte, respLen)
	_, err = io.ReadFull(conn, respBuf)
	require.NoError(t, err)

	elapsed := time.Since(start)
	// Latency (10ms) + Jitter (5ms) should take at least 5ms
	assert.GreaterOrEqual(t, elapsed, 5*time.Millisecond)

	respMsg := iso8583.NewMessage(spec)
	err = respMsg.Unpack(respBuf)
	require.NoError(t, err)

	respMTI, _ := respMsg.GetMTI()
	assert.Equal(t, "0810", respMTI)

	f39 := respMsg.GetField(39)
	require.NotNil(t, f39)
	val39, _ := f39.String()
	assert.Equal(t, "00", val39)

	// Check Server Statistics
	stats := server.GetStats()
	stats.PrintSummary("19890", "binary2", server.ActiveConnections())
}

func TestFlexibleMatcherRules(t *testing.T) {
	spec, err := utils.CreateSpecFromFile("../../specs/spec_bcp.json")
	require.NoError(t, err)
	require.NotNil(t, spec)

	routes := []config.MockRouteConfig{
		{
			Name: "Network SignOn",
			MatchFields: map[string]interface{}{
				"0":  "0800",
				"70": "1",
			},
			ResponseMTI:    "0810",
			ResponseFields: map[string]interface{}{"39": "00"},
		},
		{
			Name: "Regex Condition Code",
			MatchFields: map[string]interface{}{
				"0": "0200",
				"25": map[string]interface{}{
					"regex": "^0[1-9]$",
				},
			},
			ResponseMTI:    "0210",
			ResponseFields: map[string]interface{}{"39": "00"},
		},
		{
			Name: "Field Existence Check",
			MatchFields: map[string]interface{}{
				"0": "0400",
				"38": map[string]interface{}{
					"exists": true,
				},
			},
			ResponseMTI:    "0410",
			ResponseFields: map[string]interface{}{"39": "00"},
		},
	}

	matcher := NewMatcher(routes)

	// Test case 1: Matches SignOn route
	msg1 := iso8583.NewMessage(spec)
	msg1.MTI("0800")
	msg1.Field(70, "1")
	matched1, resp1, err := matcher.MatchAndCompose(msg1, spec)
	require.NoError(t, err)
	require.NotNil(t, matched1)
	assert.Equal(t, "Network SignOn", matched1.Name)
	respMTI1, _ := resp1.GetMTI()
	assert.Equal(t, "0810", respMTI1)

	// Test case 2: Regex match on DE 25
	msg2 := iso8583.NewMessage(spec)
	msg2.MTI("0200")
	msg2.Field(25, "02")
	matched2, resp2, err := matcher.MatchAndCompose(msg2, spec)
	require.NoError(t, err)
	require.NotNil(t, matched2)
	assert.Equal(t, "Regex Condition Code", matched2.Name)
	respMTI2, _ := resp2.GetMTI()
	assert.Equal(t, "0210", respMTI2)

	// Test case 3: Field existence check on DE 38
	msg3 := iso8583.NewMessage(spec)
	msg3.MTI("0400")
	msg3.Field(38, "AUTH12")
	matched3, resp3, err := matcher.MatchAndCompose(msg3, spec)
	require.NoError(t, err)
	require.NotNil(t, matched3)
	assert.Equal(t, "Field Existence Check", matched3.Name)
	respMTI3, _ := resp3.GetMTI()
	assert.Equal(t, "0410", respMTI3)

	// Test case 4: Fallback response when no route matches
	msg4 := iso8583.NewMessage(spec)
	msg4.MTI("0100")
	matched4, resp4, err := matcher.MatchAndCompose(msg4, spec)
	require.NoError(t, err)
	assert.Nil(t, matched4)
	require.NotNil(t, resp4)
	val39, _ := resp4.GetField(39).String()
	assert.Equal(t, "12", val39)
}

func TestRequiredFieldsMissingResponse30(t *testing.T) {
	spec, err := utils.CreateSpecFromFile("../../specs/spec_bcp.json")
	require.NoError(t, err)
	require.NotNil(t, spec)

	routes := []config.MockRouteConfig{
		{
			Name: "Financial Purchase",
			MatchFields: map[string]interface{}{
				"0": "0200",
			},
			RequiredFields: []string{"4", "11", "41"},
			ResponseMTI:    "0210",
			ResponseFields: map[string]interface{}{"38": "123456", "39": "00"},
		},
	}

	matcher := NewMatcher(routes)

	// Subtest 1: All required fields present -> Approved ("00")
	msgValid := iso8583.NewMessage(spec)
	msgValid.MTI("0200")
	msgValid.Field(4, "1000")
	msgValid.Field(11, "000001")
	msgValid.Field(41, "77973588")

	matched, resp, err := matcher.MatchAndCompose(msgValid, spec)
	require.NoError(t, err)
	require.NotNil(t, matched)
	val39Valid, _ := resp.GetField(39).String()
	assert.Equal(t, "00", val39Valid)

	// Subtest 2: Missing mandatory field 41 -> Format Error / Missing Field ("30")
	msgMissing := iso8583.NewMessage(spec)
	msgMissing.MTI("0200")
	msgMissing.Field(4, "1000")
	msgMissing.Field(11, "000001")
	// Field 41 omitted

	matchedMissing, respMissing, err := matcher.MatchAndCompose(msgMissing, spec)
	require.NoError(t, err)
	require.NotNil(t, matchedMissing)
	val39Missing, _ := respMissing.GetField(39).String()
	assert.Equal(t, "30", val39Missing)
}

func TestNilSpecServerFallback(t *testing.T) {
	srv := NewServer(nil, nil, "binary2")
	require.NotNil(t, srv)
	require.NotNil(t, srv.spec)

	err := srv.Start("19891")
	require.NoError(t, err)
	defer srv.Stop()

	conn, err := net.Dial("tcp", "localhost:19891")
	require.NoError(t, err)
	defer conn.Close()

	req := iso8583.NewMessage(srv.spec)
	req.MTI("0800")
	req.Field(7, "0412232900")
	req.Field(11, "000151")
	req.Field(70, "1")

	reqPacked, err := req.Pack()
	require.NoError(t, err)

	buf := make([]byte, 2+len(reqPacked))
	binary.BigEndian.PutUint16(buf[0:2], uint16(len(reqPacked)))
	copy(buf[2:], reqPacked)

	_, err = conn.Write(buf)
	require.NoError(t, err)

	var respLen uint16
	err = binary.Read(conn, binary.BigEndian, &respLen)
	require.NoError(t, err)
	assert.Greater(t, respLen, uint16(0))
}

func TestMockRoutesCollectionLoadingAndMatching(t *testing.T) {
	spec, err := utils.CreateSpecFromFile("../../specs/spec.json")
	require.NoError(t, err)

	dataBytes, err := os.ReadFile("../../transactions/mock_routes.json")
	require.NoError(t, err)

	tmpFile, err := os.CreateTemp(t.TempDir(), "mock_routes_*.json")
	require.NoError(t, err)
	_, err = tmpFile.Write(dataBytes)
	require.NoError(t, err)
	tmpFile.Close()

	tcLoaded, err := transactions.NewTransactionCollection(tmpFile.Name(), spec)
	require.NoError(t, err)
	require.NotNil(t, tcLoaded)

	routes := tcLoaded.GetMockRoutes()
	require.NotEmpty(t, routes)

	matcher := NewMatcher(routes)

	// Test Network 0800 F70=1
	msg0800_1 := iso8583.NewMessage(spec)
	msg0800_1.MTI("0800")
	msg0800_1.Field(7, "0725213831")
	msg0800_1.Field(11, "008008")
	msg0800_1.Field(70, "1")

	matched1, resp1, err := matcher.MatchAndCompose(msg0800_1, spec)
	require.NoError(t, err)
	require.NotNil(t, matched1)
	val39_1, _ := resp1.GetField(39).String()
	assert.Equal(t, "00", val39_1)

	// Test Financial 0200 matching & echoing card/track/fields
	msg0200 := iso8583.NewMessage(spec)
	msg0200.MTI("0200")
	msg0200.Field(2, "9876543210987654")
	msg0200.Field(3, "000000")
	msg0200.Field(4, "2500")
	msg0200.Field(7, "0725213835")
	msg0200.Field(11, "008009")
	msg0200.Field(14, "2601")

	matched2, resp2, err := matcher.MatchAndCompose(msg0200, spec)
	require.NoError(t, err)
	require.NotNil(t, matched2)
	val39_2, _ := resp2.GetField(39).String()
	assert.Equal(t, "00", val39_2)
	val2_2, _ := resp2.GetField(2).String()
	assert.Equal(t, "9876543210987654", val2_2, "Card PAN DE 2 should be echoed from request")
}

func TestMatchAndComposeWithCompositeFields(t *testing.T) {
	spec, err := utils.CreateSpecFromFile("../../specs/example_composed_emv.json")
	require.NoError(t, err)

	routes := []config.MockRouteConfig{
		{
			Name: "Composite EMV Route",
			MatchFields: map[string]interface{}{
				"0": "0200",
			},
			ResponseMTI: "0210",
			ResponseFields: map[string]interface{}{
				"39": "00",
				"55": map[string]interface{}{
					"9F26": "11223344",
					"9F27": "8",
				},
			},
		},
	}

	matcher := NewMatcher(routes)
	msg := iso8583.NewMessage(spec)
	msg.MTI("0200")

	matched, resp, err := matcher.MatchAndCompose(msg, spec)
	require.NoError(t, err)
	require.NotNil(t, matched)

	val39, _ := resp.GetField(39).String()
	assert.Equal(t, "00", val39)

	f55 := resp.GetField(55)
	require.NotNil(t, f55)
	comp55, ok := f55.(*field.Composite)
	require.True(t, ok)

	sub9f26 := comp55.GetSubfields()["9F26"]
	require.NotNil(t, sub9f26)
	str9f26, err := sub9f26.String()
	require.NoError(t, err)
	assert.Equal(t, "11223344", str9f26)

	sub9f27 := comp55.GetSubfields()["9F27"]
	require.NotNil(t, sub9f27)
	str9f27, err := sub9f27.String()
	require.NoError(t, err)
	assert.Equal(t, "8", str9f27)
}

