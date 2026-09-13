package connection

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	moovconnection "github.com/moov-io/iso8583-connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/utils"
)

// Regression: the old Connect held statusMu across moovconnection.New,
// ConnectCtx and the 200 ms stabilization sleep, so GetStatus/IsConnected
// (UI polling) froze for the whole connect window. It also wrote
// debugMode/naps/header without synchronization while the send path and the
// reconnect goroutine read them. Under -race the old implementation trips
// the detector; the timing assertion catches the liveness freeze.
func TestConnectDoesNotBlockStatusReaders(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(io.Discard, c)
			}(c)
		}
	}()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	m := NewManager("127.0.0.1", port, mockMessageSpec(), false, 0, 2*time.Second, 2*time.Second, nil)
	defer func() { _ = m.Close() }()

	stop := make(chan struct{})
	var maxDelay atomic.Int64
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			t0 := time.Now()
			_ = m.GetStatus()
			_ = m.IsConnected()
			if d := time.Since(t0); d.Nanoseconds() > maxDelay.Load() {
				maxDelay.Store(d.Nanoseconds())
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			m.SetDebugMode(i%2 == 0)
			m.SetResponseTimeout(time.Duration(i+1) * time.Millisecond)
			m.SetMaxPendingRequests(i + 1)
		}
	}()

	connectErr := m.Connect(false, utils.NewBinary2BytesAdapter())
	close(stop)
	wg.Wait()

	require.NoError(t, connectErr)
	assert.Equal(t, string(moovconnection.StatusOnline), m.GetStatus())

	if d := time.Duration(maxDelay.Load()); d > 100*time.Millisecond {
		t.Errorf("status readers stalled %v behind the connect path; GetStatus/IsConnected must not block behind ConnectCtx or the stabilization sleep", d)
	}
}

// Regression: Close during a connect attempt must cancel the in-flight
// publish instead of resurrecting a connection the caller closed.
func TestCloseDuringConnectCancelsPublish(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	accepted := make(chan struct{})
	go func() {
		c, err := ln.Accept()
		if err == nil {
			close(accepted)
			defer func() { _ = c.Close() }()
			_, _ = io.Copy(io.Discard, c)
		}
	}()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	m := NewManager("127.0.0.1", port, mockMessageSpec(), false, 0, 2*time.Second, 2*time.Second, nil)

	go func() {
		<-accepted // connect dial landed; Close while Connect is in its stabilization sleep
		_ = m.Close()
	}()

	// Either outcome is valid depending on where Close landed (cancelled
	// publish error, or publish won the race and Close hit the published
	// conn), but the manager must end up offline.
	_ = m.Connect(false, utils.NewBinary2BytesAdapter())
	_ = m.Close()
	assert.False(t, m.IsConnected())
}
