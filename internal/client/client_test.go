package client

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockReconnector struct {
	lastHost string
	lastPort string
	err      error
}

func (m *mockReconnector) ReconnectWithTarget(host, port string) error {
	m.lastHost = host
	m.lastPort = port
	return m.err
}

func TestClientConfig_ThreadSafetyAndTargetSwapping(t *testing.T) {
	mock := &mockReconnector{}
	cfg := NewClientConfig("localhost", "9999", mock)

	host, port := cfg.GetTarget()
	assert.Equal(t, "localhost", host)
	assert.Equal(t, "9999", port)
	assert.Equal(t, "localhost:9999", cfg.GetAddress())

	err := cfg.SetTarget("127.0.0.1:8080")
	require.NoError(t, err)

	host, port = cfg.GetTarget()
	assert.Equal(t, "127.0.0.1", host)
	assert.Equal(t, "8080", port)
	assert.Equal(t, "127.0.0.1", mock.lastHost)
	assert.Equal(t, "8080", mock.lastPort)

	// Error handling when reconnector fails
	mock.err = errors.New("connection failed")
	err = cfg.SetTarget("192.168.1.1:9090")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reconnect failed")
}

func TestParseTargetAddress(t *testing.T) {
	tests := []struct {
		input    string
		wantHost string
		wantPort string
		wantErr  bool
	}{
		{"localhost:9999", "localhost", "9999", false},
		{"127.0.0.1:8080", "127.0.0.1", "8080", false},
		{"8000", "localhost", "8000", false},
		{"invalid-port:abc", "", "", true},
		{"", "", "", true},
	}

	for _, tt := range tests {
		h, p, err := parseTargetAddress(tt.input)
		if tt.wantErr {
			assert.Error(t, err, "input: %s", tt.input)
		} else {
			require.NoError(t, err, "input: %s", tt.input)
			assert.Equal(t, tt.wantHost, h)
			assert.Equal(t, tt.wantPort, p)
		}
	}
}
