package connection

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/moov-io/iso8583"
	moovconnection "github.com/moov-io/iso8583-connection"

	"jiso/internal/utils"
)

type pendingRequest struct {
	responseChan    chan *iso8583.Message
	timeout         time.Time
	transactionName string
}

// NewManager creates a new connection manager

func (m *Manager) buildFullPayload(msg *iso8583.Message) ([]byte, error) {
	packedMsg, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack message: %w", err)
	}

	naps, header := m.connParams()
	if header == nil {
		return packedMsg, nil
	}

	// Fast paths for common headers avoiding cloneHeader and buffer growth
	if !naps {
		switch header.(type) {
		case *utils.Binary2BytesAdapter:
			fullPayload := make([]byte, 2+len(packedMsg))
			binary.BigEndian.PutUint16(fullPayload[0:2], uint16(len(packedMsg)))
			copy(fullPayload[2:], packedMsg)
			return fullPayload, nil
		case *utils.Binary4BytesAdapter:
			fullPayload := make([]byte, 4+len(packedMsg))
			binary.BigEndian.PutUint32(fullPayload[0:4], uint32(len(packedMsg)))
			copy(fullPayload[4:], packedMsg)
			return fullPayload, nil
		}
	}

	hdr := cloneHeader(header)
	hdr.SetLength(len(packedMsg))

	var buf bytes.Buffer
	buf.Grow(32 + len(packedMsg))
	if naps {
		napsWrite := utils.NapsWriteLengthWrapper(utils.WriteMessageLengthWrapper(hdr))
		if _, err := napsWrite(&buf, len(packedMsg)); err != nil {
			return nil, fmt.Errorf("failed to write message header: %w", err)
		}
	} else {
		if _, err := hdr.WriteTo(&buf); err != nil {
			return nil, fmt.Errorf("failed to write message header: %w", err)
		}
	}

	buf.Write(packedMsg)
	return buf.Bytes(), nil
}

// adoptSpecFor makes the connection speak the dialect of the message it is
// about to send. An analyzed extract stamps its transactions with the
// capture's own spec, and the composer honors that stamp - but the reply
// is unpacked with the spec the connection was BUILT with. Sending a
// visa-dialect request over a flex connection therefore lost every answer
// to an invisible decode failure (UAT: "no response received" next to a
// server log that had matched and answered, and raw unpack-error dumps
// bleeding over the screen). Reconnecting is the honest cost of changing
// dialect mid-session; silently discarding the response is not. Dialect
// identity is the spec name; the adopted pointer equals the message's, so
// the next same-dialect send skips the work.
func (m *Manager) adoptSpecFor(msg *iso8583.Message) error {
	if msg == nil {
		return nil
	}
	msgSpec := msg.GetSpec()
	m.statusMu.RLock()
	cur := m.spec
	m.statusMu.RUnlock()
	if msgSpec == nil || cur == nil || msgSpec == cur || msgSpec.Name == cur.Name {
		return nil
	}

	// setSpec stores the spec and, on an online connection, rebuilds it:
	// the moov reader unpacks inbound with the spec it was dialled with,
	// and a reader left on the old wire format silently drops every
	// response (see Manager.setSpec).
	if err := m.setSpec(msgSpec); err != nil {
		return fmt.Errorf("failed to switch the connection to spec %q: %w", msgSpec.Name, err)
	}

	return nil
}

// Send writes one message and waits for its reply. It refuses when the connection
// is not online instead of queueing: an operator who sends into a dropped
// connection wants that answer now, not after a timeout that ends in the same
// message.
func (m *Manager) Send(msg *iso8583.Message) (*iso8583.Message, error) {
	if err := m.adoptSpecFor(msg); err != nil {
		return nil, err
	}
	// Connection validation and error handling
	m.statusMu.RLock()
	conn := m.Connection
	status := moovconnection.StatusOffline
	if conn != nil {
		status = conn.Status()
	}
	m.statusMu.RUnlock()

	if conn == nil || status == moovconnection.StatusOffline {
		return nil, moovconnection.ErrConnectionClosed
	}

	stan := getStan(msg)
	var responseChan chan *iso8583.Message
	if stan != "" {
		responseChan = make(chan *iso8583.Message, 1)
		responseTimeout := m.responseTimeoutDur()
		pending := &pendingRequest{
			responseChan: responseChan,
			timeout:      time.Now().Add(responseTimeout),
		}
		m.pendingMu.Lock()
		m.pendingRequests[stan] = pending
		m.pendingMu.Unlock()

		defer func() {
			m.pendingMu.Lock()
			delete(m.pendingRequests, stan)
			m.pendingMu.Unlock()
		}()
	}

	fullPayload, err := m.buildFullPayload(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to build message payload: %w", err)
	}

	if m.debugMode.Load() {
		outputf("\nSENDING MESSAGE:\n%v\n", hex.Dump(fullPayload))
	}

	// Send raw combined header + message payload directly in one TCP write
	if _, err := conn.Write(fullPayload); err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}
	m.recordSendBytes(len(fullPayload))

	// Wait for response via responseChan (delivered immediately by reader goroutine) or timeout
	var response *iso8583.Message
	if responseChan != nil {
		select {
		case response = <-responseChan:
			if response == nil {
				return nil, fmt.Errorf("response timeout for STAN %s", stan)
			}
		case <-time.After(m.responseTimeoutDur()):
			return nil, fmt.Errorf("response timeout after %v for STAN %s", m.responseTimeoutDur(), stan)
		}
	} else {
		// Fallback for requests without STAN: use the connection captured
		// under statusMu above; re-reading m.Connection unlocked here would
		// defeat the guarded capture.
		response, err = conn.Send(msg)
		if err != nil {
			return nil, err
		}
	}

	if m.debugMode.Load() && response != nil {
		packedResponse, packErr := response.Pack()
		if packErr == nil {
			outputf("\nRECEIVED RESPONSE:\n%v\n", hex.Dump(packedResponse))
		}
	}

	return response, nil
}

