package connection

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/utils"
)

func getFreePort(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
	_ = l.Close()
	return port
}

func TestListenAcceptsConnection(t *testing.T) {
	spec := mockMessageSpec()
	mgr := NewManager("localhost", "0", spec, true, 1, 1*time.Second, 2*time.Second, nil)
	mgr.SetListenTimeout(2 * time.Second)

	port := getFreePort(t)
	header, err := utils.SelectLength("binary2")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- mgr.Listen(port, false, header)
	}()

	// Wait briefly then dial as remote host
	time.Sleep(100 * time.Millisecond)
	conn, err := net.Dial("tcp", "127.0.0.1:"+port)
	require.NoError(t, err)
	defer conn.Close()

	err = <-errCh
	assert.NoError(t, err)
	assert.True(t, mgr.IsConnected())
	assert.True(t, mgr.IsListening())

	_ = mgr.Close()
	assert.False(t, mgr.IsConnected())
	assert.False(t, mgr.IsListening())
}

func TestListenTimeout(t *testing.T) {
	spec := mockMessageSpec()
	mgr := NewManager("localhost", "0", spec, false, 1, 1*time.Second, 2*time.Second, nil)
	mgr.SetListenTimeout(100 * time.Millisecond)

	port := getFreePort(t)
	header, err := utils.SelectLength("binary2")
	require.NoError(t, err)

	startTime := time.Now()
	err = mgr.Listen(port, false, header)
	duration := time.Since(startTime)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.GreaterOrEqual(t, duration, 90*time.Millisecond)
	assert.False(t, mgr.IsConnected())
}

func TestListenCloseCancels(t *testing.T) {
	spec := mockMessageSpec()
	mgr := NewManager("localhost", "0", spec, false, 1, 1*time.Second, 2*time.Second, nil)
	mgr.SetListenTimeout(5 * time.Second)

	port := getFreePort(t)
	header, err := utils.SelectLength("binary2")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- mgr.Listen(port, false, header)
	}()

	time.Sleep(100 * time.Millisecond)
	assert.True(t, mgr.IsListening())

	// Close listener while waiting
	err = mgr.Close()
	assert.NoError(t, err)

	listenErr := <-errCh
	assert.Error(t, listenErr)
	assert.False(t, mgr.IsListening())
	assert.False(t, mgr.IsConnected())
}

func TestListenSendReceive(t *testing.T) {
	spec := mockMessageSpec()
	mgr := NewManager("localhost", "0", spec, true, 1, 1*time.Second, 2*time.Second, nil)
	mgr.SetListenTimeout(2 * time.Second)

	port := getFreePort(t)
	header, err := utils.SelectLength("binary2")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- mgr.Listen(port, false, header)
	}()

	time.Sleep(100 * time.Millisecond)
	remoteConn, err := net.Dial("tcp", "127.0.0.1:"+port)
	require.NoError(t, err)
	defer remoteConn.Close()

	err = <-errCh
	require.NoError(t, err)
	require.True(t, mgr.IsConnected())

	// Read loop on remote side: echo back response with MTI 0810 and matching STAN 123456
	go func() {
		remoteHeader, err := utils.SelectLength("binary2")
		if err != nil {
			return
		}
		_, err = remoteHeader.ReadFrom(remoteConn)
		if err != nil {
			return
		}
		payload := make([]byte, remoteHeader.Length())
		_, err = io.ReadFull(remoteConn, payload)
		if err != nil {
			return
		}

		req := iso8583.NewMessage(spec)
		if err := req.Unpack(payload); err != nil {
			return
		}

		resp := iso8583.NewMessage(spec)
		resp.MTI("0810")
		_ = resp.Field(11, "123456")
		_ = resp.Field(39, "00")
		respPacked, _ := resp.Pack()

		respHeader, err := utils.SelectLength("binary2")
		if err != nil {
			return
		}
		respHeader.SetLength(len(respPacked))
		_, _ = respHeader.WriteTo(remoteConn)
		_, _ = remoteConn.Write(respPacked)
	}()

	// Send message from jiso manager
	msg := iso8583.NewMessage(spec)
	msg.MTI("0800")
	_ = msg.Field(11, "123456")

	reply, err := mgr.Send(msg)
	require.NoError(t, err)
	require.NotNil(t, reply)

	mti, _ := reply.GetMTI()
	assert.Equal(t, "0810", mti)

	f39 := reply.GetField(39)
	require.NotNil(t, f39)
	f39Val, _ := f39.String()
	assert.Equal(t, "00", f39Val)

	_ = mgr.Close()
}
