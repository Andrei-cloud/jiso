// options.go turns the manager's settings into the options a moov connection is
// built with, including the timeout and header parameters a reconnect must reuse.
// manager.go owns the connection's state; this file only answers "with what
// settings should the next connection be made".
package connection

import (
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/moov-io/iso8583"
	moovconnection "github.com/moov-io/iso8583-connection"
	iso8583errors "github.com/moov-io/iso8583/errors"
	"github.com/moov-io/iso8583/network"
	isoutl "github.com/moov-io/iso8583/utils"
)

// responseTimeoutDur returns the configured response timeout.
func (m *Manager) responseTimeoutDur() time.Duration {
	return time.Duration(m.responseTimeout.Load())
}

// setConnParams stores the framing parameters used for (re)connection and
// payload building. Safe for concurrent use with Send/attemptReconnect reads.
func (m *Manager) setConnParams(naps bool, header network.Header) {
	m.paramsMu.Lock()
	m.naps = naps
	m.header = header
	m.paramsMu.Unlock()
}

// connParams returns the stored framing parameters.
func (m *Manager) connParams() (bool, network.Header) {
	m.paramsMu.RLock()
	defer m.paramsMu.RUnlock()
	return m.naps, m.header
}

func (m *Manager) buildConnectionOptions() []moovconnection.Option {
	options := []moovconnection.Option{
		moovconnection.ConnectTimeout(m.connectTimeout),
	}

	tlsCfg := m.GetTLSConfig()
	if tlsCfg != nil {
		options = append(options, func(opts *moovconnection.Options) error {
			opts.TLSConfig = tlsCfg
			return nil
		})
	}

	options = append(options,
		moovconnection.ErrorHandler(func(err error) {
			if m.debugMode.Load() {
				outputf("Error encountered: %s\n", err)
			}

			var unpackErr *iso8583errors.UnpackError
			if errors.As(err, &unpackErr) {
				outputf("Unpack error: %s\n", unpackErr)
				outputf("\n%v\n", hex.Dump(unpackErr.RawMessage))
				return
			}

			var safeErr *isoutl.SafeError
			if errors.As(err, &safeErr) {
				outputf("Unsafe error: %s\n", safeErr.UnsafeError())
			}

			if errors.Is(err, io.EOF) || errors.Is(err, moovconnection.ErrConnectionClosed) {
				outputf("Connection closed\n")
				m.statusMu.RLock()
				listenMode := m.listenMode
				m.statusMu.RUnlock()
				if listenMode {
					go m.attemptReListen()
				} else if m.reconnectAttempts > 0 {
					go m.attemptReconnect()
				}
			}
		}),
		moovconnection.InboundMessageHandler(
			func(_ *moovconnection.Connection, message *iso8583.Message) {
				m.handleInboundMessage(message)
			},
		),
		moovconnection.OnConnect(func(_ *moovconnection.Connection) error {
			if m.debugMode.Load() {
				outputf("Connection established to %s\n", m.GetAddress())
			}
			return nil
		}),
		moovconnection.ConnectionClosedHandler(func(_ *moovconnection.Connection) {
			if m.debugMode.Load() {
				outputf("Connection closed to %s\n", m.GetAddress())
			}
		}),
	)
	return options
}
