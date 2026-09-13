// connect.go is the connection lifecycle an App owns: opening the service, connecting
// with a length type, disconnecting, and what "connected" means here. Sending a
// message is in app.go, which is where the send result is assembled.
package app

import (
	"fmt"
	"strings"

	"jiso/internal/app/events"
	"jiso/internal/service"
	"jiso/internal/utils"
)

// Connect dials the configured target using the configured length header
// (defaulting to ascii4).
func (a *App) Connect() error {
	lengthType := strings.TrimSpace(a.cfg.GetHeader())
	if lengthType == "" {
		lengthType = defaultLengthType
	}

	return a.ConnectWith(lengthType)
}

// ConnectWith dials the configured target using the given length type
// (ascii4, binary2, binary4, bcd2, NAPS, visa). Success and failure are
// published to the event bus as ConnectionEvent.
func (a *App) ConnectWith(lengthType string) error {
	svc, err := a.openService()
	if err != nil {
		return err
	}

	header, err := utils.SelectLength(lengthType)
	if err != nil {
		a.events.Publish(events.ConnectionEvent{State: events.StateFailed, Detail: err.Error()})

		return err
	}

	naps := lengthType == "NAPS"

	if err := svc.Connect(naps, header); err != nil {
		a.events.Publish(events.ConnectionEvent{State: events.StateFailed, Detail: err.Error()})

		return err
	}

	a.events.Publish(events.ConnectionEvent{State: events.StateConnected, Detail: a.targetAddress()})

	return nil
}

// Disconnect closes the active connection, leaving the App usable for a
// subsequent Connect. Success and failure are published to the event bus
// as ConnectionEvent.
func (a *App) Disconnect() error {
	a.mu.Lock()
	svc := a.svc
	closed := a.closed
	a.mu.Unlock()

	if closed || svc == nil {
		return nil
	}

	if err := svc.Disconnect(); err != nil {
		a.events.Publish(events.ConnectionEvent{State: events.StateFailed, Detail: err.Error()})

		return err
	}

	a.events.Publish(events.ConnectionEvent{State: events.StateDisconnected, Detail: a.targetAddress()})

	return nil
}

// IsConnected reports whether the service currently holds an online
// connection.
func (a *App) IsConnected() bool {
	a.mu.Lock()
	svc := a.svc
	a.mu.Unlock()

	return svc != nil && svc.IsConnected()
}

// targetAddress formats the configured host:port for event details.
func (a *App) targetAddress() string {
	return fmt.Sprintf("%s:%s", a.cfg.GetHost(), a.cfg.GetPort())
}

// openService returns the live service or ErrClosed once Close ran.
func (a *App) openService() (*service.Service, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.svc == nil {
		return nil, ErrClosed
	}

	return a.svc, nil
}
