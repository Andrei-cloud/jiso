package connection

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/moov-io/iso8583"
	moovconnection "github.com/moov-io/iso8583-connection"
)

type pendingRequest struct {
	responseChan    chan *iso8583.Message
	timeout         time.Time
	transactionName string
}

// NewManager creates a new connection manager

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

	// Debug logging
	if m.debugMode {
		// Log the request
		packedMsg, err := msg.Pack()
		if err != nil {
			return nil, fmt.Errorf("failed to pack message: %w", err)
		}
		fmt.Printf("\nSENDING MESSAGE:\n%v\n", hex.Dump(packedMsg))

		// Send and get response
		response, err := m.Connection.Send(msg)
		if err != nil {
			return nil, err
		}

		// Log the response
		packedResponse, err := response.Pack()
		if err != nil {
			return nil, fmt.Errorf("failed to pack response: %w", err)
		}
		fmt.Printf("\nRECEIVED RESPONSE:\n%v\n", hex.Dump(packedResponse))

		return response, nil
	}

	// Regular operation without debug
	return m.Connection.Send(msg)
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

	return m.Connection.Send(msg)
}

// IsConnected returns the connection status

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
	stanField := msg.GetField(11)
	if stanField == nil {
		return nil, fmt.Errorf("request missing STAN field")
	}
	stan, err := stanField.String()
	if err != nil {
		return nil, fmt.Errorf("failed to get STAN from request: %w", err)
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

	// Send the message without waiting for response
	// If sending fails, clean up the pending request
	err = conn.Reply(msg)
	if err != nil {
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
