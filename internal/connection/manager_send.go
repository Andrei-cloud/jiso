package connection

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"time"

	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	moovconnection "github.com/moov-io/iso8583-connection"
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

	if m.header == nil {
		return packedMsg, nil
	}

	hdr := cloneHeader(m.header)
	hdr.SetLength(len(packedMsg))

	var buf bytes.Buffer
	if m.naps {
		napsWrite := utils.NapsWriteLengthWrapper(utils.WriteMessageLengthWrapper(hdr))
		if _, err := napsWrite(&buf, len(packedMsg)); err != nil {
			return nil, fmt.Errorf("failed to write message header: %w", err)
		}
	} else {
		if _, err := hdr.WriteTo(&buf); err != nil {
			return nil, fmt.Errorf("failed to write message header: %w", err)
		}
	}

	fullPayload := append(buf.Bytes(), packedMsg...)
	return fullPayload, nil
}

func (m *Manager) Send(msg *iso8583.Message) (*iso8583.Message, error) {
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
		pending := &pendingRequest{
			responseChan: responseChan,
			timeout:      time.Now().Add(m.responseTimeout),
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

	if m.debugMode {
		fmt.Printf("\nSENDING MESSAGE:\n%v\n", hex.Dump(fullPayload))
	}

	// Send raw combined header + message payload directly in one TCP write
	if _, err := conn.Write(fullPayload); err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	// Wait for response via responseChan (delivered immediately by reader goroutine) or timeout
	var response *iso8583.Message
	if responseChan != nil {
		select {
		case response = <-responseChan:
			if response == nil {
				return nil, fmt.Errorf("response timeout for STAN %s", stan)
			}
		case <-time.After(m.responseTimeout):
			return nil, fmt.Errorf("response timeout after %v for STAN %s", m.responseTimeout, stan)
		}
	} else {
		// Fallback for requests without STAN
		response, err = m.Connection.Send(msg)
		if err != nil {
			return nil, err
		}
	}

	if m.debugMode && response != nil {
		packedResponse, packErr := response.Pack()
		if packErr == nil {
			fmt.Printf("\nRECEIVED RESPONSE:\n%v\n", hex.Dump(packedResponse))
		}
	}

	return response, nil
}

// BackgroundSend sends a message without debug logging (for background operations)
func (m *Manager) BackgroundSend(msg *iso8583.Message) (*iso8583.Message, error) {
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

	return nil, nil
}

func (m *Manager) SendAsync(
	msg *iso8583.Message,
	transactionName string,
) (<-chan *iso8583.Message, error) {
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

	// Create pending request
	responseChan := make(chan *iso8583.Message, 1)
	pending := &pendingRequest{
		responseChan:    responseChan,
		timeout:         time.Now().Add(m.responseTimeout),
		transactionName: transactionName,
	}

	// Check for duplicate STAN and add to pending requests atomically
	m.pendingMu.Lock()
	if _, exists := m.pendingRequests[stan]; exists {
		m.pendingMu.Unlock()
		return nil, fmt.Errorf("STAN %s already in use by pending request", stan)
	}

	// Check if max pending requests limit is reached
	if len(m.pendingRequests) >= m.maxPendingRequests {
		m.pendingMu.Unlock()
		return nil, fmt.Errorf("maximum pending requests limit reached (%d)", m.maxPendingRequests)
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

	if m.debugMode {
		fmt.Printf("\nSENDING MESSAGE:\n%v\n", hex.Dump(fullPayload))
	}

	if _, err := conn.Write(fullPayload); err != nil {
		m.pendingMu.Lock()
		delete(m.pendingRequests, stan)
		m.pendingMu.Unlock()
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	// Start timeout handler
	go func() {
		time.Sleep(m.responseTimeout)
		m.pendingMu.Lock()
		if pendingReq, exists := m.pendingRequests[stan]; exists && pendingReq == pending {
			delete(m.pendingRequests, stan)
			select {
			case pendingReq.responseChan <- nil: // Send nil to indicate timeout
			default:
			}
			close(pendingReq.responseChan)
			if m.debugMode {
				fmt.Printf("Request timeout for STAN %s, transaction %s\n", stan, transactionName)
			}
		}
		m.pendingMu.Unlock()
	}()

	return responseChan, nil
}

// SetResponseTimeout sets the timeout for waiting responses
