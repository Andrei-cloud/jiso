package server

import (
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/prefix"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

func TestServer_mTLS(t *testing.T) {
	certsDir := filepath.Join("..", "..", "testdata", "certs")
	configFile := filepath.Join(certsDir, "tls_config.json")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		t.Skip("testdata/certs/tls_config.json not found; skipping server mTLS test")
	}

	tlsCfg, err := config.LoadTLSConfig(configFile)
	require.NoError(t, err)

	serverTLS, err := tlsCfg.BuildServerTLSConfig()
	require.NoError(t, err)

	spec := utils.GetDefaultSpec()
	server := NewServer(spec, nil, "binary2")
	server.SetTLSConfig(serverTLS)

	require.NoError(t, server.Start("19894"))
	defer func() {
		_ = server.Stop()
	}()

	clientTLS, err := tlsCfg.BuildCryptoTLSConfig()
	require.NoError(t, err)

	// Dial with mTLS client config
	conn, err := tls.Dial("tcp", "127.0.0.1:19894", clientTLS)
	require.NoError(t, err)
	defer conn.Close()

	assert.Equal(t, 1, server.ActiveConnections())
}

func TestMockServerLifecycleAndMatching(t *testing.T) {
	spec := utils.GetDefaultSpec()

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

	require.NoError(t, server.Start("19890"))
	defer func() {
		_ = server.Stop()
	}()
	assert.True(t, server.IsRunning())
	assert.Equal(t, "19890", server.GetPort())

	// Connect to mock server via TCP socket
	conn, err := net.Dial("tcp", "localhost:19890")
	require.NoError(t, err)
	defer conn.Close()

	// Build 0800 Sign On request
	req := iso8583.NewMessage(spec)
	req.MTI("0800")
	require.NoError(t, req.Field(7, "0412232900"))
	require.NoError(t, req.Field(11, "000151"))
	require.NoError(t, req.Field(37, "251020000150"))
	require.NoError(t, req.Field(70, "1"))

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
	spec := utils.GetDefaultSpec()

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
	require.NoError(t, msg1.Field(70, "1"))
	matched1, resp1, err := matcher.MatchAndCompose(msg1, spec)
	require.NoError(t, err)
	require.NotNil(t, matched1)
	assert.Equal(t, "Network SignOn", matched1.Name)
	respMTI1, _ := resp1.GetMTI()
	assert.Equal(t, "0810", respMTI1)

	// Test case 2: Regex match on DE 25
	msg2 := iso8583.NewMessage(spec)
	msg2.MTI("0200")
	require.NoError(t, msg2.Field(25, "02"))
	matched2, resp2, err := matcher.MatchAndCompose(msg2, spec)
	require.NoError(t, err)
	require.NotNil(t, matched2)
	assert.Equal(t, "Regex Condition Code", matched2.Name)
	respMTI2, _ := resp2.GetMTI()
	assert.Equal(t, "0210", respMTI2)

	// Test case 3: Field existence check on DE 38
	msg3 := iso8583.NewMessage(spec)
	msg3.MTI("0400")
	require.NoError(t, msg3.Field(38, "AUTH12"))
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
	spec := utils.GetDefaultSpec()

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
	require.NoError(t, msgValid.Field(4, "1000"))
	require.NoError(t, msgValid.Field(11, "000001"))
	require.NoError(t, msgValid.Field(41, "77973588"))

	matched, resp, err := matcher.MatchAndCompose(msgValid, spec)
	require.NoError(t, err)
	require.NotNil(t, matched)
	val39Valid, _ := resp.GetField(39).String()
	assert.Equal(t, "00", val39Valid)

	// Subtest 2: Missing mandatory field 41 -> Format Error / Missing Field ("30")
	msgMissing := iso8583.NewMessage(spec)
	msgMissing.MTI("0200")
	require.NoError(t, msgMissing.Field(4, "1000"))
	require.NoError(t, msgMissing.Field(11, "000001"))
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
	defer func() {
		_ = srv.Stop()
	}()

	conn, err := net.Dial("tcp", "localhost:19891")
	require.NoError(t, err)
	defer conn.Close()

	req := iso8583.NewMessage(srv.spec)
	req.MTI("0800")
	require.NoError(t, req.Field(7, "0412232900"))
	require.NoError(t, req.Field(11, "000151"))
	require.NoError(t, req.Field(70, "1"))

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
	spec := utils.GetDefaultSpec()

	configData := `[
		{
			"type": "mock_route",
			"name": "Network SignOn Route",
			"match_fields": {
				"0": "0800",
				"70": "1"
			},
			"echo_fields": [7, 11, 70],
			"response_mti": "0810",
			"response_fields": {
				"39": "00"
			}
		},
		{
			"type": "mock_route",
			"name": "Financial Purchase Route",
			"match_fields": {
				"0": "0200",
				"3": "000000"
			},
			"echo_fields": [2, 3, 4, 7, 11, 14, 41, 49],
			"response_mti": "0210",
			"response_fields": {
				"38": "auth_code",
				"39": "00"
			}
		}
	]`

	tmpFile, err := os.CreateTemp(t.TempDir(), "mock_routes_*.json")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(configData)
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
	require.NoError(t, msg0800_1.Field(7, "0725213831"))
	require.NoError(t, msg0800_1.Field(11, "008008"))
	require.NoError(t, msg0800_1.Field(70, "1"))

	matched1, resp1, err := matcher.MatchAndCompose(msg0800_1, spec)
	require.NoError(t, err)
	require.NotNil(t, matched1)
	val39_1, _ := resp1.GetField(39).String()
	assert.Equal(t, "00", val39_1)

	// Test Financial 0200 matching & echoing card/track/fields
	msg0200 := iso8583.NewMessage(spec)
	msg0200.MTI("0200")
	require.NoError(t, msg0200.Field(2, "9876543210987654"))
	require.NoError(t, msg0200.Field(3, "000000"))
	require.NoError(t, msg0200.Field(4, "2500"))
	require.NoError(t, msg0200.Field(7, "0725213835"))
	require.NoError(t, msg0200.Field(11, "008009"))
	require.NoError(t, msg0200.Field(14, "2601"))
	require.NoError(t, msg0200.Field(41, "77973588"))
	require.NoError(t, msg0200.Field(49, "634"))

	matched2, resp2, err := matcher.MatchAndCompose(msg0200, spec)
	require.NoError(t, err)
	require.NotNil(t, matched2)
	val39_2, _ := resp2.GetField(39).String()
	assert.Equal(t, "00", val39_2)
	val2_2, _ := resp2.GetField(2).String()
	assert.Equal(t, "9876543210987654", val2_2, "Card PAN DE 2 should be echoed from request")
}

