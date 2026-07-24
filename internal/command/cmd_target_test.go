package command

import (
	"path/filepath"
	"testing"

	"jiso/internal/client"
	"jiso/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTargetCommand(t *testing.T) {
	clientCfg := client.NewClientConfig("localhost", "9999", nil)
	cmd := NewTargetCommand(clientCfg, nil)

	assert.Contains(t, cmd.GetStatus(), "localhost:9999")
	assert.Contains(t, cmd.GetStatus(), "OFFLINE")

	err := cmd.SetTarget("192.168.1.100:8000")
	require.NoError(t, err)

	host, port := clientCfg.GetTarget()
	assert.Equal(t, "192.168.1.100", host)
	assert.Equal(t, "8000", port)

	err = cmd.SetIP("10.0.0.1")
	require.NoError(t, err)
	host, port = clientCfg.GetTarget()
	assert.Equal(t, "10.0.0.1", host)
	assert.Equal(t, "8000", port)

	err = cmd.SetPort("9090")
	require.NoError(t, err)
	host, port = clientCfg.GetTarget()
	assert.Equal(t, "10.0.0.1", host)
	assert.Equal(t, "9090", port)
}

func TestTargetCommand_UpdatesServiceAddress(t *testing.T) {
	specPath := filepath.Join("..", "..", "specs", "spec_bcp.json")
	svc, err := service.NewService("localhost", "9999", specPath, false, 1, 0, 0, 0)
	require.NoError(t, err)

	clientCfg := client.NewClientConfig("localhost", "9999", nil)
	cmd := NewTargetCommand(clientCfg, svc)

	err = cmd.SetTarget("127.0.0.1:8888")
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:8888", svc.Address)
}
