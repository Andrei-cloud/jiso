package server

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"jiso/internal/config"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
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
			ResponseFields: map[string]string{"39": "00"},
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
			ResponseFields: map[string]string{"39": "00"},
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
			ResponseFields: map[string]string{"39": "00"},
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
			ResponseFields: map[string]string{"39": "00"},
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
