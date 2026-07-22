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
			Name:           "SignOn Approval",
			MatchMTI:       "0800",
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
