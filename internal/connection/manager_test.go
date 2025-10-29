package connection

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/moov-io/iso8583"
	moovconnection "github.com/moov-io/iso8583-connection"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/network"
	"github.com/moov-io/iso8583/prefix"
	"github.com/stretchr/testify/assert"
)

type Binary2BytesAdapter struct {
	binary2Bytes *network.Binary2Bytes
}

func (a *Binary2BytesAdapter) SetLength(length int) {
	a.binary2Bytes.SetLength(length)
}

func (a *Binary2BytesAdapter) Length() int {
	return a.binary2Bytes.Length()
}

func (a *Binary2BytesAdapter) WriteTo(w io.Writer) (int, error) {
	n, err := a.binary2Bytes.WriteTo(w)
	return n, err
}

func (a *Binary2BytesAdapter) ReadFrom(r io.Reader) (int, error) {
	n, err := a.binary2Bytes.ReadFrom(r)
	if err != nil {
		return 0, fmt.Errorf("reading from reader: %w", err)
	}

	return n, nil
}

// mockMessageSpec creates a basic ISO8583 message spec for testing
func mockMessageSpec() *iso8583.MessageSpec {
	spec := &iso8583.MessageSpec{
		Name: "Test Spec",
		Fields: map[int]field.Field{
			0: field.NewString(&field.Spec{
				Length:      4,
				Description: "Message Type Indicator",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			1: field.NewBitmap(&field.Spec{
				Length:      16,
				Description: "Bitmap",
				Enc:         encoding.Binary,
				Pref:        prefix.Binary.Fixed,
			}),
			2: field.NewString(&field.Spec{
				Length:      19,
				Description: "Primary Account Number",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.LL,
			}),
			11: field.NewString(&field.Spec{
				Length:      6,
				Description: "Systems Trace Audit Number",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			39: field.NewString(&field.Spec{
				Length:      2,
				Description: "Response Code",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
		},
	}
	return spec
}

func TestNewManager(t *testing.T) {
	spec := mockMessageSpec()
	manager := NewManager("localhost", "8080", spec, true, 3, 5*time.Second, 10*time.Second, nil)

	assert.NotNil(t, manager)
	assert.Equal(t, "localhost:8080", manager.GetAddress())
	assert.Equal(t, "Not initialized", manager.GetStatus())
	assert.False(t, manager.IsConnected())
}

func TestManagerConnectionStatus(t *testing.T) {
	spec := mockMessageSpec()
	manager := NewManager("localhost", "8080", spec, false, 3, 5*time.Second, 10*time.Second, nil)

	// Initial state should be not connected
	assert.False(t, manager.IsConnected())

	// A connection that hasn't been established should be able to close without error
	err := manager.Close()
	assert.NoError(t, err)
}

func TestManagerSendWithNoConnection(t *testing.T) {
	spec := mockMessageSpec()
	manager := NewManager("localhost", "8080", spec, false, 3, 5*time.Second, 10*time.Second, nil)

	// Create a test message
	message := iso8583.NewMessage(spec)
	err := message.Field(0, "0100")
	assert.NoError(t, err)

	// Attempting to send without a connection should fail
	_, err = manager.Send(message)
	assert.Error(t, err)
	assert.Equal(t, moovconnection.ErrConnectionClosed, err)

	// Same for background send
	_, err = manager.BackgroundSend(message)
	assert.Error(t, err)
	assert.Equal(t, moovconnection.ErrConnectionClosed, err)
}

type testServer struct {
	listener net.Listener
	spec     *iso8583.MessageSpec
	header   network.Header
	respond  bool
	done     chan struct{}
}

func startTestServer(spec *iso8583.MessageSpec, respond bool) (*testServer, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	server := &testServer{
		listener: listener,
		spec:     spec,
		header:   &Binary2BytesAdapter{network.NewBinary2BytesHeader()},
		respond:  respond,
		done:     make(chan struct{}),
	}

	go server.run()

	return server, nil
}

func (s *testServer) run() {
	for {
		select {
		case <-s.done:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			continue
		}

		go s.handle(conn)
	}
}

func (s *testServer) handle(conn net.Conn) {
	defer conn.Close()

	for {
		// Read length
		_, err := s.header.ReadFrom(conn)
		if err != nil {
			if err == io.EOF {
				return
			}
			continue
		}

		messageLength := s.header.Length()

		// Read message
		buf := make([]byte, messageLength)
		_, err = conn.Read(buf)
		if err != nil {
			return
		}

		if !s.respond {
			// For timeout test, don't respond but keep connection open
			continue
		}

		// Unpack
		msg := iso8583.NewMessage(s.spec)
		err = msg.Unpack(buf)
		if err != nil {
			continue
		}

		// Create response
		resp := iso8583.NewMessage(s.spec)
		resp.MTI("0810")
		if stan, err := msg.GetString(11); err == nil {
			resp.Field(11, stan)
		}
		resp.Field(39, "00")

		// Pack response
		respPacked, err := resp.Pack()
		if err != nil {
			continue
		}

		// Write length
		s.header.SetLength(len(respPacked))
		_, err = s.header.WriteTo(conn)
		if err != nil {
			return
		}

		// Write response
		_, err = conn.Write(respPacked)
		if err != nil {
			return
		}
	}
}

func (s *testServer) port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *testServer) Close() {
	close(s.done)
	s.listener.Close()
}

func TestSendAsyncTimeout(t *testing.T) {
	spec := mockMessageSpec()
	// Create a server that responds after a very long delay
	server, err := startDelayedTestServer(spec, 2*time.Second) // Much longer than test timeout
	assert.NoError(t, err)
	defer server.Close()

	manager := NewManager(
		"localhost",
		fmt.Sprintf("%d", server.port()),
		spec,
		true, // Enable debug mode
		3,
		5*time.Second,
		10*time.Second,
		nil,
	)
	manager.SetResponseTimeout(100 * time.Millisecond) // Short timeout for testing

	// Connect
	err = manager.Connect(false, &Binary2BytesAdapter{network.NewBinary2BytesHeader()})
	assert.NoError(t, err)
	defer manager.Close()

	// Create test message
	message := iso8583.NewMessage(spec)
	err = message.Field(0, "0800")
	assert.NoError(t, err)
	err = message.Field(11, "123456") // STAN
	assert.NoError(t, err)

	// Send async
	responseChan, err := manager.SendAsync(message, "test_transaction")
	assert.NoError(t, err)
	assert.NotNil(t, responseChan)

	// Wait for timeout (channel should be closed)
	select {
	case resp := <-responseChan:
		// Channel was closed due to timeout, so resp should be nil
		if resp != nil {
			t.Errorf("Expected nil response due to timeout, but got: %v", resp)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Expected channel to be closed due to timeout")
	}

	// Channel should be closed after timeout
	select {
	case resp, ok := <-responseChan:
		if ok {
			t.Errorf("Channel should be closed after timeout, but got: %v", resp)
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("Channel should be closed immediately after timeout")
	}
}

func TestSendAsyncLateResponse(t *testing.T) {
	spec := mockMessageSpec()
	// Create a server that responds after a delay
	server, err := startDelayedTestServer(spec, 200*time.Millisecond)
	assert.NoError(t, err)
	defer server.Close()

	manager := NewManager(
		"localhost",
		fmt.Sprintf("%d", server.port()),
		spec,
		false,
		3,
		5*time.Second,
		10*time.Second,
		nil,
	)
	manager.SetResponseTimeout(100 * time.Millisecond) // Short timeout

	// Connect
	err = manager.Connect(false, &Binary2BytesAdapter{network.NewBinary2BytesHeader()})
	assert.NoError(t, err)
	defer manager.Close()

	// Create test message
	message := iso8583.NewMessage(spec)
	err = message.Field(0, "0800")
	assert.NoError(t, err)
	err = message.Field(11, "123456") // STAN
	assert.NoError(t, err)

	// Send async
	responseChan, err := manager.SendAsync(message, "test_transaction")
	assert.NoError(t, err)
	assert.NotNil(t, responseChan)

	// Wait for timeout (channel should be closed)
	select {
	case resp := <-responseChan:
		// Channel was closed due to timeout, so resp should be nil
		if resp != nil {
			t.Errorf("Expected nil response due to timeout, but got: %v", resp)
		}
	case <-time.After(150 * time.Millisecond):
		t.Error("Expected channel to be closed due to timeout")
	}

	// Channel should be closed after timeout
	select {
	case resp, ok := <-responseChan:
		if ok {
			t.Errorf("Channel should be closed after timeout, but got: %v", resp)
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("Channel should be closed immediately after timeout")
	}

	// Wait a bit more to ensure the late response doesn't cause issues
	time.Sleep(100 * time.Millisecond)
}

func TestSendAsyncSuccessfulResponse(t *testing.T) {
	spec := mockMessageSpec()
	// Create a server that responds immediately
	server, err := startTestServer(spec, true)
	assert.NoError(t, err)
	defer server.Close()

	manager := NewManager(
		"localhost",
		fmt.Sprintf("%d", server.port()),
		spec,
		false,
		3,
		5*time.Second,
		10*time.Second,
		nil,
	)
	manager.SetResponseTimeout(1 * time.Second) // Longer timeout

	// Connect
	err = manager.Connect(false, &Binary2BytesAdapter{network.NewBinary2BytesHeader()})
	assert.NoError(t, err)
	defer manager.Close()

	// Create test message
	message := iso8583.NewMessage(spec)
	err = message.Field(0, "0800")
	assert.NoError(t, err)
	err = message.Field(11, "123456") // STAN
	assert.NoError(t, err)

	// Send async
	responseChan, err := manager.SendAsync(message, "test_transaction")
	assert.NoError(t, err)
	assert.NotNil(t, responseChan)

	// Wait for response
	select {
	case resp := <-responseChan:
		assert.NotNil(t, resp)
		// Verify STAN matches
		respStan, err := resp.GetString(11)
		assert.NoError(t, err)
		assert.Equal(t, "123456", respStan)
	case <-time.After(500 * time.Millisecond):
		t.Error("Expected response within timeout")
	}
}

func TestSendAsyncMultipleRequests(t *testing.T) {
	spec := mockMessageSpec()
	// Create a server that responds with delay
	server, err := startDelayedTestServer(spec, 50*time.Millisecond)
	assert.NoError(t, err)
	defer server.Close()

	manager := NewManager(
		"localhost",
		fmt.Sprintf("%d", server.port()),
		spec,
		false,
		3,
		5*time.Second,
		10*time.Second,
		nil,
	)
	manager.SetResponseTimeout(200 * time.Millisecond)

	// Connect
	err = manager.Connect(false, &Binary2BytesAdapter{network.NewBinary2BytesHeader()})
	assert.NoError(t, err)
	defer manager.Close()

	// Send multiple async requests
	const numRequests = 3
	responseChans := make([]<-chan *iso8583.Message, numRequests)

	for i := 0; i < numRequests; i++ {
		message := iso8583.NewMessage(spec)
		err = message.Field(0, "0800")
		assert.NoError(t, err)
		stan := fmt.Sprintf("%06d", 123456+i) // Different STAN for each
		err = message.Field(11, stan)
		assert.NoError(t, err)

		responseChan, err := manager.SendAsync(message, fmt.Sprintf("test_transaction_%d", i))
		assert.NoError(t, err)
		responseChans[i] = responseChan
	}

	// Wait for all responses
	for i, responseChan := range responseChans {
		select {
		case resp := <-responseChan:
			assert.NotNil(t, resp)
			// Verify STAN matches
			respStan, err := resp.GetString(11)
			assert.NoError(t, err)
			expectedStan := fmt.Sprintf("%06d", 123456+i)
			assert.Equal(t, expectedStan, respStan)
		case <-time.After(300 * time.Millisecond):
			t.Errorf("Expected response for request %d within timeout", i)
		}
	}
}

func TestSendAsyncSTANMismatch(t *testing.T) {
	spec := mockMessageSpec()
	// Create a server that responds with wrong STAN
	server, err := startMismatchTestServer(spec)
	assert.NoError(t, err)
	defer server.Close()

	manager := NewManager(
		"localhost",
		fmt.Sprintf("%d", server.port()),
		spec,
		true, // Enable debug mode
		3,
		5*time.Second,
		10*time.Second,
		nil,
	)
	manager.SetResponseTimeout(200 * time.Millisecond)

	// Connect
	err = manager.Connect(false, &Binary2BytesAdapter{network.NewBinary2BytesHeader()})
	assert.NoError(t, err)
	defer manager.Close()

	// Create test message
	message := iso8583.NewMessage(spec)
	err = message.Field(0, "0800")
	assert.NoError(t, err)
	err = message.Field(11, "123456") // STAN
	assert.NoError(t, err)

	// Send async
	responseChan, err := manager.SendAsync(message, "test_transaction")
	assert.NoError(t, err)
	assert.NotNil(t, responseChan)

	// Wait for timeout (since STAN won't match)
	select {
	case resp := <-responseChan:
		// For STAN mismatch, we expect no response to be delivered
		// The channel should eventually be closed by timeout
		if resp != nil {
			t.Errorf("Expected no response due to STAN mismatch, but got: %v", resp)
		}
		// Channel was closed (got nil), which is expected after timeout
	case <-time.After(250 * time.Millisecond):
		t.Error("Expected channel to be closed due to timeout")
	}
}

// startDelayedTestServer creates a test server that responds after a specified delay
func startDelayedTestServer(
	spec *iso8583.MessageSpec,
	delay time.Duration,
) (*delayedTestServer, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	server := &delayedTestServer{
		listener: listener,
		spec:     spec,
		header:   &Binary2BytesAdapter{network.NewBinary2BytesHeader()},
		delay:    delay,
		done:     make(chan struct{}),
	}

	go server.run()

	return server, nil
}

type delayedTestServer struct {
	listener net.Listener
	spec     *iso8583.MessageSpec
	header   network.Header
	delay    time.Duration
	done     chan struct{}
}

func (s *delayedTestServer) run() {
	for {
		select {
		case <-s.done:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			continue
		}

		go s.handle(conn)
	}
}

func (s *delayedTestServer) handle(conn net.Conn) {
	defer conn.Close()

	for {
		// Read length
		_, err := s.header.ReadFrom(conn)
		if err != nil {
			if err == io.EOF {
				return
			}
			continue
		}

		messageLength := s.header.Length()

		// Read message
		buf := make([]byte, messageLength)
		_, err = conn.Read(buf)
		if err != nil {
			return
		}

		// Delay before responding
		time.Sleep(s.delay)

		// Unpack
		msg := iso8583.NewMessage(s.spec)
		err = msg.Unpack(buf)
		if err != nil {
			continue
		}

		// Create response
		resp := iso8583.NewMessage(s.spec)
		resp.MTI("0810")
		if stan, err := msg.GetString(11); err == nil {
			resp.Field(11, stan)
		}
		resp.Field(39, "00")

		// Pack response
		respPacked, err := resp.Pack()
		if err != nil {
			continue
		}

		// Write length
		s.header.SetLength(len(respPacked))
		_, err = s.header.WriteTo(conn)
		if err != nil {
			return
		}

		// Write response
		_, err = conn.Write(respPacked)
		if err != nil {
			return
		}
	}
}

func (s *delayedTestServer) port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *delayedTestServer) Close() {
	close(s.done)
	s.listener.Close()
}

// startMismatchTestServer creates a test server that responds with wrong STAN
func startMismatchTestServer(spec *iso8583.MessageSpec) (*mismatchTestServer, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	server := &mismatchTestServer{
		listener: listener,
		spec:     spec,
		header:   &Binary2BytesAdapter{network.NewBinary2BytesHeader()},
		done:     make(chan struct{}),
	}

	go server.run()

	return server, nil
}

type mismatchTestServer struct {
	listener net.Listener
	spec     *iso8583.MessageSpec
	header   network.Header
	done     chan struct{}
}

func (s *mismatchTestServer) run() {
	for {
		select {
		case <-s.done:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			continue
		}

		go s.handle(conn)
	}
}

func (s *mismatchTestServer) handle(conn net.Conn) {
	defer conn.Close()

	for {
		// Read length
		_, err := s.header.ReadFrom(conn)
		if err != nil {
			if err == io.EOF {
				return
			}
			continue
		}

		messageLength := s.header.Length()

		// Read message
		buf := make([]byte, messageLength)
		_, err = conn.Read(buf)
		if err != nil {
			return
		}

		// Unpack
		msg := iso8583.NewMessage(s.spec)
		err = msg.Unpack(buf)
		if err != nil {
			continue
		}

		// Create response with WRONG STAN
		resp := iso8583.NewMessage(s.spec)
		resp.MTI("0810")
		resp.Field(11, "999999") // Wrong STAN
		resp.Field(39, "00")

		// Pack response
		respPacked, err := resp.Pack()
		if err != nil {
			continue
		}

		// Write length
		s.header.SetLength(len(respPacked))
		_, err = s.header.WriteTo(conn)
		if err != nil {
			return
		}

		// Write response
		_, err = conn.Write(respPacked)
		if err != nil {
			return
		}
	}
}

func (s *mismatchTestServer) port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *mismatchTestServer) Close() {
	close(s.done)
	s.listener.Close()
}
