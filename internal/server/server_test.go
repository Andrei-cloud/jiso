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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/config"
	"jiso/internal/utils"
)

func TestServer_mTLS(t *testing.T) {
	t.Parallel()

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
	defer func() { _ = conn.Close() }()

	assert.Equal(t, 1, server.ActiveConnections())
}

func TestMockServerLifecycleAndMatching(t *testing.T) {
	t.Parallel()

	spec := utils.GetDefaultSpec()

	routes := []config.MockRouteConfig{
		{
			Name: "SignOn Approval",
			MatchFields: map[string]any{
				"0": "0800",
			},
			ResponseMTI:    "0810",
			EchoFields:     []int{7, 11, 37},
			ResponseFields: map[string]any{"39": "00"},
			LatencyMs:      10,
			JitterMs:       5,
		},
	}

	server := NewServer(spec, routes, "binary2")
	require.False(t, server.IsRunning())

	// Port 0 asks the kernel for a free port. A fixed number here makes the test
	// depend on nothing else on the machine holding that port, and it stops the
	// package from ever running in parallel with itself.
	require.NoError(t, server.Start("0"))
	defer func() {
		_ = server.Stop()
	}()
	assert.True(t, server.IsRunning())
	port := server.GetPort()
	require.NotEqual(t, "0", port, "Start should report the port it bound")

	// Connect to mock server via TCP socket
	conn, err := net.Dial("tcp", "localhost:"+port)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

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
	stats.PrintSummary(port, "binary2", server.ActiveConnections())
}

func TestNilSpecServerFallback(t *testing.T) {
	t.Parallel()

	srv := NewServer(nil, nil, "binary2")
	require.NotNil(t, srv)
	require.NotNil(t, srv.spec)

	// Port 0, for the same reason as the other server test: a fixed port makes
	// this test depend on the machine it runs on.
	err := srv.Start("0")
	require.NoError(t, err)
	defer func() {
		_ = srv.Stop()
	}()

	conn, err := net.Dial("tcp", "localhost:"+srv.GetPort())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

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
