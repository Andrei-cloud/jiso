package connection

import (
	"io"
	"net"
	"testing"
	"time"

	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	moovconnection "github.com/moov-io/iso8583-connection"
	"github.com/moov-io/iso8583/network"
	"github.com/stretchr/testify/assert"
)

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
		header:   utils.NewBinary2BytesAdapter(),
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

