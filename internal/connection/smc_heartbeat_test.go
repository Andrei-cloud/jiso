package connection

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"jiso/internal/utils"
)

func TestIsVisaHeader(t *testing.T) {
	vHdr, err := utils.NewVisaHeader("123456")
	assert.NoError(t, err)
	assert.True(t, IsVisaHeader(vHdr))

	binHdr, _ := utils.SelectLength("binary2")
	assert.False(t, IsVisaHeader(binHdr))

	assert.False(t, IsVisaHeader(nil))
}

func TestSMCHeartbeatDaemon_Lifecycle(t *testing.T) {
	mgr := NewManager("localhost", "9999", nil, false, 1, time.Second, time.Second, nil)
	daemon := NewSMCHeartbeatDaemon(mgr, 100*time.Millisecond)

	assert.False(t, daemon.IsRunning())

	daemon.Start()
	assert.True(t, daemon.IsRunning())

	// Idempotent start
	daemon.Start()
	assert.True(t, daemon.IsRunning())

	daemon.Stop()
	assert.False(t, daemon.IsRunning())

	// Idempotent stop
	daemon.Stop()
	assert.False(t, daemon.IsRunning())
}
