package connection

import (
	"fmt"
	"strings"
	"time"

	"jiso/internal/config"

	"github.com/moov-io/iso8583"
)

type RouteMatcher interface {
	MatchAndCompose(req *iso8583.Message, spec *iso8583.MessageSpec) (*config.MockRouteConfig, *iso8583.Message, error)
}

func NormalizeStan(stan string) string {
	stan = strings.TrimSpace(stan)
	if stan == "" {
		return ""
	}
	if len(stan) < 6 {
		return strings.Repeat("0", 6-len(stan)) + stan
	}
	return stan
}

func getStan(message *iso8583.Message) string {
	if message == nil {
		return ""
	}
	stanField := message.GetField(11)
	if stanField == nil {
		return ""
	}
	val, err := stanField.String()
	if err != nil || val == "" {
		return ""
	}
	return NormalizeStan(val)
}

// Manager handles connections to ISO8583 servers

func (m *Manager) handleInboundMessage(message *iso8583.Message) {
	// Get normalized STAN from response if present
	stan := getStan(message)
	mti, _ := message.GetMTI()

	// Check if message is a response MTI (e.g., 0810, 0110, 0210, 0410 - 3rd digit is odd 1/3/5)
	isResponse := len(mti) == 4 && (mti[2] == '1' || mti[2] == '3' || mti[2] == '5')

	var pending *pendingRequest
	var exists bool
	if stan != "" {
		// Find and remove pending request atomically
		m.pendingMu.Lock()
		pending, exists = m.pendingRequests[stan]
		if exists {
			delete(m.pendingRequests, stan)
		}
		m.pendingMu.Unlock()
	}

	if exists && pending != nil {
		// Send response to waiting goroutine with timeout protection
		select {
		case pending.responseChan <- message:
			// Successfully sent response
		case <-time.After(100 * time.Millisecond):
			if m.debugMode {
				fmt.Printf("Timeout sending inbound message to channel for STAN %s\n", stan)
			}
			// Close the channel to signal completion
			close(pending.responseChan)
		}
		return
	}

	// If this is a response to a synchronous Send() call, iso8583-connection matches it internally.
	// We don't want to log unsolicited warning or trigger mock route matchers for response messages.
	if isResponse {
		return
	}

	if exists && pending != nil {
		// Send response to waiting goroutine with timeout protection
		select {
		case pending.responseChan <- message:
			// Successfully sent response
		case <-time.After(100 * time.Millisecond):
			if m.debugMode {
				fmt.Printf("Timeout sending inbound message to channel for STAN %s\n", stan)
			}
			// Close the channel to signal completion
			close(pending.responseChan)
		}
		return
	}

	// Unmatched response or unsolicited incoming message
	m.statusMu.RLock()
	matcher := m.mockMatcher
	m.statusMu.RUnlock()

	if matcher != nil {
		go func(req *iso8583.Message) {
			mti, _ := req.GetMTI()
			matchedRoute, resp, err := matcher.MatchAndCompose(req, m.spec)
			if err != nil || resp == nil {
				if m.debugMode {
					fmt.Printf("\n[CLIENT-UNSOLICITED] ❌ Error matching/composing response for MTI %s: %v\n", mti, err)
				}
				return
			}

			routeName := "Catch-all Fallback"
			if matchedRoute != nil {
				routeName = matchedRoute.Name
				if matchedRoute.DropConnection {
					fmt.Printf("\n[CLIENT-UNSOLICITED] 🔴 Matched Route '%s' for MTI %s -> Dropping connection\n", routeName, mti)
					_ = m.Close()
					return
				}
			}

			respCode := ""
			if f39 := resp.GetField(39); f39 != nil {
				respCode, _ = f39.String()
			}
			respMTI, _ := resp.GetMTI()

			if matchedRoute != nil {
				fmt.Printf("\n[CLIENT-UNSOLICITED] 🟢 Matched Route '%s' for MTI %s -> Responding %s (RC: %s)\n", routeName, mti, respMTI, respCode)
			} else {
				fmt.Printf("\n[CLIENT-UNSOLICITED] ⚠️ Fallback (No Route Match) for MTI %s -> Responding %s (RC: 12)\n", mti, respMTI)
			}

			m.statusMu.RLock()
			conn := m.Connection
			m.statusMu.RUnlock()

			if conn != nil {
				if err := conn.Reply(resp); err != nil && m.debugMode {
					fmt.Printf("\n[CLIENT-UNSOLICITED] ❌ Error sending reply: %v\n", err)
				}
			}
		}(message)
	} else if m.debugMode {
		fmt.Printf("Unmatched inbound message received for STAN %s\n", stan)
	}
}

// SendAsync sends a message asynchronously and returns a channel for the response

func (m *Manager) attemptReconnect() {
	m.reconnectMu.Lock()
	if m.reconnecting {
		m.reconnectMu.Unlock()
		return // Already reconnecting
	}
	m.reconnecting = true
	m.reconnectMu.Unlock()

	defer func() {
		m.reconnectMu.Lock()
		m.reconnecting = false
		m.reconnectMu.Unlock()
	}()

	maxBackoff := 30 * time.Second
	baseDelay := 1 * time.Second

	for attempt := 1; attempt <= m.reconnectAttempts; attempt++ {
		delay := time.Duration(1<<uint(attempt-1)) * baseDelay
		if delay > maxBackoff {
			delay = maxBackoff
		}

		if m.networkStats != nil {
			m.networkStats.RecordBackoff(delay)
		}

		if m.debugMode {
			fmt.Printf(
				"Waiting %v before reconnection attempt %d/%d\n",
				delay,
				attempt,
				m.reconnectAttempts,
			)
		}
		time.Sleep(delay)

		if m.networkStats != nil {
			m.networkStats.RecordReconnectAttempt()
		}

		startTime := time.Now()
		err := m.Connect(m.naps, m.header)
		if err == nil {
			if m.networkStats != nil {
				duration := time.Since(startTime)
				m.networkStats.RecordReconnectSuccess(duration)
			}
			if m.debugMode {
				fmt.Printf("Reconnection successful on attempt %d\n", attempt)
			}
			return
		}

		if m.networkStats != nil {
			m.networkStats.RecordReconnectFailure()
		}

		if m.debugMode {
			fmt.Printf("Reconnection attempt %d failed: %s\n", attempt, err)
		}
	}

	if m.debugMode {
		fmt.Printf("All reconnection attempts failed\n")
	}
}