// BackgroundSend sends a message without debug logging (for background operations)
func (m *Manager) BackgroundSend(msg *iso8583.Message) (*iso8583.Message, error) {
	if err := m.adoptSpecFor(msg); err != nil {
		return nil, err
	}
	m.statusMu.RLock()
	conn := m.Connection
	status := moovconnection.StatusOffline
	if conn != nil {
		status = conn.Status()
	}
	m.statusMu.RUnlock()

	if conn == nil || status == moovconnection.StatusOffline {
		return nil, moovconnection.ErrConnectionClosed
	}

	fullPayload, err := m.buildFullPayload(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to build message payload: %w", err)
	}

	if _, err := conn.Write(fullPayload); err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}
	m.recordSendBytes(len(fullPayload))

	return nil, nil
}

// SendAsync writes a message and returns the channel that will carry its reply,
// so the TUI keeps rendering while a send is in flight rather than blocking the
// update loop until the peer answers.
func (m *Manager) SendAsync(
	msg *iso8583.Message,
	transactionName string,
) (<-chan *iso8583.Message, error) {
	if err := m.adoptSpecFor(msg); err != nil {
		return nil, err
	}
	m.statusMu.RLock()
	conn := m.Connection
	status := moovconnection.StatusOffline
	if conn != nil {
		status = conn.Status()
	}
	m.statusMu.RUnlock()

	if conn == nil || status == moovconnection.StatusOffline {
		return nil, moovconnection.ErrConnectionClosed
	}

	// Get STAN from request
	stan := getStan(msg)
	if stan == "" {
		return nil, fmt.Errorf("request missing or invalid STAN field")
	}

	responseTimeout := m.responseTimeoutDur()

	// Create pending request
	responseChan := make(chan *iso8583.Message, 1)
	pending := &pendingRequest{
		responseChan:    responseChan,
		timeout:         time.Now().Add(responseTimeout),
		transactionName: transactionName,
	}

	// Check for duplicate STAN and add to pending requests atomically
	m.pendingMu.Lock()
	if _, exists := m.pendingRequests[stan]; exists {
		m.pendingMu.Unlock()
		return nil, fmt.Errorf("STAN %s already in use by pending request", stan)
	}

	// Check if max pending requests limit is reached
	if maxPending := m.GetMaxPendingRequests(); len(m.pendingRequests) >= maxPending {
		m.pendingMu.Unlock()
		return nil, fmt.Errorf("maximum pending requests limit reached (%d)", maxPending)
	}

	m.pendingRequests[stan] = pending
	m.pendingMu.Unlock()

	fullPayload, err := m.buildFullPayload(msg)
	if err != nil {
		m.pendingMu.Lock()
		delete(m.pendingRequests, stan)
		m.pendingMu.Unlock()
		return nil, fmt.Errorf("failed to build message payload: %w", err)
	}

	if m.debugMode.Load() {
		outputf("\nSENDING MESSAGE:\n%v\n", hex.Dump(fullPayload))
	}

	if _, err := conn.Write(fullPayload); err != nil {
		m.pendingMu.Lock()
		delete(m.pendingRequests, stan)
		m.pendingMu.Unlock()
		return nil, fmt.Errorf("failed to send message: %w", err)
	}
	m.recordSendBytes(len(fullPayload))

	// Set timeout handler with time.AfterFunc to avoid dedicated goroutine allocation
	time.AfterFunc(responseTimeout, func() {
		m.pendingMu.Lock()
		if pendingReq, exists := m.pendingRequests[stan]; exists && pendingReq == pending {
			delete(m.pendingRequests, stan)
			select {
			case pendingReq.responseChan <- nil: // Send nil to indicate timeout
			default:
			}
			close(pendingReq.responseChan)
			if m.debugMode.Load() {
				outputf("Request timeout for STAN %s, transaction %s\n", stan, transactionName)
			}
		}
		m.pendingMu.Unlock()
	})

	return responseChan, nil
}

// SetResponseTimeout sets the timeout for waiting responses
