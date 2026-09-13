package app

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"jiso/internal/app/events"
)

// acceptServer accepts and holds connections without speaking ISO8583;
// enough for Connect to succeed at the transport level.
func acceptServer(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open accept server: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				// Hold the connection open until the peer closes it.
				buf := make([]byte, 1024)
				for {
					if _, err := conn.Read(buf); err != nil {
						_ = conn.Close()
						return
					}
				}
			}()
		}
	}()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener.Addr() = %T, want *net.TCPAddr", listener.Addr())
	}

	return strconv.Itoa(addr.Port)
}

// nextConnectionEvent waits for the next ConnectionEvent on ch.
func nextConnectionEvent(t *testing.T, ch <-chan events.Event) events.ConnectionEvent {
	t.Helper()

	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("event channel closed before a ConnectionEvent arrived")
		}
		ce, ok := ev.(events.ConnectionEvent)
		if !ok {
			t.Fatalf("event = %T (%+v), want events.ConnectionEvent", ev, ev)
		}

		return ce
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for ConnectionEvent")

		return events.ConnectionEvent{}
	}
}

func TestAppEventsConnectFailurePublishesFailed(t *testing.T) {
	cfg := testConfig(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open probe listener: %v", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener.Addr() = %T, want *net.TCPAddr", listener.Addr())
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("failed to close probe listener: %v", err)
	}

	cfg.SetHost("127.0.0.1")
	cfg.SetPort(strconv.Itoa(addr.Port))
	cfg.SetReconnectAttempts(0)
	cfg.SetConnectTimeout(time.Second)
	cfg.SetTotalConnectTimeout(2 * time.Second)

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer func() {
		_ = a.Close()
	}()

	if a.Events() == nil {
		t.Fatal("Events returned nil")
	}

	ch, unsub := a.Events().Subscribe()
	defer unsub()

	if err := a.Connect(); err == nil {
		t.Fatal("Connect to closed port returned no error")
	}

	ce := nextConnectionEvent(t, ch)
	if ce.State != events.StateFailed {
		t.Errorf("State = %q, want %q", ce.State, events.StateFailed)
	}
	if strings.TrimSpace(ce.Detail) == "" {
		t.Error("failed ConnectionEvent carries no Detail text")
	}
}

func TestAppEventsConnectDisconnectPublishes(t *testing.T) {
	cfg := testConfig(t)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort(acceptServer(t))
	cfg.SetConnectTimeout(2 * time.Second)
	cfg.SetTotalConnectTimeout(4 * time.Second)

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer func() {
		_ = a.Close()
	}()

	ch, unsub := a.Events().Subscribe()
	defer unsub()

	if err := a.Connect(); err != nil {
		t.Fatalf("Connect against accept server failed: %v", err)
	}

	ce := nextConnectionEvent(t, ch)
	if ce.State != events.StateConnected {
		t.Errorf("State = %q, want %q", ce.State, events.StateConnected)
	}
	if ce.Detail != "127.0.0.1:"+cfg.GetPort() {
		t.Errorf("Detail = %q, want the target address", ce.Detail)
	}

	if err := a.Disconnect(); err != nil {
		t.Fatalf("Disconnect failed: %v", err)
	}

	ce = nextConnectionEvent(t, ch)
	if ce.State != events.StateDisconnected {
		t.Errorf("State = %q, want %q", ce.State, events.StateDisconnected)
	}
}

func TestAppEventsCloseClosesBus(t *testing.T) {
	cfg := testConfig(t)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ch, unsub := a.Events().Subscribe()
	defer unsub()

	if n := a.Events().Subscribers(); n != 1 {
		t.Fatalf("Subscribers = %d, want 1", n)
	}

	if err := a.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close must be idempotent, got: %v", err)
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("event channel still open after App.Close")
		}
	case <-time.After(time.Second):
		t.Fatal("event channel not closed after App.Close")
	}

	if n := a.Events().Subscribers(); n != 0 {
		t.Errorf("Subscribers after Close = %d, want 0", n)
	}
}