func TestMatchAndComposeWithCompositeFields(t *testing.T) {
	spec := utils.GetDefaultSpec()
	spec.Fields[55] = field.NewComposite(&field.Spec{
		Length:      255,
		Description: "EMV Data",
		Pref:        prefix.Binary.Fixed,
		Bitmap:      field.NewBitmap(&field.Spec{Length: 1, Description: "Bitmap", Enc: encoding.Binary, Pref: prefix.Binary.Fixed, DisableAutoExpand: true}),
		Subfields: map[string]field.Field{
			"1": field.NewString(&field.Spec{
				Length:      8,
				Description: "Application Cryptogram",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			"2": field.NewString(&field.Spec{
				Length:      1,
				Description: "Cryptogram Information Data",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
		},
	})

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
					"1": "11223344",
					"2": "8",
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

	sub1 := comp55.GetSubfields()["1"]
	require.NotNil(t, sub1)
	str1, err := sub1.String()
	require.NoError(t, err)
	assert.Equal(t, "11223344", str1)

	sub2 := comp55.GetSubfields()["2"]
	require.NotNil(t, sub2)
	str2, err := sub2.String()
	require.NoError(t, err)
	assert.Equal(t, "8", str2)
}

func TestMatcher_SpecificityOrder_CardRouteBeforeGenericRoute(t *testing.T) {
	spec := utils.GetDefaultSpec()

	// Generic route is listed FIRST, specific card route is listed SECOND
	routes := []config.MockRouteConfig{
		{
			Name: "Generic Purchase Route",
			MatchFields: map[string]interface{}{
				"0": "0100",
				"3": "000000",
			},
			ResponseMTI:    "0110",
			ResponseFields: map[string]interface{}{"39": "00"},
		},
		{
			Name: "Card-Specific Decline Route",
			MatchFields: map[string]interface{}{
				"0": "0100",
				"3": "000000",
				"2": "4000111122223333",
			},
			ResponseMTI:    "0110",
			ResponseFields: map[string]interface{}{"39": "51"},
		},
	}

	matcher := NewMatcher(routes)

	// 1. Send request with card 4000111122223333 -> should match Card-Specific Route and return "51"
	reqSpecific := iso8583.NewMessage(spec)
	reqSpecific.MTI("0100")
	require.NoError(t, reqSpecific.Field(2, "4000111122223333"))
	require.NoError(t, reqSpecific.Field(3, "000000"))

	matched, resp, err := matcher.MatchAndCompose(reqSpecific, spec)
	require.NoError(t, err)
	require.NotNil(t, matched)
	assert.Equal(t, "Card-Specific Decline Route", matched.Name)
	rc, err := resp.GetField(39).String()
	require.NoError(t, err)
	assert.Equal(t, "51", rc)

	// 2. Send request with different card 4000999999999999 -> should match Generic Route and return "00"
	reqGeneric := iso8583.NewMessage(spec)
	reqGeneric.MTI("0100")
	require.NoError(t, reqGeneric.Field(2, "4000999999999999"))
	require.NoError(t, reqGeneric.Field(3, "000000"))

	matchedGen, respGen, err := matcher.MatchAndCompose(reqGeneric, spec)
	require.NoError(t, err)
	require.NotNil(t, matchedGen)
	assert.Equal(t, "Generic Purchase Route", matchedGen.Name)
	rcGen, err := respGen.GetField(39).String()
	require.NoError(t, err)
	assert.Equal(t, "00", rcGen)
}

func TestMatcher_ListValueMatching(t *testing.T) {
	spec := utils.GetDefaultSpec()

	routes := []config.MockRouteConfig{
		{
			Name: "Card Pool A Route (Direct Slice)",
			MatchFields: map[string]interface{}{
				"0": "0100",
				"2": []interface{}{"4000111122223333", "4000222233334444"},
				"3": []string{"000000", "100000"},
			},
			ResponseMTI:    "0110",
			ResponseFields: map[string]interface{}{"39": "00"},
		},
		{
			Name: "Card Pool B Route (In Map Operator)",
			MatchFields: map[string]interface{}{
				"0": "0100",
				"2": map[string]interface{}{
					"in": []interface{}{"4000333344445555", "4000444455556666"},
				},
			},
			ResponseMTI:    "0110",
			ResponseFields: map[string]interface{}{"39": "51"},
		},
	}

	matcher := NewMatcher(routes)

	// Case 1: Card 1 from Pool A with DE3=000000 -> Should match Route 1 (RC: 00)
	req1 := iso8583.NewMessage(spec)
	req1.MTI("0100")
	require.NoError(t, req1.Field(2, "4000111122223333"))
	require.NoError(t, req1.Field(3, "000000"))

	m1, resp1, err := matcher.MatchAndCompose(req1, spec)
	require.NoError(t, err)
	require.NotNil(t, m1)
	assert.Equal(t, "Card Pool A Route (Direct Slice)", m1.Name)
	rc1, _ := resp1.GetField(39).String()
	assert.Equal(t, "00", rc1)

	// Case 2: Card 2 from Pool A with DE3=100000 -> Should match Route 1 (RC: 00)
	req2 := iso8583.NewMessage(spec)
	req2.MTI("0100")
	require.NoError(t, req2.Field(2, "4000222233334444"))
	require.NoError(t, req2.Field(3, "100000"))

	m2, resp2, err := matcher.MatchAndCompose(req2, spec)
	require.NoError(t, err)
	require.NotNil(t, m2)
	assert.Equal(t, "Card Pool A Route (Direct Slice)", m2.Name)
	rc2, _ := resp2.GetField(39).String()
	assert.Equal(t, "00", rc2)

	// Case 3: Card 1 from Pool B -> Should match Route 2 (RC: 51)
	req3 := iso8583.NewMessage(spec)
	req3.MTI("0100")
	require.NoError(t, req3.Field(2, "4000333344445555"))
	require.NoError(t, req3.Field(3, "000000"))

	m3, resp3, err := matcher.MatchAndCompose(req3, spec)
	require.NoError(t, err)
	require.NotNil(t, m3)
	assert.Equal(t, "Card Pool B Route (In Map Operator)", m3.Name)
	rc3, _ := resp3.GetField(39).String()
	assert.Equal(t, "51", rc3)

	// Case 4: Non-matching Card -> Fallback (RC: 12)
	req4 := iso8583.NewMessage(spec)
	req4.MTI("0100")
	require.NoError(t, req4.Field(2, "4000999999999999"))
	require.NoError(t, req4.Field(3, "000000"))

	m4, resp4, err := matcher.MatchAndCompose(req4, spec)
	require.NoError(t, err)
	assert.Nil(t, m4)
	rc4, _ := resp4.GetField(39).String()
	assert.Equal(t, "12", rc4)
}

func TestMatcher_NumericEquivalenceAndLeadingZeros(t *testing.T) {
	spec := utils.GetDefaultSpec()

	routes := []config.MockRouteConfig{
		{
			Name: "Route with Integer 0 ProcCode",
			MatchFields: map[string]interface{}{
				"0":  "0100",
				"3":  0,      // integer 0 in JSON
				"22": "0100", // exact 4-digit string
				"25": 59,     // integer 59 in JSON
			},
			EchoFields:     []int{2, 3, 22, 25},
			ResponseMTI:    "0110",
			ResponseFields: map[string]interface{}{"39": "00"},
		},
	}

	matcher := NewMatcher(routes)

	req := iso8583.NewMessage(spec)
	req.MTI("0100")
	require.NoError(t, req.Field(2, "4000123456789010"))
	require.NoError(t, req.Field(3, "000000")) // exact ISO 6-zero string
	require.NoError(t, req.Field(22, "0100"))
	require.NoError(t, req.Field(25, "59"))

	matched, resp, err := matcher.MatchAndCompose(req, spec)
	require.NoError(t, err)
	require.NotNil(t, matched)
	assert.Equal(t, "Route with Integer 0 ProcCode", matched.Name)

	rc, err := resp.GetField(39).String()
	require.NoError(t, err)
	assert.Equal(t, "00", rc)

	// Verify echo fields in response
	f3, _ := resp.GetField(3).String()
	assert.Equal(t, "000000", f3)
	f22, _ := resp.GetField(22).String()
	assert.Equal(t, "0100", f22)
	f25, _ := resp.GetField(25).String()
	assert.Equal(t, "59", f25)
}



